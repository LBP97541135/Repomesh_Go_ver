package agentteams

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkflowProxiesVerbatim(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"detail":"ambiguous"}`))
	}))
	defer upstream.Close()

	client := &Client{BaseURL: upstream.URL, Token: "sa-token"}
	body, status, err := client.Workflow(context.Background(), "pricing-01", "train-ticket-pricing")
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	if status != http.StatusConflict {
		t.Fatalf("status=%d; want 409 passthrough", status)
	}
	if string(body) != `{"detail":"ambiguous"}` {
		t.Fatalf("body=%q", body)
	}
	if !strings.HasSuffix(gotPath, "/api/v1/projects/pricing-01/workflow") {
		t.Fatalf("path=%q", gotPath)
	}
	if gotQuery != "includeTasks=true&team=train-ticket-pricing" {
		t.Fatalf("query=%q", gotQuery)
	}
	if gotAuth != "Bearer sa-token" {
		t.Fatalf("auth=%q", gotAuth)
	}
}
