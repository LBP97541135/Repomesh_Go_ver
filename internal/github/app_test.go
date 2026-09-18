package github

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var sharedTestKey = sync.OnceValues(func() (*rsa.PrivateKey, error) { return rsa.GenerateKey(rand.Reader, 2048) })

func testKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := sharedTestKey()
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func TestAppCapabilityRequiresEachWorkingPermission(t *testing.T) {
	for _, test := range []struct {
		name, permissions, suspended string
		wantStatus                   string
		wantReasons                  []string
	}{
		{"allowed", `{"contents":"write","pull_requests":"write","metadata":"read"}`, `null`, "allowed", []string{}},
		{"metadata only", `{"metadata":"read"}`, `null`, "denied", []string{"APP_PERMISSION_MISSING"}},
		{"contents read", `{"contents":"read","pull_requests":"write","metadata":"read"}`, `null`, "denied", []string{"APP_PERMISSION_MISSING"}},
		{"pull requests read", `{"contents":"write","pull_requests":"read","metadata":"read"}`, `null`, "denied", []string{"APP_PERMISSION_MISSING"}},
		{"metadata absent", `{"contents":"write","pull_requests":"write"}`, `null`, "denied", []string{"APP_PERMISSION_MISSING"}},
		{"suspended", `{"contents":"write","pull_requests":"write","metadata":"read"}`, `"2026-09-11T00:00:00Z"`, "denied", []string{"APP_INSTALLATION_SUSPENDED"}},
		{"suspended and missing", `{"metadata":"read"}`, `"2026-09-11T00:00:00Z"`, "denied", []string{"APP_INSTALLATION_SUSPENDED", "APP_PERMISSION_MISSING"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := testClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodGet || req.URL.String() != "https://api.github.com/repos/owner/project/installation" {
					t.Fatal("incorrect app endpoint")
				}
				verifyAppJWT(t, req.Header.Get("Authorization"))
				return reply(200, `{"id":12,"app_id":123,"suspended_at":`+test.suspended+`,"permissions":`+test.permissions+`}`, nil), nil
			})
			capability, err := client.AppCapability(context.Background(), "owner", "project")
			if err != nil {
				t.Fatal(err)
			}
			if capability.Status != test.wantStatus || !reflect.DeepEqual(capability.ReasonCodes, test.wantReasons) || capability.ObservedAt == nil || time.Since(*capability.ObservedAt) > time.Second || calls != 1 {
				t.Fatalf("capability = %+v, calls = %d", capability, calls)
			}
		})
	}
}

