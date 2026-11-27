package compose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/indikaimk/docker_works/internal/config"
)

func (s *Service) decryptSopsFiles(ctx context.Context, app *config.AppConfig) error {
	if app.Sops == nil || len(app.Sops.Files) == 0 {
		return nil
	}

	var env []string
	if app.Sops.AgeKeyFile != "" {
		env = append(env, "SOPS_AGE_KEY_FILE="+app.Sops.AgeKeyFile)
	}
	if app.Sops.AgeKey != "" {
		env = append(env, "SOPS_AGE_KEY="+app.Sops.AgeKey)
	}

	for _, sf := range app.Sops.Files {
		srcPath := filepath.Join(app.WorkDir, sf.Source)
		dstPath := filepath.Join(app.WorkDir, sf.Target)

		args := []string{"--decrypt"}
		if sf.OutputType != "" {
			args = append(args, "--output-type", sf.OutputType)
		}
		args = append(args, "--", srcPath)

		s.logger.Info("Decrypting SOPS secret file", "app", app.Name, "source", sf.Source, "target", sf.Target)
		plaintext, err := s.runner.Run(ctx, app.WorkDir, env, "sops", args...)
		if err != nil {
			return fmt.Errorf("sops decryption failed for %s: %w", sf.Source, err)
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return fmt.Errorf("failed to create target directory for %s: %w", sf.Target, err)
		}

		tmpFile, err := os.CreateTemp(filepath.Dir(dstPath), ".sops-tmp-*")
		if err != nil {
			return fmt.Errorf("failed to create temp file for %s: %w", sf.Target, err)
		}
		tmpPath := tmpFile.Name()
		if err := tmpFile.Chmod(0600); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to set 0600 permissions on %s: %w", sf.Target, err)
		}
		if _, err := tmpFile.WriteString(plaintext); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to write decrypted secrets to %s: %w", sf.Target, err)
		}
		if err := tmpFile.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		if err := os.Rename(tmpPath, dstPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to finalize decrypted file %s: %w", sf.Target, err)
		}
	}

	return nil
}
