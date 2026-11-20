package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsRegistryRenderAndServeHTTP(t *testing.T) {
	reg := NewRegistry()
	reg.RecordDeploy("web", "git", "success", 1500*time.Millisecond)
	reg.RecordDeploy("web", "registry", "failure", 500*time.Millisecond)
	reg.RecordRollback("web")
	reg.RecordPollError("web", "git")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	reg.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("unexpected content-type: %s", ct)
	}

	body := w.Body.String()
	expected := []string{
		`docker_works_deployments_total{app="web",trigger="git",status="success"} 1`,
		`docker_works_deployments_total{app="web",trigger="registry",status="failure"} 1`,
		`docker_works_rollbacks_total{app="web"} 1`,
		`docker_works_poll_errors_total{app="web",watcher="git"} 1`,
		`docker_works_last_deploy_success{app="web"} 0`,
		`docker_works_last_deployment_duration_seconds{app="web"} 0.500`,
	}
	for _, exp := range expected {
		if !strings.Contains(body, exp) {
			t.Errorf("expected metric line %q in output:\n%s", exp, body)
		}
	}
}
