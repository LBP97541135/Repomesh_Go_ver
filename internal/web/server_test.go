package web

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestScaffoldDoesNotClaimBusinessReadiness(t *testing.T) {
	handler := newHandler(fstest.MapFS{"index.html": {Data: []byte("<h1>RepoMesh scaffold</h1>")}})
	for _, test := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/healthz", 200, `"businessReady":false`},
		{"GET", "/readyz", 503, "not_implemented"},
		{"POST", "/api/v1/issues", 404, "not_implemented"},
		{"GET", "/", 200, "RepoMesh scaffold"},
		{"GET", "/missing", 404, "404"},
		{"POST", "/", 405, "method not allowed"},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
			if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), test.body) {
				t.Fatalf("got %d %q; want %d containing %q", recorder.Code, recorder.Body.String(), test.status, test.body)
			}
		})
	}
}

func TestMissingAssetsFailsBeforeListening(t *testing.T) {
	if err := Run(context.Background(), "invalid address", t.TempDir()); err == nil || !strings.Contains(err.Error(), "frontend index.html missing") {
		t.Fatalf("expected missing frontend diagnostic, got %v", err)
	}
}

func TestShutdownWaitsForInflightRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})}
	defer server.Close()
	stopped := make(chan error, 1)
	go func() { stopped <- serve(ctx, server, listener) }()
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		response, requestErr := client.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			response.Body.Close()
		}
		requestDone <- requestErr
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter handler")
	}
	cancel()
	select {
	case err := <-stopped:
		t.Fatalf("server returned before the in-flight request completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release <- struct{}{}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not finish graceful shutdown")
	}
	if err := <-requestDone; err != nil {
		t.Fatalf("in-flight request failed: %v", err)
	}
}

func TestProjectBrowserRoutesUseOpaqueProjectIDs(t *testing.T) {
	for _, route := range []string{
		"/projects",
		"/projects/new",
		"/projects/project-项目",
		"/projects/project-项目/settings",
		"/project-creations/10000000-0000-1000-0000-000000000001",
		"/projects/project-项目/updates/ffffffff-ffff-ffff-ffff-ffffffffffff",
	} {
		if !projectBrowserRoute(route) {
			t.Fatalf("valid browser route rejected: %s", route)
		}
	}
	for _, route := range []string{
		"/projects/",
		"/projects/.",
		"/projects/..",
		"/projects/a%2Fb",
		"/projects/a\\b",
		"/projects/new/settings",
		"/project-creations/not-a-uuid",
		"/projects/project-项目/updates/not-a-uuid",
	} {
		if projectBrowserRoute(route) {
			t.Fatalf("invalid browser route accepted: %s", route)
		}
	}
}
