# Contributing

## Local checks

Before opening a pull request, run:

```powershell
go fmt ./...
go vet ./...
go test ./...
go build ./cmd/uplinkwatch
docker compose -f deploy/docker-compose.yml config --quiet
```

Changes to metrics must update the Grafana dashboard, Prometheus rules, and
relevant documentation. Changes to alert behavior must include or update a
runbook.

Never commit browser-derived configuration, tokens, chat identifiers, cookies,
or response bodies from monitored sites.
