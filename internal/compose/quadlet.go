package compose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/indikaimk/docker_works/internal/config"
)

// quadletExtOrder defines the supported Quadlet extensions and their activation priority.
var quadletExtOrder = map[string]int{
	".network":   1,
	".volume":    2,
	".image":     3,
	".build":     4,
	".pod":       5,
	".kube":      6,
	".container": 7,
}

func isQuadletFile(name string) bool {
	_, ok := quadletExtOrder[strings.ToLower(filepath.Ext(name))]
	return ok
}

func discoverQuadletFiles(workDir string, explicit []string) ([]string, error) {
	var files []string
	if len(explicit) > 0 {
		for _, f := range explicit {
			p := f
			if !filepath.IsAbs(p) {
				p = filepath.Join(workDir, f)
			}
			if _, err := os.Stat(p); err != nil {
				return nil, fmt.Errorf("quadlet file %q not found: %w", f, err)
			}
			files = append(files, p)
		}
	} else {
		entries, err := os.ReadDir(workDir)
		if err != nil {
			return nil, fmt.Errorf("failed to read quadlet work_dir %s: %w", workDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if isQuadletFile(entry.Name()) {
				files = append(files, filepath.Join(workDir, entry.Name()))
			}
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no quadlet files (*.container, *.pod, *.kube, *.network, *.volume, *.image, *.build) found in %s", workDir)
	}

	sort.Slice(files, func(i, j int) bool {
		ei := quadletExtOrder[strings.ToLower(filepath.Ext(files[i]))]
		ej := quadletExtOrder[strings.ToLower(filepath.Ext(files[j]))]
		if ei != ej {
			return ei < ej
		}
		return filepath.Base(files[i]) < filepath.Base(files[j])
	})

	return files, nil
}

func parseQuadletMetadata(filePath string) (serviceName string, images []string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", nil
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "ServiceName":
			serviceName = val
		case "Image":
			if val != "" && !strings.HasSuffix(val, ".image") && !strings.HasSuffix(val, ".build") {
				images = append(images, val)
			}
		}
	}
	return serviceName, images
}

func deriveQuadletServiceName(filePath string, explicitServiceName string) string {
	if explicitServiceName != "" {
		if strings.HasSuffix(explicitServiceName, ".service") {
			return explicitServiceName
		}
		return explicitServiceName + ".service"
	}

	base := filepath.Base(filePath)
	ext := strings.ToLower(filepath.Ext(base))
	stem := strings.TrimSuffix(base, filepath.Ext(base))

	switch ext {
	case ".network":
		return stem + "-network.service"
	case ".volume":
		return stem + "-volume.service"
	case ".image":
		return stem + "-image.service"
	case ".build":
		return stem + "-build.service"
	case ".pod":
		return stem + "-pod.service"
	default:
		return stem + ".service"
	}
}

func resolveQuadletPlan(app *config.AppConfig) (files []string, services []string, images []string, err error) {
	var explicitFiles []string
	var explicitServices []string
	if app.Quadlet != nil {
		explicitFiles = app.Quadlet.Files
		explicitServices = app.Quadlet.Services
	}

	files, err = discoverQuadletFiles(app.WorkDir, explicitFiles)
	if err != nil {
		return nil, nil, nil, err
	}

	seenImg := make(map[string]struct{})
	seenSvc := make(map[string]struct{})

	for _, f := range files {
		svcOverride, imgs := parseQuadletMetadata(f)
		for _, img := range imgs {
			if _, ok := seenImg[img]; !ok {
				seenImg[img] = struct{}{}
				images = append(images, img)
			}
		}
		if len(explicitServices) == 0 {
			svc := deriveQuadletServiceName(f, svcOverride)
			if _, ok := seenSvc[svc]; !ok {
				seenSvc[svc] = struct{}{}
				services = append(services, svc)
			}
		}
	}

	if len(explicitServices) > 0 {
		for _, s := range explicitServices {
			if !strings.HasSuffix(s, ".service") {
				s += ".service"
			}
			services = append(services, s)
		}
	}

	return files, services, images, nil
}

func syncQuadletFiles(app *config.AppConfig, files []string) error {
	targetDir := filepath.Join(app.Quadlet.Dir, app.Name)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create quadlet target dir %s: %w", targetDir, err)
	}

	// Remove stale quadlet unit files from previous commits
	existing, err := os.ReadDir(targetDir)
	if err == nil {
		for _, entry := range existing {
			if !entry.IsDir() && isQuadletFile(entry.Name()) {
				_ = os.Remove(filepath.Join(targetDir, entry.Name()))
			}
		}
	}

	for _, src := range files {
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("failed to read quadlet file %s: %w", src, err)
		}
		dst := filepath.Join(targetDir, filepath.Base(src))
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("failed to write quadlet file %s: %w", dst, err)
		}
	}
	return nil
}

func systemctlArgs(userMode bool, args ...string) []string {
	if userMode {
		return append([]string{"--user"}, args...)
	}
	return args
}

