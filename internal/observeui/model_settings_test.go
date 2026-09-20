package observeui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssistantSettingsPersistAndTestSavedKey(t *testing.T) {
	s, h := newTestServer(t)
	status, _ := request(t, h, "POST", "/api/assistant/test", map[string]string{})
	if status != 409 {
		t.Fatal("model list requested without a saved key")
	}
	status, _ = request(t, h, "PUT", "/api/assistant", map[string]string{"model": " deepseek-flash ", "api_key": " test-assistant-secret "})
	if status != 200 {
		t.Fatal("initial save failed")
	}
	status, _ = request(t, h, "PUT", "/api/assistant", map[string]string{"model": "deepseek-v4-pro", "api_key": ""})
	if status != 200 {
		t.Fatal("blank key failed to preserve configuration")
	}
	restarted, err := New(Options{Archive: s.archives[0].Journal.Dir})
	if err != nil {
		t.Fatal(err)
	}
	restarted.client = &http.Client{Transport: mockRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.deepseek.com/models" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer test-assistant-secret" {
			t.Error("test did not use the saved provider/key")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"deepseek-flash"},{"id":"deepseek-v4-pro"}]}`)), Header: http.Header{}}, nil
	})}
	h2 := httptest.NewServer(restarted)
	defer h2.Close()
	status, body := request(t, h2, "GET", "/api/assistant", nil)
	if status != 200 || !strings.Contains(string(body), "deepseek-v4-pro") || strings.Contains(string(body), "test-assistant-secret") {
		t.Fatal("saved model missing or key exposed")
	}
	status, body = request(t, h2, "POST", "/api/assistant/test", map[string]string{})
	if status != 200 || !strings.Contains(string(body), `"ok":true`) || !strings.Contains(string(body), "deepseek-v4-pro") || strings.Contains(string(body), "test-assistant-secret") {
		t.Fatal("saved model test failed or exposed key")
	}
}

func TestAssistantConnectionFailureDoesNotExposeProviderBody(t *testing.T) {
	s, h := newTestServer(t)
	request(t, h, "PUT", "/api/assistant", map[string]string{"model": "deepseek-flash", "api_key": "private-test-key"})
	s.client = &http.Client{Transport: mockRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`private-test-key rejected`)), Header: http.Header{}}, nil
	})}
	status, body := request(t, h, "POST", "/api/assistant/test", map[string]string{})
	if status != 502 || strings.Contains(string(body), "private-test-key") {
		t.Fatal("provider rejection hidden as success or key exposed")
	}
}
