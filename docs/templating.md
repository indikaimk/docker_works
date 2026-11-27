# Templates (Go `text/template`)

DockerWorks includes a built-in, zero-dependency Go `text/template` renderer (`apps[].templates`) for generating configuration files such as `prometheus.yml`, `nginx.conf`, or `.env` from structured YAML/JSON variable files before container startup.

## Why Go `text/template`?

- **Zero external tools:** No Python or Jinja2 installation required on the target host.
- **Fail-fast & rollback-safe:** Templates are parsed with `missingkey=error`, so missing variables abort the deployment and trigger an automated rollback.
- **Works with SOPS & Rollbacks:** When paired with `apps[].sops`, encrypted variable files (e.g. `vars.enc.yaml`) are decrypted before templates render, and rollbacks re-render the previous commit/digest's templates.

---

## Configuration Example

```yaml
apps:
  - name: "prometheus"
    templates:
      vars_files:
        - "vars.yaml"                  # Relative to work_dir
        - "/etc/docker-works/vars.yaml" # Or host-specific vars outside Git
      vars:
        scrape_interval: "15s"         # Inline vars override vars_files
      files:
        - source: "prometheus.yml.tmpl"
          target: "prometheus.yml"     # Optional (auto-strips .tmpl/.tpl/.gotmpl)
```

---

## Converting from Jinja2

| Pattern | Jinja2 | Go `text/template` |
| :--- | :--- | :--- |
| Variable | `{{ item.hostname }}` | `{{ .hostname }}` (inside `range`) or `{{ .var_name }}` |
| Loop | `{% for item in ericsson_dcn %}...{% endfor %}` | `{{- range .ericsson_dcn }}...{{- end }}` |
| Conditional | `{% if env == "prod" %}...{% endif %}` | `{{- if eq .env "prod" }}...{{- end }}` |
| Default value | `{{ port \| default(8080) }}` | `{{ default 8080 .port }}` |

### Example: `prometheus.yml.tmpl`

With `vars.yaml`:

```yaml
ericsson_dcn:
  - hostname: "dcn-01.example.com:9100"
  - hostname: "dcn-02.example.com:9100"
```

And `prometheus.yml.tmpl`:

```yaml
scrape_configs:
  - job_name: "prometheus"
    static_configs:
      - targets: ["127.0.0.1:9090"]
  - job_name: "Ericsson DCN"
    static_configs:
      - targets:
        {{- range .ericsson_dcn }}
        - {{ .hostname }}
        {{- end }}
```

DockerWorks renders `prometheus.yml` atomically into `work_dir` right before `pre_deploy` hooks and Compose/Quadlet startup.

---

## Built-in Template Functions

| Function | Example | Description |
| :--- | :--- | :--- |
| `default` | `{{ default "15s" .interval }}` | Returns fallback if value is `nil` or `""`. |
| `join` | `{{ join "," .hosts }}` | Joins a string slice. |
| `upper` / `lower` | `{{ upper .env }}` | Converts case. |
| `trim` | `{{ trim .name }}` | Trims leading/trailing whitespace. |
| `env` | `{{ env "HOST_REGION" }}` | Reads a host environment variable. |
