# Webhooks & Health Endpoints

When `server.enabled: true` is set in `config.yaml`, DockerWorks starts an HTTP server on `server.listen` (default `127.0.0.1:8080`).

## Health Endpoint

```http
GET /healthz
```

Returns `200 OK` with JSON:

```json
{"status":"ok"}
```

## Webhook Endpoint

```http
POST /webhooks/{app}
```

Triggers an asynchronous sync and deployment for `{app}` and returns `202 Accepted`:

```json
{
  "status": "triggered",
  "app": "my-app",
  "message": "Deployment initiated"
}
```

### Authentication

If `apps[].webhook.secret` or `server.webhook_secret` is configured (app-level secret takes precedence), DockerWorks validates the request using any of the following methods:

1. **GitHub HMAC-SHA256**: `X-Hub-Signature-256: sha256=<hex>`
2. **GitLab Secret Token**: `X-Gitlab-Token: <secret>`
3. **Bearer Token**: `Authorization: Bearer <secret>`

### GitHub Setup

1. In your GitHub repository, open **Settings > Webhooks > Add webhook**.
2. Set **Payload URL** to `https://your-domain.example/webhooks/{app}` (proxied to DockerWorks).
3. Set **Content type** to `application/json`.
4. Set **Secret** to your configured `webhook_secret`.

### Manual Trigger

```bash
curl -X POST "http://127.0.0.1:8080/webhooks/my-app" \
  -H "Authorization: Bearer my-webhook-secret"
```

## Prometheus Metrics Endpoint

When `server.enabled: true` and `server.metrics_enabled: true` are set, DockerWorks exposes Prometheus metrics in text exposition format at:

```http
GET /metrics
```

Exported metrics:
- `docker_works_deployments_total{app,trigger,status}`
- `docker_works_rollbacks_total{app}`
- `docker_works_poll_errors_total{app,watcher}`
- `docker_works_last_deploy_success{app}`
- `docker_works_last_deploy_timestamp_seconds{app}`
- `docker_works_last_deployment_duration_seconds{app}`

!!! tip "Keep `/metrics` internal"
    If you reverse-proxy `/webhooks/` to the public internet, only proxy `/webhooks/` and scrape `http://127.0.0.1:8080/metrics` locally from Prometheus.
