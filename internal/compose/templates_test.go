package compose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/indikaimk/docker_works/internal/config"
)

func TestRenderTemplatesPrometheusExample(t *testing.T) {
	workDir := t.TempDir()

	varsYAML := `
ericsson_dcn:
  - hostname: "dcn-01.example.com:9100"
  - hostname: "dcn-02.example.com:9100"
`
	if err := os.WriteFile(filepath.Join(workDir, "vars.yaml"), []byte(varsYAML), 0644); err != nil {
		t.Fatal(err)
	}

	tmplContent := `scrape_configs:
  - job_name: "prometheus"
    static_configs:
      - targets: ["127.0.0.1:9090"]
  - job_name: "Ericsson DCN"
    static_configs:
      - targets:
        {{- range .ericsson_dcn }}
        - {{ .hostname }}
        {{- end }}
`
	if err := os.WriteFile(filepath.Join(workDir, "prometheus.yml.tmpl"), []byte(tmplContent), 0644); err != nil {
		t.Fatal(err)
	}

	runner := &mockRunner{}
	service := NewService(runner, nil)

	app := &config.AppConfig{
		Name:               "prom-app",
		WorkDir:            workDir,
		ComposeFiles:       []string{"docker-compose.yml"},
		HealthCheckTimeout: 2 * time.Second,
		Templates: &config.TemplatesConfig{
			VarsFiles: []string{"vars.yaml"},
			Files: []config.TemplateFileSpec{
				{
					Source: "prometheus.yml.tmpl",
					Target: "prometheus.yml",
				},
			},
		},
	}

	if err := service.Deploy(context.Background(), app, "docker compose"); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	rendered, err := os.ReadFile(filepath.Join(workDir, "prometheus.yml"))
	if err != nil {
		t.Fatalf("failed to read rendered prometheus.yml: %v", err)
	}

	for _, want := range []string{"- dcn-01.example.com:9100", "- dcn-02.example.com:9100"} {
		if !strings.Contains(string(rendered), want) {
			t.Errorf("expected %q in rendered output:\n%s", want, string(rendered))
		}
	}
}

func TestRenderTemplatesMissingKeyFails(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "bad.tmpl"), []byte("host={{ .missing_key }}"), 0644); err != nil {
		t.Fatal(err)
	}

	service := NewService(&mockRunner{}, nil)
	app := &config.AppConfig{
		Name:    "bad-tmpl",
		WorkDir: workDir,
		Templates: &config.TemplatesConfig{
			Files: []config.TemplateFileSpec{
				{Source: "bad.tmpl", Target: "bad.txt"},
			},
		},
	}

	if err := service.renderTemplates(app); err == nil {
		t.Fatalf("expected error for missing template key, got nil")
	}
}
