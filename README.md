# rpz-loader

`rpz-loader` is a small daemon that keeps PowerDNS RPZ zones up to date.

It supports:

- Managed (remote) RPZ zones: periodically fetch a zone file from a URL and load it into PowerDNS.
- Static RPZ zones: render a zone file from a set of rules and load it into PowerDNS.
- Prometheus metrics at `GET /metrics` (default listen address `:2112`).

## Prerequisites

- Go (see `go.mod`)
- PowerDNS tools installed on the host running this process:
  - `pdnsutil` must be available on `PATH`

`rpz-loader` shells out to `pdnsutil` to load zone files into PowerDNS.

## Configuration

The application reads a YAML config file from an explicit path passed to the program.

Config schema corresponds to `internal/config`.

Example `config.yaml`:

```yaml
data_dir: /var/lib/rpz-loader

rpzs:
  - name: example-rpz
    type: managed
    reload_schedule: "*/5 * * * *" # cron
    url: "https://example.com/example-rpz.zone"

  - name: static-rpz
    type: static
    ttl: 60
    rules:
      - trigger: bad.example.
        action: "." # NXDOMAIN
        include_subdomains: true
```

### Notes

- `data_dir` is where fetched/generated zone files are written.
- For managed RPZs, `reload_schedule` must be a valid cron expression.
- For static RPZs, `ttl` and `rules` are required.


## Metrics

Prometheus metrics are exposed at:

- `GET http://localhost:2112/metrics`

Metrics include:

- `rpz_loader_zone_reload_total{zone,result}`
- `rpz_loader_zone_reload_duration_seconds{zone}`


## Development

Add/refresh dependencies:

```bash
go mod tidy
```

Build:

```bash
go test ./...
```
