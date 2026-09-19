//go:build linux

package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/secrets"
)

type processDeployment struct {
	configPath          string
	origin              string
	address             string
	trustedCertificates *x509.CertPool
}

type webProcess struct {
	command *exec.Cmd
	done    chan error
	stopped bool
}

type restartReceiptSet struct {
	creation projects.CreationReceipt
	update   projects.UpdateReceipt
}

type restartProcessEvidence struct {
	FirstPID            int  `json:"firstPid"`
	SecondPID           int  `json:"secondPid"`
	FirstExited         bool `json:"firstExited"`
	SecondExited        bool `json:"secondExited"`
	SessionStatus       int  `json:"sessionStatus"`
	CreationStatus      int  `json:"creationStatus"`
	UpdateStatus        int  `json:"updateStatus"`
	SameActor           bool `json:"sameActor"`
	SameCreationReceipt bool `json:"sameCreationReceipt"`
	SameUpdateReceipt   bool `json:"sameUpdateReceipt"`
	UniqueCreation      bool `json:"uniqueCreation"`
	UniqueUpdate        bool `json:"uniqueUpdate"`
}

func TestPostgresProjectProcessRestart(t *testing.T) {
	releaseDirectory := os.Getenv("REPOMESH_B03_TEST_RELEASE_DIR")
	assets := ""
	binary := ""
	if releaseDirectory == "" {
		assets = t.TempDir()
		if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("<!doctype html><title>B03 process restart</title>"), 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		if !filepath.IsAbs(releaseDirectory) {
			t.Fatal("REPOMESH_B03_TEST_RELEASE_DIR must be an absolute directory")
		}
		releaseInfo, err := os.Stat(releaseDirectory)
		if err != nil || !releaseInfo.IsDir() {
			t.Fatal("REPOMESH_B03_TEST_RELEASE_DIR must name an existing directory")
		}
		binary = filepath.Join(releaseDirectory, "bin", "repomesh-web")
		binaryInfo, err := os.Stat(binary)
		if err != nil || !binaryInfo.Mode().IsRegular() || binaryInfo.Mode().Perm()&0111 == 0 {
			t.Fatal("release bin/repomesh-web must be a regular executable file")
		}
		assets = filepath.Join(releaseDirectory, "web", "dist")
		indexInfo, err := os.Stat(filepath.Join(assets, "index.html"))
		if err != nil || !indexInfo.Mode().IsRegular() {
			t.Fatal("release web/dist/index.html must be a regular file")
		}
	}
	expectedIndex, err := os.ReadFile(filepath.Join(assets, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := startProjectBrowserServer(t, assets)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	fixtureClient := fixture.server.Client()
	fixtureClient.Jar = jar
	fixtureClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	expected := seedRestartOperations(t, fixture, fixtureClient)
	databaseURL := fixture.pool.Config().ConnString()
	actor := fixture.fixtures.ActorA
	roots := fixture.rootConfig
	if err := fixture.closeFixture(); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	if releaseDirectory == "" {
		repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			t.Fatal(err)
		}
		binary = buildProcessWebBinary(t, repositoryRoot, directory)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	deployment := prepareProcessDeployment(t, directory, address, roots)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: deployment.trustedCertificates, MinVersion: tls.VersionTLS12}}, Jar: jar, Timeout: 3 * time.Second}
	t.Cleanup(func() { client.CloseIdleConnections() })

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	currentActor := func() string {
		requestCtx, requestCancel := context.WithTimeout(ctx, 3*time.Second)
		defer requestCancel()
		request, requestErr := http.NewRequestWithContext(requestCtx, http.MethodGet, deployment.origin+"/api/session", nil)
		if requestErr != nil {
			return ""
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			return ""
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return ""
		}
		var session access.Session
		if json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&session) != nil {
			return ""
		}
		return session.User.ID
	}
	evidence := restartProcessEvidence{}
	first, err := startProcessWeb(ctx, binary, assets, databaseURL, deployment)
	if err != nil {
		t.Fatal(err)
	}
	evidence.FirstPID = first.command.Process.Pid
	if err = waitProcessWeb(ctx, first, client, deployment.origin); err != nil {
		_ = stopProcessWeb(context.Background(), first)
		t.Fatal(err)
	}
	requestCtx, requestCancel := context.WithTimeout(ctx, 3*time.Second)
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, deployment.origin+"/", nil)
	if err != nil {
		requestCancel()
		_ = stopProcessWeb(context.Background(), first)
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		requestCancel()
		_ = stopProcessWeb(context.Background(), first)
		t.Fatal(err)
	}
	actualIndex, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	requestCancel()
	if readErr != nil {
		_ = stopProcessWeb(context.Background(), first)
		t.Fatal(readErr)
	}
	if response.StatusCode != http.StatusOK || !bytes.Equal(actualIndex, expectedIndex) {
		_ = stopProcessWeb(context.Background(), first)
		t.Fatal("first process root response did not match index.html")
	}
	actual, err := readRestartReceipts(ctx, client, deployment.origin, expected)
	if err != nil {
		_ = stopProcessWeb(context.Background(), first)
		t.Fatal(err)
	}
	evidence.SessionStatus, evidence.CreationStatus, evidence.UpdateStatus = 200, 200, 200
	evidence.SameCreationReceipt, evidence.SameUpdateReceipt = sameRestartReceipts(expected, actual)
	evidence.SameActor = currentActor() == actor
	if err = stopProcessWeb(ctx, first); err != nil {
		t.Fatal(err)
	}
	evidence.FirstExited = true

	second, err := startProcessWeb(ctx, binary, assets, databaseURL, deployment)
	if err != nil {
		t.Fatal(err)
	}
	evidence.SecondPID = second.command.Process.Pid
	if err = waitProcessWeb(ctx, second, client, deployment.origin); err != nil {
		_ = stopProcessWeb(context.Background(), second)
		t.Fatal(err)
	}
	requestCtx, requestCancel = context.WithTimeout(ctx, 3*time.Second)
	request, err = http.NewRequestWithContext(requestCtx, http.MethodGet, deployment.origin+"/", nil)
	if err != nil {
		requestCancel()
		_ = stopProcessWeb(context.Background(), second)
		t.Fatal(err)
	}
	response, err = client.Do(request)
	if err != nil {
		requestCancel()
		_ = stopProcessWeb(context.Background(), second)
		t.Fatal(err)
	}
	actualIndex, readErr = io.ReadAll(response.Body)
	response.Body.Close()
	requestCancel()
	if readErr != nil {
		_ = stopProcessWeb(context.Background(), second)
		t.Fatal(readErr)
	}
	if response.StatusCode != http.StatusOK || !bytes.Equal(actualIndex, expectedIndex) {
		_ = stopProcessWeb(context.Background(), second)
		t.Fatal("second process root response did not match index.html")
	}
	actual, err = readRestartReceipts(ctx, client, deployment.origin, expected)
	if err != nil {
		_ = stopProcessWeb(context.Background(), second)
		t.Fatal(err)
	}
	creationSame, updateSame := sameRestartReceipts(expected, actual)
	evidence.SameCreationReceipt = evidence.SameCreationReceipt && creationSame
	evidence.SameUpdateReceipt = evidence.SameUpdateReceipt && updateSame
	evidence.SameActor = evidence.SameActor && currentActor() == actor
	creationCount, updateCount, err := countRestartOperations(ctx, fixture.pool, actor, expected)
	if err != nil {
		_ = stopProcessWeb(context.Background(), second)
		t.Fatal(err)
	}
	evidence.UniqueCreation, evidence.UniqueUpdate = creationCount == 1, updateCount == 1
	if err = stopProcessWeb(ctx, second); err != nil {
		t.Fatal(err)
	}
	evidence.SecondExited = true
	if !evidence.SameActor || !evidence.SameCreationReceipt || !evidence.SameUpdateReceipt || !evidence.UniqueCreation || !evidence.UniqueUpdate {
		t.Fatalf("process restart assertions failed: %+v", evidence)
	}
	encoded, _ := json.Marshal(evidence)
	t.Log(string(encoded))
}

