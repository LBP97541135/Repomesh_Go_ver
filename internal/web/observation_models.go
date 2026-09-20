package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/access"
)

// ObservationModels connects the authenticated settings page to the local
// workbench's existing private configuration. It is not a general HTTP proxy.
type ObservationModels struct {
	origin string
	client *http.Client
}

func NewObservationModels(address string) (*ObservationModels, error) {
	if address == "" {
		address = "http://127.0.0.1:18090"
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("observation workbench must be a loopback HTTP(S) origin")
	}
	// Do not resolve arbitrary hostnames or use environment HTTP proxies when
	// transferring a key to another process on this machine.
	if u.Hostname() == "localhost" {
		if u.Port() == "" {
			u.Host = "127.0.0.1"
		} else {
			u.Host = net.JoinHostPort("127.0.0.1", u.Port())
		}
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("observation workbench must be a loopback HTTP(S) origin")
	}
	u.Path = ""
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &ObservationModels{origin: u.String(), client: &http.Client{
		Transport: transport, Timeout: 25 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func registerObservationModels(mux *http.ServeMux, auth Auth, bridge *ObservationModels) {
	for _, route := range []string{"GET /api/settings/observation-models/{purpose}", "POST /api/settings/observation-models/{purpose}", "POST /api/settings/observation-models/{purpose}/test"} {
		registerModelRoute(mux, route, auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
			admin, err := auth.Service.IsAdmin(r.Context(), principal.ActorID())
			if err != nil {
				return err
			}
			if !admin {
				return &access.Failure{Status: 403, Code: "ADMIN_REQUIRED"}
			}
			if bridge == nil {
				return &access.Failure{Status: 503, Code: "OBSERVATION_WORKBENCH_UNAVAILABLE"}
			}
			return bridge.serve(w, r)
		})
	}
}

func (b *ObservationModels) serve(w http.ResponseWriter, r *http.Request) error {
	purpose := r.PathValue("purpose")
	path, readPath := "/api/model", "/api/settings"
	if purpose == "deepseek" {
		path, readPath = "/api/assistant", "/api/assistant"
	} else if purpose != "jev" {
		return &access.Failure{Status: 404, Code: "RESOURCE_NOT_FOUND"}
	}
	method := r.Method
	var input any
	if method == "GET" {
		path = readPath
	} else if strings.HasSuffix(r.URL.Path, "/test") {
		path += "/test"
		input = struct{}{}
	} else {
		var config struct {
			Model string `json:"model"`
			Key   string `json:"api_key"`
		}
		data, err := readJSONBody(w, r, 16<<10)
		if err != nil {
			return err
		}
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if d.Decode(&config) != nil || d.Decode(new(any)) != io.EOF {
			return &access.Failure{Status: 400, Code: "INVALID_JSON"}
		}
		input = config
		method = "PUT"
	}
	var result struct {
		Model                string   `json:"model"`
		Configured           bool     `json:"configured"`
		ModelConfigured      bool     `json:"model_configured"`
		Provider             string   `json:"provider"`
		Endpoint             string   `json:"endpoint"`
		ModelEndpoint        string   `json:"model_endpoint"`
		OK                   bool     `json:"ok"`
		AuthenticationOK     bool     `json:"authentication_ok"`
		Models               []string `json:"models"`
		SelectedModelListing string   `json:"selected_model_listing"`
		Message              string   `json:"message"`
	}
	if err := b.request(r.Context(), method, path, input, &result); err != nil {
		return err
	}
	if strings.HasSuffix(path, "/test") {
		if !result.AuthenticationOK || result.Models == nil {
			return &access.Failure{Status: 502, Code: "OBSERVATION_MODEL_RESPONSE_INVALID"}
		}
		writeJSON(w, 200, map[string]any{"ok": result.OK, "authentication_ok": result.AuthenticationOK, "models": result.Models, "selected_model_listing": result.SelectedModelListing, "message": result.Message})
		return nil
	}
	if result.Model == "" {
		return &access.Failure{Status: 502, Code: "OBSERVATION_MODEL_RESPONSE_INVALID"}
	}
	if purpose == "jev" && method == "GET" {
		result.Configured = result.ModelConfigured && result.Provider == "typesafe"
	}
	result.Endpoint = "https://api.deepseek.com/chat/completions"
	if purpose == "jev" {
		result.Endpoint = "https://api.typesafe.ai/v1/systemone"
	}
	writeJSON(w, 200, map[string]any{"model": result.Model, "configured": result.Configured, "endpoint": result.Endpoint})
	return nil
}

func (b *ObservationModels) request(ctx context.Context, method, path string, body, result any) error {
	var encoded []byte
	if body != nil {
		encoded, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.origin+path, bytes.NewReader(encoded))
	if err != nil {
		return &access.Failure{Status: 503, Code: "OBSERVATION_WORKBENCH_UNAVAILABLE"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-RepoMesh-Local", "1")
	// No browser cookies, authorization headers or caller-selected URLs cross
	// this boundary. The workbench remains the single owner of these keys.
	res, err := b.client.Do(req)
	if err != nil {
		return &access.Failure{Status: 503, Code: "OBSERVATION_WORKBENCH_UNAVAILABLE"}
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		status, code := 502, "OBSERVATION_MODEL_REQUEST_FAILED"
		if res.StatusCode == 400 || res.StatusCode == 409 {
			status, code = res.StatusCode, "OBSERVATION_CONFIGURATION_REJECTED"
		}
		return &access.Failure{Status: status, Code: code}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if err != nil || len(data) > 64<<10 || json.Unmarshal(data, result) != nil {
		return &access.Failure{Status: 502, Code: "OBSERVATION_MODEL_RESPONSE_INVALID"}
	}
	return nil
}
