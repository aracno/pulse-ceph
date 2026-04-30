# Pulse Ceph Alerting

This fork adds first-class alerting for Ceph cluster health and OSD state while
keeping the existing Pulse alert model: alert rules are derived from monitored
resource state, can be globally disabled by resource family, can be disabled per
resource, and resolve automatically when the monitored condition clears.

## Scope

Ceph alerting covers:

- cluster health state, such as `HEALTH_WARN` and `HEALTH_ERR`
- OSD availability state, based on `numOsds`, `numOsdsUp`, and `numOsdsIn`
- Proxmox API based Ceph data collected during the regular polling cycle
- host-agent Ceph data when an agent reports Ceph state
- global and per-cluster alert disable controls in the Alerts threshold UI

It does not introduce manual percentage thresholds for Ceph health. Ceph already
publishes an explicit health state, so Pulse follows that source of truth.

## Runtime Behavior

During each monitoring cycle, Pulse refreshes Ceph data for a Proxmox instance
after storage polling. If Ceph is detected, the backend builds a `CephCluster`
model and evaluates the alert rules immediately.

If Ceph is no longer detected for an instance, Pulse clears stale Ceph alerts for
that instance. This keeps removed or disabled Ceph clusters from leaving old
alerts visible until the generic stale-alert cleanup runs.

Host agents can also report Ceph data. When a host report includes Ceph state,
Pulse updates the global Ceph cluster state and evaluates the same Ceph alert
rules.

## Alert Rules

### Ceph Health

Alert type: `ceph-health`

Resolution condition:

- `health` is empty
- `health` is `OK`
- `health` is `HEALTH_OK`

Warning condition:

- any non-OK health value that is not classified as critical
- typical example: `HEALTH_WARN`

Critical condition:

- `HEALTH_ERR`
- `ERR`
- health values containing `ERROR`

The alert message includes `healthMessage` when Proxmox provides one.

### OSD State

Alert type: `ceph-osd-state`

Resolution condition:

- no OSD data is available, or
- all OSDs are up and in

Warning condition:

- one or more OSDs are out, with no OSDs down

Critical condition:

- one or more OSDs are down

Pulse computes:

```text
osdsDown = max(0, numOsds - numOsdsUp)
osdsOut  = max(0, numOsds - numOsdsIn)
```

The alert metadata includes the OSD counts, Ceph FSID, clear condition, and last
Ceph update timestamp.

## Alert Configuration

The global alert configuration supports:

```json
{
  "disableAllCeph": false
}
```

Per-cluster overrides use the cluster resource ID as the key:

```json
{
  "overrides": {
    "aoostar-pve01": {
      "disabled": true
    }
  }
}
```

When `disableAllCeph` is enabled, all Ceph health and OSD-state alerts are
cleared and no new Ceph alerts are emitted until the flag is disabled again.

When a single Ceph resource override has `disabled: true`, only that cluster's
Ceph alerts are cleared and suppressed.

## UI Integration

The Alerts threshold page includes a `Ceph` section under the Proxmox tab when
at least one Ceph cluster is present or configured through overrides.

The section shows:

- cluster display name
- current Ceph health
- OSD summary in the form `<up>/<total> up, <in>/<total> in`
- active alert indicators through the existing alert table behavior
- a global Ceph alert toggle in the section header
- a per-cluster disable toggle in the resource row

Ceph rows are intentionally not editable for numeric thresholds. The only
resource-level control is enable/disable, matching the state-based nature of
Ceph health alerts.

## Local Docker Deployment

The local development container is expected to run without mock data and to keep
real configuration in the named Docker volume:

```powershell
docker build --target runtime -t pulse-ceph:dev .

docker rm -f pulse-ceph

docker run -d `
  --name pulse-ceph `
  --restart unless-stopped `
  -p 7655:7655 `
  -v pulse-ceph-data:/data `
  -e TZ=Asia/Baku `
  pulse-ceph:dev
```

The `pulse-ceph-data` volume is not deleted by this workflow. It preserves the
configured Proxmox/PBS credentials, alert configuration, history, and real
homelab data.

Health check:

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:7655/api/health
```

## Development Workflow

The project can be built and tested from WSL2. The current local setup uses Go
and Node installed in the WSL user toolchain:

```bash
export TZ=UTC
export PATH=$HOME/.local/toolchains/go1.25.1/bin:$HOME/.local/toolchains/node-v20.19.6-linux-x64/bin:/usr/bin:/bin
```

Backend tests:

```bash
go test ./internal/alerts ./internal/monitoring
go test $(go list ./... | grep -v "/tmp$")
```

Frontend tests:

```bash
cd frontend-modern
npm ci
npm run type-check
npm run lint
npm test -- --run
```

Before a Docker build from Windows/Docker Desktop, remove the WSL-created
`frontend-modern/node_modules` directory if it exists:

```bash
rm -rf frontend-modern/node_modules
```

This keeps the Docker build context clean and avoids Windows/WSL filesystem
permission issues.

## Files Touched

Backend:

- `internal/alerts/alerts.go`
- `internal/alerts/ceph_test.go`
- `internal/monitoring/monitor.go`
- `internal/monitoring/monitor_polling.go`

Frontend:

- `frontend-modern/src/components/Alerts/ThresholdsTable.tsx`
- `frontend-modern/src/components/Alerts/ResourceTable.tsx`
- `frontend-modern/src/components/Alerts/Thresholds/types.ts`
- `frontend-modern/src/pages/Alerts.tsx`
- `frontend-modern/src/types/alerts.ts`

## Operational Notes

- Ceph alerting depends on the Ceph data already collected by Pulse.
- If the Proxmox API token cannot read Ceph status, the Ceph section may be
  empty and no Ceph alerts will be evaluated.
- API routes may require authentication; unauthenticated checks against
  `/api/state`, `/api/alerts/active`, or `/api/alerts/config` can return `401`.
- The initial alert evaluation happens during the monitoring poll cycle. A
  freshly restarted container may need a short moment before all Ceph state is
  visible in the UI.

## Troubleshooting

### No Ceph Section Appears

Check that the configured Proxmox instance actually exposes Ceph status to the
Pulse API token. Also confirm Ceph is not disabled for the instance.

Useful logs:

```powershell
docker logs --tail 200 pulse-ceph
```

### Ceph Health Is Visible But No Alert Fires

Verify global and per-resource disable settings:

- `disableAllCeph` must be `false`
- the cluster override must not contain `disabled: true`
- alert activation state must allow notifications if you are testing delivery

### OSD Alert Does Not Fire

Pulse needs valid OSD counts. If `numOsds` is zero or unavailable, Pulse clears
the OSD-state alert because it cannot safely infer degradation.

### Tests Fail Around Time Zones

Run backend tests with `TZ=UTC`. Some existing tests assert parsed times and are
sensitive to the local timezone.
