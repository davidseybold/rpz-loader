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
nameserver: ns1.example.internal         # SOA/NS name of the generated zones
hostmaster_email: hostmaster@example.internal
also_notify:                             # notified after every load (a list, or one host)
  - 192.168.5.21:53

rpzs:
  - name: example-rpz
    type: managed
    reload_schedule: "*/5 * * * *" # cron
    url: "https://example.com/example-rpz.zone"
    min_rules: 1000          # reject a download with fewer rules (default 1)
    max_shrink_percent: 50   # reject one this much smaller than the last good one (default 50; 100 = off)

  - name: static-rpz
    type: static
    ttl: 60
    rules:
      - trigger: bad.example.
        action: "." # NXDOMAIN
        include_subdomains: true
```

### Notes

- `data_dir` is where fetched/generated zone files are written. Files are replaced
  atomically, and only after a download passes its checks.
- Unknown keys are errors, so a misspelled key can't be ignored silently.
- A managed download is rejected, and the zone already loaded in PowerDNS is kept, when
  it is an HTML page, has fewer than `min_rules` rules, or has shrunk by more than
  `max_shrink_percent` since the last good one (read from the zone file after a restart).
- A failed sync (download, check, or PowerDNS) is reported as `result="fail"`.
- For managed RPZs, `reload_schedule` must be a valid cron expression.
- For static RPZs, `ttl` and `rules` are required.


## Metrics

Prometheus metrics are exposed at:

- `GET http://localhost:2112/metrics`

Metrics include:

- `rpz_loader_zone_reload_total{zone,result}`: `result` is `success` or `fail`
- `rpz_loader_zone_reload_duration_seconds{zone}`
- `rpz_loader_zone_last_success_timestamp_seconds{zone,type}`: when the zone was last
  loaded. Static zones load only at start, so alert on staleness with `type="managed"`.
- `rpz_loader_zone_rules{zone,type}`: rules in the zone as last loaded


## Development

Add/refresh dependencies:

```bash
go mod tidy
```

Test:

```bash
go test ./...
```