func (s *Service) waitForQuadletHealth(ctx context.Context, app *config.AppConfig, userMode bool, services []string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	pollInterval := 2 * time.Second
	if timeout < 5*time.Second {
		pollInterval = 50 * time.Millisecond
	}

	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("quadlet health check timed out after %v", timeout)
		}

		allActive := true
		for _, svc := range services {
			args := systemctlArgs(userMode, "show", svc, "--property=ActiveState,SubState,Result", "--value")
			out, err := s.runner.Run(ctx, app.WorkDir, nil, "systemctl", args...)
			if err != nil {
				allActive = false
				continue
			}

			lines := strings.Split(strings.TrimSpace(out), "\n")
			if len(lines) < 3 {
				allActive = false
				continue
			}
			activeState := strings.TrimSpace(lines[0])
			subState := strings.TrimSpace(lines[1])
			result := strings.TrimSpace(lines[2])

			if activeState == "failed" || (activeState == "inactive" && result != "" && result != "success") {
				return fmt.Errorf("quadlet unit %s failed (active=%s, sub=%s, result=%s)", svc, activeState, subState, result)
			}
			if activeState != "active" {
				allActive = false
			}
		}

		if allActive {
			s.logger.Info("All quadlet units reported active", "app", app.Name, "services", services)
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (s *Service) applyQuadletUnits(ctx context.Context, app *config.AppConfig, pullImages bool) error {
	files, services, images, err := resolveQuadletPlan(app)
	if err != nil {
		return err
	}

	if pullImages && len(images) > 0 {
		for _, img := range images {
			s.logger.Info("Pulling quadlet image", "app", app.Name, "image", img)
			if out, err := s.runner.Run(ctx, app.WorkDir, nil, "podman", "pull", img); err != nil {
				s.logger.Warn("podman pull returned error (might be using local image)", "app", app.Name, "image", img, "output", out, "error", err)
			}
		}
	}

	s.logger.Info("Syncing quadlet files", "app", app.Name, "target_dir", filepath.Join(app.Quadlet.Dir, app.Name), "count", len(files))
	if err := syncQuadletFiles(app, files); err != nil {
		return err
	}

	userMode := app.Quadlet.IsUserMode()

	s.logger.Info("Reloading systemd daemon for quadlet units", "app", app.Name, "user_mode", userMode)
	if out, err := s.runner.Run(ctx, app.WorkDir, nil, "systemctl", systemctlArgs(userMode, "daemon-reload")...); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %w, output: %s", err, out)
	}

	restartArgs := systemctlArgs(userMode, append([]string{"restart"}, services...)...)
	s.logger.Info("Restarting quadlet units", "app", app.Name, "services", services)
	if out, err := s.runner.Run(ctx, app.WorkDir, nil, "systemctl", restartArgs...); err != nil {
		return fmt.Errorf("systemctl restart failed: %w, output: %s", err, out)
	}

	return s.waitForQuadletHealth(ctx, app, userMode, services, app.HealthCheckTimeout)
}

func (s *Service) deployQuadlet(ctx context.Context, app *config.AppConfig) error {
	s.logger.Info("Starting quadlet deployment for app", "app", app.Name, "work_dir", app.WorkDir)

	if err := os.MkdirAll(app.WorkDir, 0755); err != nil {
		return fmt.Errorf("failed to create app workdir %s: %w", app.WorkDir, err)
	}

	if app.Hooks.PreDeploy != "" {
		if err := s.RunHook(ctx, app.Hooks.PreDeploy, app.WorkDir, app.Name); err != nil {
			return fmt.Errorf("pre-deploy hook failed: %w", err)
		}
	}

	if err := s.applyQuadletUnits(ctx, app, true); err != nil {
		return err
	}

	if app.Hooks.PostDeploy != "" {
		if err := s.RunHook(ctx, app.Hooks.PostDeploy, app.WorkDir, app.Name); err != nil {
			return fmt.Errorf("post-deploy hook failed: %w", err)
		}
	}

	if app.AutoPrune {
		s.logger.Info("Pruning unused container images", "app", app.Name)
		if _, err := s.Prune(ctx, "podman"); err != nil {
			s.logger.Warn("Failed to prune container images", "error", err)
		}
	}

	s.logger.Info("Quadlet deployment successfully finished", "app", app.Name)
	return nil
}

func (s *Service) rollbackQuadlet(ctx context.Context, app *config.AppConfig) error {
	s.logger.Warn("Executing quadlet rollback for app", "app", app.Name, "work_dir", app.WorkDir)

	if app.Hooks.OnRollback != "" {
		if err := s.RunHook(ctx, app.Hooks.OnRollback, app.WorkDir, app.Name); err != nil {
			s.logger.Warn("on_rollback hook returned error", "app", app.Name, "error", err)
		}
	}

	if err := s.applyQuadletUnits(ctx, app, false); err != nil {
		return fmt.Errorf("quadlet rollback failed: %w", err)
	}

	s.logger.Info("Quadlet rollback completed and active", "app", app.Name)
	return nil
}