func buildProcessWebBinary(t *testing.T, repositoryRoot, outputDirectory string) string {
	t.Helper()
	output := filepath.Join(outputDirectory, "repomesh-web")
	command := exec.Command("go", "build", "-o", output, "./cmd/repomesh-web")
	command.Dir = repositoryRoot
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build process web binary: %v: %s", err, strings.TrimSpace(string(result)))
	}
	return output
}

func prepareProcessDeployment(t *testing.T, directory, address string, roots secrets.Config) processDeployment {
	t.Helper()
	write := func(name string, data []byte) string {
		path := filepath.Join(directory, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(data); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
		return path
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "RepoMesh B03 process test"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	certificatePath := write("tls.crt", certificatePEM)
	tlsKeyPath := write("tls.key", keyPEM)
	clientSecretPath := write("client-secret", []byte("b03-process-test-client-secret\n"))
	privateKeyPath := write("github-app.pem", keyPEM)
	origin := "https://" + address
	configuration := access.Deployment{Origin: origin, AppID: "987654321", ClientID: "b03-process-client", CallbackURL: origin + "/api/auth/github/callback", ClientSecretFile: clientSecretPath, PrivateKeyFile: privateKeyPath, ActiveRootID: roots.ActiveRootID, TLSCertificateFile: certificatePath, TLSKeyFile: tlsKeyPath}
	for _, root := range roots.Roots {
		configuration.Roots = append(configuration.Roots, struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		}{ID: root.ID, Path: root.Path})
	}
	configurationBytes, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	configurationPath := write("auth.json", configurationBytes)
	trusted := x509.NewCertPool()
	if !trusted.AppendCertsFromPEM(certificatePEM) {
		t.Fatal("could not trust process test certificate")
	}
	return processDeployment{configPath: configurationPath, origin: origin, address: address, trustedCertificates: trusted}
}

func startProcessWeb(ctx context.Context, binary, assets, databaseURL string, deployment processDeployment) (*webProcess, error) {
	command := exec.CommandContext(ctx, binary, "--addr", deployment.address, "--assets", assets, "--auth-config", deployment.configPath)
	for _, value := range os.Environ() {
		name := strings.SplitN(value, "=", 2)[0]
		if name == "REPOMESH_AUTH_CONFIG" || name == "REPOMESH_WEB_ADDR" || name == "REPOMESH_WEB_ASSETS" || name == "REPOMESH_DATABASE_URL" {
			continue
		}
		command.Env = append(command.Env, value)
	}
	command.Env = append(command.Env, "REPOMESH_DATABASE_URL="+databaseURL)
	if err := command.Start(); err != nil {
		return nil, err
	}
	result := &webProcess{command: command, done: make(chan error, 1)}
	go func() { result.done <- command.Wait() }()
	return result, nil
}

func waitProcessWeb(ctx context.Context, process *webProcess, client *http.Client, origin string) error {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		requestCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, origin+"/healthz", nil)
		if err == nil {
			response, requestErr := client.Do(request)
			if requestErr == nil {
				response.Body.Close()
				cancel()
				if response.StatusCode == 200 {
					return nil
				}
			} else {
				cancel()
			}
		} else {
			cancel()
		}
		select {
		case err := <-process.done:
			process.stopped = true
			if err == nil {
				return errors.New("web process exited before health check with exit 0")
			}
			return errors.New("web process exited before health check")
		case <-deadline.C:
			return errors.New("web process did not become healthy")
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func stopProcessWeb(ctx context.Context, process *webProcess) error {
	if process == nil || process.stopped {
		return nil
	}
	if err := process.command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case err := <-process.done:
		process.stopped = true
		return err
	case <-ctx.Done():
		_ = process.command.Process.Kill()
		err := <-process.done
		process.stopped = true
		if err != nil {
			return ctx.Err()
		}
		return ctx.Err()
	}
}

func seedRestartOperations(t *testing.T, server *browserTestServer, client *http.Client) restartReceiptSet {
	t.Helper()
	request := func(method, path, body, origin, csrf, key string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, server.server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	status, _ := request(http.MethodGet, "/__test/login?actor=a", "", "", "", "")
	if status != http.StatusSeeOther {
		t.Fatalf("restart fixture login status=%d", status)
	}
	status, data := request(http.MethodGet, "/api/session", "", "", "", "")
	var session access.Session
	if status != 200 || json.Unmarshal(data, &session) != nil {
		t.Fatalf("restart fixture session status=%d", status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, _ = request(http.MethodGet, "/api/repositories?limit=50", "", "", "", "")
		if status == 200 {
			break
		}
		if status != 503 || time.Now().After(deadline) {
			t.Fatalf("restart fixture discovery status=%d", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	createKey := "71000000-0000-1000-0000-000000000001"
	createBody := `{"name":"process restart","purpose":"first process","repositoryIds":["` + server.fixtures.RepositoryA + `"]}`
	status, data = request(http.MethodPost, "/api/projects", createBody, server.server.URL, session.CSRFToken, createKey)
	var creation projects.CreationReceipt
	if status != 201 || json.Unmarshal(data, &creation) != nil {
		t.Fatalf("restart fixture create status=%d", status)
	}
	updateKey := "72000000-0000-1000-0000-000000000001"
	updateBody := `{"expectedProjectRevision":"` + creation.ProjectRevision + `","purpose":"second process"}`
	status, data = request(http.MethodPatch, "/api/projects/"+creation.ProjectID, updateBody, server.server.URL, session.CSRFToken, updateKey)
	var update projects.UpdateReceipt
	if status != 200 || json.Unmarshal(data, &update) != nil || update.ProjectRevision == creation.ProjectRevision {
		t.Fatalf("restart fixture update status=%d", status)
	}
	return restartReceiptSet{creation: creation, update: update}
}

func readRestartReceipts(ctx context.Context, client *http.Client, origin string, expected restartReceiptSet) (restartReceiptSet, error) {
	result := restartReceiptSet{}
	for _, target := range []struct {
		path string
		out  any
	}{
		{path: "/api/project-creations/" + expected.creation.ProjectCreationID, out: &result.creation},
		{path: "/api/projects/" + expected.update.ProjectID + "/updates/" + expected.update.UpdateID, out: &result.update},
	} {
		requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, origin+target.path, nil)
		if err != nil {
			cancel()
			return result, err
		}
		response, err := client.Do(request)
		if err != nil {
			cancel()
			return result, err
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		cancel()
		if readErr != nil {
			return result, readErr
		}
		if response.StatusCode != 200 {
			return result, errors.New("restart receipt status " + response.Status)
		}
		if err = json.Unmarshal(data, target.out); err != nil {
			return result, errors.New("restart receipt was invalid JSON")
		}
	}
	return result, nil
}

func sameRestartReceipts(expected, actual restartReceiptSet) (bool, bool) {
	expectedCreation, _ := json.Marshal(expected.creation)
	actualCreation, _ := json.Marshal(actual.creation)
	expectedUpdate, _ := json.Marshal(expected.update)
	actualUpdate, _ := json.Marshal(actual.update)
	return bytes.Equal(expectedCreation, actualCreation), bytes.Equal(expectedUpdate, actualUpdate)
}

func countRestartOperations(ctx context.Context, pool *pgxpool.Pool, actor string, expected restartReceiptSet) (int64, int64, error) {
	var creation, update int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.creation_operations WHERE actor=$1 AND key=$2`, actor, expected.creation.ProjectCreationID).Scan(&creation); err != nil {
		return 0, 0, err
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.update_operations WHERE project_id=$1 AND actor=$2 AND key=$3`, expected.update.ProjectID, actor, expected.update.UpdateID).Scan(&update); err != nil {
		return 0, 0, err
	}
	return creation, update, nil
}
