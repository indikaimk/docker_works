package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type deployKey struct {
	app     string
	trigger string
	status  string
}

type pollErrKey struct {
	app     string
	watcher string
}

// Registry collects deployment, rollback, and watcher metrics in Prometheus text format.
type Registry struct {
	mu               sync.RWMutex
	deployments      map[deployKey]uint64
	rollbacks        map[string]uint64
	pollErrors       map[pollErrKey]uint64
	lastSuccess      map[string]float64
	lastDeployTS     map[string]float64
	lastDurationSecs map[string]float64
}

func NewRegistry() *Registry {
	return &Registry{
		deployments:      make(map[deployKey]uint64),
		rollbacks:        make(map[string]uint64),
		pollErrors:       make(map[pollErrKey]uint64),
		lastSuccess:      make(map[string]float64),
		lastDeployTS:     make(map[string]float64),
		lastDurationSecs: make(map[string]float64),
	}
}

func (r *Registry) RecordDeploy(app, trigger, status string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.deployments[deployKey{app: app, trigger: trigger, status: status}]++
	r.lastDurationSecs[app] = duration.Seconds()
	if status == "success" {
		r.lastSuccess[app] = 1
		r.lastDeployTS[app] = float64(time.Now().Unix())
	} else {
		r.lastSuccess[app] = 0
	}
}

func (r *Registry) RecordRollback(app string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rollbacks[app]++
}

func (r *Registry) RecordPollError(app, watcher string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pollErrors[pollErrKey{app: app, watcher: watcher}]++
}

func escapeLabel(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

// Render formats all collected metrics in Prometheus text exposition format (0.0.4).
func (r *Registry) Render() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var b strings.Builder

	b.WriteString("# HELP docker_works_deployments_total Total number of container deployments.\n")
	b.WriteString("# TYPE docker_works_deployments_total counter\n")
	dKeys := make([]deployKey, 0, len(r.deployments))
	for k := range r.deployments {
		dKeys = append(dKeys, k)
	}
	sort.Slice(dKeys, func(i, j int) bool {
		if dKeys[i].app != dKeys[j].app {
			return dKeys[i].app < dKeys[j].app
		}
		if dKeys[i].trigger != dKeys[j].trigger {
			return dKeys[i].trigger < dKeys[j].trigger
		}
		return dKeys[i].status < dKeys[j].status
	})
	for _, k := range dKeys {
		fmt.Fprintf(&b, "docker_works_deployments_total{app=%q,trigger=%q,status=%q} %d\n",
			escapeLabel(k.app), escapeLabel(k.trigger), escapeLabel(k.status), r.deployments[k])
	}

	b.WriteString("# HELP docker_works_rollbacks_total Total number of automated rollbacks.\n")
	b.WriteString("# TYPE docker_works_rollbacks_total counter\n")
	for _, app := range sortedKeys(r.rollbacks) {
		fmt.Fprintf(&b, "docker_works_rollbacks_total{app=%q} %d\n", escapeLabel(app), r.rollbacks[app])
	}

	b.WriteString("# HELP docker_works_poll_errors_total Total number of watcher polling errors.\n")
	b.WriteString("# TYPE docker_works_poll_errors_total counter\n")
	pKeys := make([]pollErrKey, 0, len(r.pollErrors))
	for k := range r.pollErrors {
		pKeys = append(pKeys, k)
	}
	sort.Slice(pKeys, func(i, j int) bool {
		if pKeys[i].app != pKeys[j].app {
			return pKeys[i].app < pKeys[j].app
		}
		return pKeys[i].watcher < pKeys[j].watcher
	})
	for _, k := range pKeys {
		fmt.Fprintf(&b, "docker_works_poll_errors_total{app=%q,watcher=%q} %d\n",
			escapeLabel(k.app), escapeLabel(k.watcher), r.pollErrors[k])
	}

	b.WriteString("# HELP docker_works_last_deploy_success Whether the last deployment succeeded (1) or failed (0).\n")
	b.WriteString("# TYPE docker_works_last_deploy_success gauge\n")
	for _, app := range sortedKeys(r.lastSuccess) {
		fmt.Fprintf(&b, "docker_works_last_deploy_success{app=%q} %.0f\n", escapeLabel(app), r.lastSuccess[app])
	}

	b.WriteString("# HELP docker_works_last_deploy_timestamp_seconds Unix timestamp of the last successful deployment.\n")
	b.WriteString("# TYPE docker_works_last_deploy_timestamp_seconds gauge\n")
	for _, app := range sortedKeys(r.lastDeployTS) {
		fmt.Fprintf(&b, "docker_works_last_deploy_timestamp_seconds{app=%q} %.0f\n", escapeLabel(app), r.lastDeployTS[app])
	}

	b.WriteString("# HELP docker_works_last_deployment_duration_seconds Duration of the last deployment in seconds.\n")
	b.WriteString("# TYPE docker_works_last_deployment_duration_seconds gauge\n")
	for _, app := range sortedKeys(r.lastDurationSecs) {
		fmt.Fprintf(&b, "docker_works_last_deployment_duration_seconds{app=%q} %.3f\n", escapeLabel(app), r.lastDurationSecs[app])
	}

	return b.String()
}

func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(r.Render()))
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
