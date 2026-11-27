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

func isTimerFile(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".timer")
}

func discoverQuadletFiles(workDir string, explicit []string) (quadletFiles []string, timerFiles []string, err error) {
	if len(explicit) > 0 {
		for _, f := range explicit {
			p := f
			if !filepath.IsAbs(p) {
				p = filepath.Join(workDir, f)
			}
			if _, err := os.Stat(p); err != nil {
				return nil, nil, fmt.Errorf("quadlet file %q not found: %w", f, err)
			}
			if isTimerFile(p) {
				timerFiles = append(timerFiles, p)
			} else {
				quadletFiles = append(quadletFiles, p)
			}
		}
	} else {
		entries, err := os.ReadDir(workDir)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read quadlet work_dir %s: %w", workDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if isTimerFile(name) {
				timerFiles = append(timerFiles, filepath.Join(workDir, name))
			} else if isQuadletFile(name) {
				quadletFiles = append(quadletFiles, filepath.Join(workDir, name))
			}
		}
	}

	if len(quadletFiles) == 0 && len(timerFiles) == 0 {
		return nil, nil, fmt.Errorf("no quadlet or timer files (*.container, *.pod, *.kube, *.network, *.volume, *.image, *.build, *.timer) found in %s", workDir)
	}

	sort.Slice(quadletFiles, func(i, j int) bool {
		ei := quadletExtOrder[strings.ToLower(filepath.Ext(quadletFiles[i]))]
		ej := quadletExtOrder[strings.ToLower(filepath.Ext(quadletFiles[j]))]
		if ei != ej {
			return ei < ej
		}
		return filepath.Base(quadletFiles[i]) < filepath.Base(quadletFiles[j])
	})
	sort.Slice(timerFiles, func(i, j int) bool {
		return filepath.Base(timerFiles[i]) < filepath.Base(timerFiles[j])
	})

	return quadletFiles, timerFiles, nil
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

func parseTimerTarget(filePath string) string {
	data, err := os.ReadFile(filePath)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "[") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == "Unit" {
				if val := strings.TrimSpace(parts[1]); val != "" {
					return val
				}
			}
		}
	}
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base)) + ".service"
}

type quadletPlan struct {
	quadletFiles []string
	timerFiles   []string
	services     []string
	timers       []string
	images       []string
}

func resolveQuadletPlan(app *config.AppConfig) (*quadletPlan, error) {
	var explicitFiles []string
	var explicitServices []string
	if app.Quadlet != nil {
		explicitFiles = app.Quadlet.Files
		explicitServices = app.Quadlet.Services
	}

	quadletFiles, timerFiles, err := discoverQuadletFiles(app.WorkDir, explicitFiles)
	if err != nil {
		return nil, err
	}

	plan := &quadletPlan{
		quadletFiles: quadletFiles,
		timerFiles:   timerFiles,
	}

	timerOwnedServices := make(map[string]struct{})
	for _, tf := range timerFiles {
		plan.timers = append(plan.timers, filepath.Base(tf))
		timerOwnedServices[parseTimerTarget(tf)] = struct{}{}
	}

	seenImg := make(map[string]struct{})
	seenSvc := make(map[string]struct{})

	for _, f := range quadletFiles {
		svcOverride, imgs := parseQuadletMetadata(f)
		for _, img := range imgs {
			if _, ok := seenImg[img]; !ok {
				seenImg[img] = struct{}{}
				plan.images = append(plan.images, img)
			}
		}
		if len(explicitServices) == 0 {
			svc := deriveQuadletServiceName(f, svcOverride)
			if _, ownedByTimer := timerOwnedServices[svc]; ownedByTimer {
				continue
			}
			if _, ok := seenSvc[svc]; !ok {
				seenSvc[svc] = struct{}{}
				plan.services = append(plan.services, svc)
			}
		}
	}

	if len(explicitServices) > 0 {
		for _, s := range explicitServices {
			if !strings.HasSuffix(s, ".service") && !strings.HasSuffix(s, ".timer") {
				s += ".service"
			}
			if strings.HasSuffix(s, ".timer") {
				plan.timers = append(plan.timers, s)
			} else {
				plan.services = append(plan.services, s)
			}
		}
	}

	return plan, nil
}

// resolveQuadletRelativePaths rewrites portable "./..." source paths in Volume, EnvironmentFile,
// Yaml, ConfigMap, and SeccompProfile directives to absolute paths under workDir.
func resolveQuadletRelativePaths(content, workDir string) string {
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		absWorkDir = workDir
	}

	resolveRel := func(rel string) (string, bool) {
		if !strings.HasPrefix(rel, "./") {
			return rel, false
		}
		cleaned := filepath.Clean(rel)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return rel, false
		}
		return filepath.Join(absWorkDir, cleaned), true
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "[") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		switch key {
		case "Volume":
			parts := strings.SplitN(val, ":", 2)
			if resolved, ok := resolveRel(parts[0]); ok {
				if len(parts) == 2 {
					lines[i] = key + "=" + resolved + ":" + parts[1]
				} else {
					lines[i] = key + "=" + resolved
				}
			}
		case "EnvironmentFile":
			prefix := ""
			target := val
			if strings.HasPrefix(val, "-") {
				prefix = "-"
				target = val[1:]
			}
			if resolved, ok := resolveRel(target); ok {
				lines[i] = key + "=" + prefix + resolved
			}
		case "Yaml", "ConfigMap", "SeccompProfile":
			if resolved, ok := resolveRel(val); ok {
				lines[i] = key + "=" + resolved
			}
		}
	}

	return strings.Join(lines, "\n")
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
		resolved := resolveQuadletRelativePaths(string(data), app.WorkDir)
		dst := filepath.Join(targetDir, filepath.Base(src))
		if err := os.WriteFile(dst, []byte(resolved), 0644); err != nil {
			return fmt.Errorf("failed to write quadlet file %s: %w", dst, err)
		}
	}
	return nil
}

