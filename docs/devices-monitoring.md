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

- `Ping`: runs a small ICMP sample per device and records online/offline, average latency from successful probes, packet loss percentage, online streak uptime, and timestamps.
- `UniFi`: calls the official Site Manager `GET /v1/devices` endpoint through the backend allowlisted proxy, normalizes inventory/status/firmware/startup time, and enriches gateway latency/loss from ISP metrics when available.
- `SNMP`: uses the same ping sample for online/offline, latency, and packet loss, then performs SNMPv2c `sysUpTime` polling when available.

Manual checks from the Devices page force the same backend poll path.

## UniFi API

Pulse uses the official UniFi Site Manager API surface:

- `GET https://api.ui.com/v1/devices`
- `GET https://api.ui.com/v1/hosts`
- `GET https://api.ui.com/v1/sites`

Authentication uses the `X-API-Key` header. Official responses are wrapped in a standard object with `data`, `httpStatusCode`, and `traceId`; UniFi also documents that fields can vary between versions, especially around nested payloads.

The backend normalizer is intentionally defensive. It walks `data`, `devices`, and `uidb` structures and extracts names from common fields such as `name`, `displayName`, `hostname`, `alias`, `reportedState.name`, and `meta.name`. This prevents the wizard from falling back to generic names like `UniFi device 1` when the API returns nested records.

The telemetry model is deliberately conservative:

- Online/offline comes from the device state returned by the API.
- Uptime comes from UniFi `startupTime` when present.
- Gateway latency/loss comes from the UniFi ISP metrics endpoint, which publishes 5-minute interval metrics.
- Switch/AP latency and loss are left empty unless a reliable API metric exists.
- CPU, RAM, WAN throughput, and interface throughput are intentionally not collected in Devices because the current Site Manager API payloads do not expose them reliably.

Pulse intentionally exposes only an allowlisted UniFi proxy surface:

- Host: `https://api.ui.com`
- Endpoints: `/v1/devices`, `/v1/hosts`, `/v1/sites`, and the current EA ISP metrics windows.

## Device Alerts

Device alert configuration lives in `Alerts -> Thresholds -> Device Check Alerts`.

Configurable alert controls:

- Global device alert enable/disable.
- Offline alerts.
- Uptime minimum threshold.
- Latency warning threshold.
- Packet loss warning threshold.
- Per-check alert enable/disable.
- Per-device alert enable/disable.
- Per-device alert thresholds for offline, uptime, latency, and packet loss.

For the numeric thresholds, `0` disables that metric family. For example, setting
latency to `0` disables latency alerts while keeping offline and packet loss alerts
available.

Uptime alerts are evaluated as a low-uptime window: when enabled, an online device
alerts if its collected uptime is between `0` and the configured threshold. This
is meant to catch recent reboots without treating long-running devices as a
problem.

The backend evaluates these settings after each poll and persists the latest summary. This keeps the behavior aligned with the existing Pulse alerting model: broad family switches first, then targeted overrides.

## SNMP Notes

SNMP support is intentionally narrow:

- Reachability, latency, and packet loss come from the ping sample.
- Uptime comes from standard `sysUpTime` (`1.3.6.1.2.1.1.3.0`) when exposed.

If a device does not expose `sysUpTime`, Pulse leaves uptime empty instead of guessing. SNMPv3 credentials can be stored in the UI, but the backend polling path currently enables SNMPv2c/community collection first because that is the common homelab baseline.