func verifyAppJWT(t *testing.T, authorization string) {
	t.Helper()
	if !strings.HasPrefix(authorization, "Bearer ") {
		t.Fatal("app authorization missing")
	}
	parts := strings.Split(strings.TrimPrefix(authorization, "Bearer "), ".")
	if len(parts) != 3 {
		t.Fatal("invalid JWT")
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var headerValue struct {
		Alg  string `json:"alg"`
		Type string `json:"typ"`
	}
	if json.Unmarshal(header, &headerValue) != nil || headerValue.Alg != "RS256" || headerValue.Type != "JWT" {
		t.Fatal("incorrect JWT header")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
		Issuer    string `json:"iss"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		t.Fatal("invalid JWT claims")
	}
	now := time.Now().Unix()
	if claims.Issuer != "Iv1.client" || claims.IssuedAt < now-62 || claims.IssuedAt > now-59 || claims.ExpiresAt <= now || claims.ExpiresAt > now+600 || claims.ExpiresAt-claims.IssuedAt > 600 {
		t.Fatalf("incorrect JWT claims: %+v", claims)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	key, err := sharedTestKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatal("JWT signature invalid")
	}
}

func TestAppFailuresNeverBecomeAllowed(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		payload string
		kind    string
	}{
		{"unauthorized", 401, `{"message":"secret"}`, "unauthorized"},
		{"forbidden", 403, `{"message":"secret"}`, "denied"},
		{"unavailable", 503, `{"message":"secret"}`, "unavailable"},
		{"wrong app", 200, `{"id":12,"app_id":456,"suspended_at":null,"permissions":{"contents":"write","pull_requests":"write","metadata":"read"}}`, "unavailable"},
		{"missing suspension", 200, `{"id":12,"app_id":123,"permissions":{"contents":"write","pull_requests":"write","metadata":"read"}}`, "unavailable"},
		{"malformed suspension", 200, `{"id":12,"app_id":123,"suspended_at":false,"permissions":{"contents":"write","pull_requests":"write","metadata":"read"}}`, "unavailable"},
		{"missing permissions", 200, `{"id":12,"app_id":123,"suspended_at":null}`, "unavailable"},
		{"oversized missing", 404, strings.Repeat("x", maxResponseBytes+1), "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return reply(test.status, test.payload, nil), nil })
			capability, err := client.AppCapability(context.Background(), "owner", "project")
			requireKind(t, err, test.kind)
			if capability.Status != "" || capability.ObservedAt != nil {
				t.Fatal("failed app observation returned capability")
			}
		})
	}
	client := testClient(t, func(*http.Request) (*http.Response, error) { return reply(404, `{"message":"Not Found"}`, nil), nil })
	capability, err := client.AppCapability(context.Background(), "owner", "project")
	if err != nil {
		t.Fatal(err)
	}
	if capability.Status != "denied" || !reflect.DeepEqual(capability.ReasonCodes, []string{"APP_INSTALLATION_MISSING"}) || capability.ObservedAt == nil {
		t.Fatalf("capability = %+v", capability)
	}
}

func TestAppAcceptsPKCS8RSAKey(t *testing.T) {
	key, err := sharedTestKey()
	if err != nil {
		t.Fatal(err)
	}
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t, func(req *http.Request) (*http.Response, error) {
		verifyAppJWT(t, req.Header.Get("Authorization"))
		return reply(404, `{}`, nil), nil
	})
	client.config.PrivateKey = func(context.Context) ([]byte, error) {
		return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}), nil
	}
	_, err = client.AppCapability(context.Background(), "owner", "project")
	if err != nil {
		t.Fatal(err)
	}
}

func TestInvalidAppKeysNeverContactProvider(t *testing.T) {
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecBytes, err := x509.MarshalPKCS8PrivateKey(ec)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{
		[]byte("secret-invalid-key"),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(weak)}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecBytes}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("broken")}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("broken")}),
		pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte("secret")}),
		append(testKeyPEM(t), []byte("extra secret material")...),
		[]byte(strings.Repeat("secret", 12000)),
	} {
		client := testClient(t, func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid key contacted provider")
			return nil, nil
		})
		client.config.PrivateKey = func(context.Context) ([]byte, error) { return data, nil }
		_, err := client.AppCapability(context.Background(), "owner", "project")
		requireKind(t, err, "unavailable")
	}
	client := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("missing key contacted provider")
		return nil, nil
	})
	client.config.PrivateKey = func(context.Context) ([]byte, error) { return nil, errors.New("private-key-path") }
	_, err = client.AppCapability(context.Background(), "owner", "project")
	requireKind(t, err, "unavailable")
}

func TestAppTokenIsReusedAcrossCalls(t *testing.T) {
	var reads atomic.Int32
	client := testClient(t, func(req *http.Request) (*http.Response, error) {
		verifyAppJWT(t, req.Header.Get("Authorization"))
		return reply(404, `{}`, nil), nil
	})
	keyPEM := testKeyPEM(t)
	client.config.PrivateKey = func(context.Context) ([]byte, error) {
		reads.Add(1)
		return keyPEM, nil
	}
	for range 3 {
		if _, err := client.AppCapability(context.Background(), "owner", "project"); err != nil {
			t.Fatal(err)
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("private key reads = %d", reads.Load())
	}
}

func TestAppCapabilityAllowsConcurrentCalls(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return reply(404, `{}`, nil), nil
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.AppCapability(context.Background(), "owner", "project"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 8 {
		t.Fatalf("calls = %d", calls.Load())
	}
}