func syncTimerFiles(app *config.AppConfig, timerFiles []string) error {
	if len(timerFiles) == 0 {
		return nil
	}
	systemdDir := app.Quadlet.SystemdDir
	if systemdDir == "" {
		if app.Quadlet.IsUserMode() {
			systemdDir = filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
		} else {
			systemdDir = "/etc/systemd/system"
		}
	}
	if err := os.MkdirAll(systemdDir, 0755); err != nil {
		return fmt.Errorf("failed to create systemd unit dir %s: %w", systemdDir, err)
	}
	for _, src := range timerFiles {
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("failed to read timer file %s: %w", src, err)
		}
		dst := filepath.Join(systemdDir, filepath.Base(src))
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("failed to write timer file %s: %w", dst, err)
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

func (s *Service) waitForQuadletHealth(ctx context.Context, app *config.AppConfig, userMode bool, units []string, timeout time.Duration) error {
	if len(units) == 0 {
		return nil
	}
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

		allHealthy := true
		for _, unit := range units {
			args := systemctlArgs(userMode, "show", unit, "--property=Type,ActiveState,SubState,Result", "--value")
			out, err := s.runner.Run(ctx, app.WorkDir, nil, "systemctl", args...)
			if err != nil {
				allHealthy = false
				continue
			}

			lines := strings.Split(strings.TrimSpace(out), "\n")
			if len(lines) < 4 {
				allHealthy = false
				continue
			}
			unitType := strings.TrimSpace(lines[0])
			activeState := strings.TrimSpace(lines[1])
			subState := strings.TrimSpace(lines[2])
			result := strings.TrimSpace(lines[3])

			if activeState == "failed" || (activeState == "inactive" && result != "" && result != "success") {
				return fmt.Errorf("quadlet unit %s failed (active=%s, sub=%s, result=%s)", unit, activeState, subState, result)
			}
			// Completed oneshot batch tasks transition to inactive with Result=success.
			if unitType == "oneshot" && activeState == "inactive" && result == "success" {
				continue
			}
			if activeState != "active" {
				allHealthy = false
			}
		}

		if allHealthy {
			s.logger.Info("All quadlet units reported healthy/active", "app", app.Name, "units", units)
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
	plan, err := resolveQuadletPlan(app)
	if err != nil {
		return err
	}

	if pullImages && len(plan.images) > 0 {
		for _, img := range plan.images {
			s.logger.Info("Pulling quadlet image", "app", app.Name, "image", img)
			if out, err := s.runner.Run(ctx, app.WorkDir, nil, "podman", "pull", img); err != nil {
				s.logger.Warn("podman pull returned error (might be using local image)", "app", app.Name, "image", img, "output", out, "error", err)
			}
		}
	}

	s.logger.Info("Syncing quadlet files", "app", app.Name, "target_dir", filepath.Join(app.Quadlet.Dir, app.Name), "count", len(plan.quadletFiles))
	if err := syncQuadletFiles(app, plan.quadletFiles); err != nil {
		return err
	}
	if err := syncTimerFiles(app, plan.timerFiles); err != nil {
		return err
	}

	userMode := app.Quadlet.IsUserMode()

	s.logger.Info("Reloading systemd daemon for quadlet units", "app", app.Name, "user_mode", userMode)
	if out, err := s.runner.Run(ctx, app.WorkDir, nil, "systemctl", systemctlArgs(userMode, "daemon-reload")...); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %w, output: %s", err, out)
	}

	if len(plan.services) > 0 {
		restartArgs := systemctlArgs(userMode, append([]string{"restart"}, plan.services...)...)
		s.logger.Info("Restarting quadlet units", "app", app.Name, "services", plan.services)
		if out, err := s.runner.Run(ctx, app.WorkDir, nil, "systemctl", restartArgs...); err != nil {
			return fmt.Errorf("systemctl restart failed: %w, output: %s", err, out)
		}
	}

	if len(plan.timers) > 0 {
		enableArgs := systemctlArgs(userMode, append([]string{"enable", "--now"}, plan.timers...)...)
		s.logger.Info("Enabling quadlet timer units", "app", app.Name, "timers", plan.timers)
		if out, err := s.runner.Run(ctx, app.WorkDir, nil, "systemctl", enableArgs...); err != nil {
			return fmt.Errorf("systemctl enable timers failed: %w, output: %s", err, out)
		}
		restartTimerArgs := systemctlArgs(userMode, append([]string{"restart"}, plan.timers...)...)
		if out, err := s.runner.Run(ctx, app.WorkDir, nil, "systemctl", restartTimerArgs...); err != nil {
			return fmt.Errorf("systemctl restart timers failed: %w, output: %s", err, out)
		}
	}

	unitsToCheck := append([]string{}, plan.services...)
	unitsToCheck = append(unitsToCheck, plan.timers...)
	return s.waitForQuadletHealth(ctx, app, userMode, unitsToCheck, app.HealthCheckTimeout)
}

func (s *Service) deployQuadlet(ctx context.Context, app *config.AppConfig) error {
	s.logger.Info("Starting quadlet deployment for app", "app", app.Name, "work_dir", app.WorkDir)

	if err := os.MkdirAll(app.WorkDir, 0755); err != nil {
		return fmt.Errorf("failed to create app workdir %s: %w", app.WorkDir, err)
	}

	if err := s.decryptSopsFiles(ctx, app); err != nil {
		return err
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

	if err := s.decryptSopsFiles(ctx, app); err != nil {
		return fmt.Errorf("failed to decrypt sops secrets during quadlet rollback: %w", err)
	}

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
