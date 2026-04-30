# Devices Monitoring

This note captures the current shape of Pulse network device monitoring in the `pulse-ceph` fork.

## Scope

Devices covers manageable network hardware:

- UniFi consoles, gateways, switches, and access points.
- Generic switches, routers, modems, and appliances reachable through Ping or SNMP.

Docker remains supported internally by agents and backend data paths, but Docker management is intentionally hidden from the main UI in this fork.

## UI Workflow

The UI is organized around two concepts:

- Monitoring checks in `Settings -> Platforms -> Devices`.
- Device inventory in the top-level `Devices` tab.

Checks describe how data is collected:

- `Ping`: default baseline check for reachability, latency, and packet loss.
- `UniFi`: one or more UniFi Site Manager API checks.
- `SNMP`: one or more SNMP checks for managed network hardware.

Checks can be added, edited, enabled, disabled, and removed from Settings. Ping checks intentionally do not have a default target because the target belongs to each device entry.

The `Devices` tab contains an `Add device` wizard. The wizard first selects one configured check, then captures device identity and address details. For UniFi checks, the wizard calls Pulse backend discovery and lets the user select a returned device.

## Backend Persistence

Device monitoring is persisted server-side in:

```text
/data/devices.json
```

The store contains:

- Checks, including API/SNMP credentials and collection intervals.
- Managed device inventory.
- Device alert settings and override maps.
- Last check timestamps, last errors, and alert evaluation summary.

The frontend still migrates old local browser draft data once if it finds it, but the backend store is now the source of truth.

## Backend Collection Routines

Pulse starts a lightweight devices poller with the main API router. The poller wakes every 5 seconds and only runs devices whose selected check interval is due.

Current collectors:

- `Ping`: executes a single ICMP ping and records online/offline, latency, packet loss, and timestamps.
- `UniFi`: calls the official Site Manager `GET /v1/devices` endpoint through the backend allowlisted proxy, normalizes the response, and merges fresh identity/status data into managed devices.
- `SNMP`: performs a conservative UDP reachability check against port 161 as a first backend routine. Deeper OID polling can be added without changing the UI model.

Manual checks from the Devices page force the same backend poll path.

## UniFi API

Pulse uses the official UniFi Site Manager API surface:

- `GET https://api.ui.com/v1/devices`
- `GET https://api.ui.com/v1/hosts`
- `GET https://api.ui.com/v1/sites`

Authentication uses the `X-API-Key` header. Official responses are wrapped in a standard object with `data`, `httpStatusCode`, and `traceId`; UniFi also documents that fields can vary between versions, especially around nested payloads.

The backend normalizer is intentionally defensive. It walks `data`, `devices`, and `uidb` structures and extracts names from common fields such as `name`, `displayName`, `hostname`, `alias`, `reportedState.name`, and `meta.name`. This prevents the wizard from falling back to generic names like `UniFi device 1` when the API returns nested records.

Pulse intentionally exposes only an allowlisted UniFi proxy surface:

- Host: `https://api.ui.com`
- Endpoints: `/v1/devices`, `/v1/hosts`, `/v1/sites`, and the current EA ISP metrics windows.

## Device Alerts

Device alert configuration lives in `Alerts -> Thresholds -> Device Check Alerts`.

Configurable alert controls:

- Global device alert enable/disable.
- Offline alerts.
- Warning/degraded state alerts.
- Latency warning threshold.
- Packet loss warning threshold.
- Per-check alert enable/disable.
- Per-device alert enable/disable.

The backend evaluates these settings after each poll and persists the latest summary. This keeps the behavior aligned with the existing Pulse alerting model: broad family switches first, then targeted overrides.

## SNMP Roadmap

The SNMP routine currently validates reachability. The next pragmatic layer is to add standard OIDs for:

- Uptime.
- Interface status and throughput.
- CPU and memory when exposed by the device MIB.
- Temperature when exposed by the device MIB.

SNMPv3 should be preferred when available; SNMPv2c remains useful for homelab equipment that only exposes community-based polling.
