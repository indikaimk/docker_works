package compose

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/indikaimk/docker_works/internal/config"
	"gopkg.in/yaml.v3"
)

func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"default": func(fallback, val any) any {
			if val == nil || val == "" {
				return fallback
			}
			return val
		},
		"join":  strings.Join,
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"trim":  strings.TrimSpace,
		"env":   os.Getenv,
	}
}

func (s *Service) renderTemplates(app *config.AppConfig) error {
	if app.Templates == nil || len(app.Templates.Files) == 0 {
		return nil
	}

	data := make(map[string]any)

	// 1. Load and merge vars_files in order
	for _, vf := range app.Templates.VarsFiles {
		varPath := vf
		if !filepath.IsAbs(varPath) {
			varPath = filepath.Join(app.WorkDir, vf)
		}
		raw, err := os.ReadFile(varPath)
		if err != nil {
			return fmt.Errorf("failed to read template vars file %s: %w", vf, err)
		}
		var fileVars map[string]any
		if err := yaml.Unmarshal(raw, &fileVars); err != nil {
			return fmt.Errorf("failed to parse template vars file %s: %w", vf, err)
		}
		for k, v := range fileVars {
			data[k] = v
		}
	}

	// 2. Inline vars override vars_files
	for k, v := range app.Templates.Vars {
		data[k] = v
	}

	// 3. Render each template file atomically
	for _, tf := range app.Templates.Files {
		srcPath := filepath.Join(app.WorkDir, tf.Source)
		dstPath := filepath.Join(app.WorkDir, tf.Target)

		srcBytes, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", tf.Source, err)
		}

		tmpl, err := template.New(filepath.Base(tf.Source)).
			Funcs(templateFuncMap()).
			Option("missingkey=error").
			Parse(string(srcBytes))
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", tf.Source, err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return fmt.Errorf("failed to render template %s: %w", tf.Source, err)
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return fmt.Errorf("failed to create target directory for %s: %w", tf.Target, err)
		}

		tmpFile, err := os.CreateTemp(filepath.Dir(dstPath), ".tmpl-tmp-*")
		if err != nil {
			return fmt.Errorf("failed to create temp file for %s: %w", tf.Target, err)
		}
		tmpPath := tmpFile.Name()
		if err := tmpFile.Chmod(0644); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		if _, err := tmpFile.Write(buf.Bytes()); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		if err := tmpFile.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		if err := os.Rename(tmpPath, dstPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to write rendered template %s: %w", tf.Target, err)
		}

		s.logger.Info("Rendered Go template", "app", app.Name, "source", tf.Source, "target", tf.Target)
	}

	return nil
}
