package access

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/database"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/jsoninput"
	"repomesh.local/repomesh/internal/secrets"
)

type Deployment struct {
	Origin           string `json:"origin"`
	AppID            string `json:"appId"`
	ClientID         string `json:"clientId"`
	CallbackURL      string `json:"callbackUrl"`
	ClientSecretFile string `json:"clientSecretFile"`
	PrivateKeyFile   string `json:"privateKeyFile"`
	ActiveRootID     string `json:"activeRootId"`
	Roots            []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	} `json:"roots"`
	TLSCertificateFile string `json:"tlsCertificateFile"`
	TLSKeyFile         string `json:"tlsKeyFile"`
}

type Runtime struct {
	Service    *Service
	Deployment Deployment
	db         *database.DB
	secrets    *secrets.Store
}

func (runtime *Runtime) SecretStore() *secrets.Store { return runtime.secrets }

func (runtime *Runtime) Close() { runtime.db.Close() }

func (runtime *Runtime) Pool() *pgxpool.Pool { return runtime.db.Pool() }

func OpenRuntime(ctx context.Context, configPath, databaseURL string) (*Runtime, error) {
	configFile, err := os.Open(configPath)
	if err != nil {
		return nil, errors.New("authentication configuration unavailable")
	}
	defer configFile.Close()
	data, err := io.ReadAll(io.LimitReader(configFile, 1024*1024+1))
	var config Deployment
	if err != nil || jsoninput.Decode(data, &config, 1024*1024) != nil {
		return nil, errors.New("invalid authentication configuration")
	}
	origin, err := url.Parse(config.Origin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || config.CallbackURL != config.Origin+"/api/auth/github/callback" {
		return nil, errors.New("authentication requires an exact HTTPS origin and callback")
	}
	if (config.TLSCertificateFile == "") != (config.TLSKeyFile == "") {
		return nil, errors.New("HTTPS certificate and key must be configured together")
	}
	roots := secrets.Config{ActiveRootID: config.ActiveRootID}
	for _, root := range config.Roots {
		roots.Roots = append(roots.Roots, secrets.RootFile{ID: root.ID, Path: root.Path})
	}
	var store *secrets.Store
	var clientRef, privateRef secrets.VersionID
	owner := secrets.Owner{Kind: "github-app", ID: config.AppID}
	provider, err := github.New(github.Config{ClientID: config.ClientID, AppID: config.AppID, CallbackURL: config.CallbackURL,
		ClientSecret: func(ctx context.Context) ([]byte, error) {
			return store.Open(ctx, clientRef, owner, "github-app-client-secret")
		},
		PrivateKey: func(ctx context.Context) ([]byte, error) {
			return store.Open(ctx, privateRef, owner, "github-app-private-key")
		},
	})
	if err != nil {
		return nil, err
	}
	db, err := database.Open(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			db.Close()
		}
	}()
	state, err := db.Check(ctx)
	if err != nil {
		return nil, err
	}
	if state.Pending != 0 {
		return nil, errors.New("authentication schema is pending; run db migrate explicitly")
	}
	store, err = secrets.New(ctx, db.Pool(), roots)
	if err != nil {
		return nil, err
	}
	service := New(db.Pool(), store, nil)
	clientRef, err = service.importAppCredential(ctx, config, "github-app-client-secret", config.ClientSecretFile)
	if err != nil {
		return nil, err
	}
	privateRef, err = service.importAppCredential(ctx, config, "github-app-private-key", config.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	service.provider = provider
	service.setIssueAppCredential(config.AppID, privateRef)
	ok = true
	return &Runtime{Service: service, Deployment: config, db: db, secrets: store}, nil
}

func (s *Service) importAppCredential(ctx context.Context, config Deployment, purpose secrets.Purpose, path string) (secrets.VersionID, error) {
	plaintext, err := readPrivateFile(path)
	if err != nil {
		return "", err
	}
	if purpose == "github-app-client-secret" {
		plaintext = bytes.TrimSpace(plaintext)
	}
	if len(plaintext) == 0 {
		return "", errors.New("empty App credential")
	}
	fingerprint := digest(string(plaintext))
	var ref secrets.VersionID
	err = s.pool.QueryRow(ctx, `SELECT version_ref FROM repomesh_access.app_credentials WHERE app_id=$1 AND client_id=$2 AND purpose=$3 AND fingerprint=$4`, config.AppID, config.ClientID, string(purpose), fingerprint).Scan(&ref)
	owner := secrets.Owner{Kind: "github-app", ID: config.AppID}
	if err == nil {
		_, err = s.secrets.Open(ctx, ref, owner, purpose)
		return ref, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", unavailable()
	}
	ref, err = s.secrets.Seal(ctx, owner, purpose, plaintext)
	if err != nil {
		return "", err
	}
	var selected secrets.VersionID
	err = s.pool.QueryRow(ctx, `INSERT INTO repomesh_access.app_credentials(app_id,client_id,purpose,fingerprint,version_ref) VALUES($1,$2,$3,$4,$5)
    ON CONFLICT(app_id,client_id,purpose,fingerprint) DO UPDATE SET fingerprint=EXCLUDED.fingerprint RETURNING version_ref`, config.AppID, config.ClientID, string(purpose), fingerprint, string(ref)).Scan(&selected)
	if err != nil {
		return "", unavailable()
	}
	if selected != ref {
		if err = s.secrets.SetEnabled(ctx, ref, false); err != nil {
			return "", err
		}
	}
	return selected, nil
}
