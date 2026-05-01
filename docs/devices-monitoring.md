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

- `Ping`: runs a small ICMP sample per device and records online/offline, average latency from successful probes, packet loss percentage, and timestamps.
- `UniFi`: calls the official Site Manager `GET /v1/devices` endpoint through the backend allowlisted proxy, measures API latency, normalizes the response, and merges fresh identity/status/telemetry data into managed devices.
- `SNMP`: uses the same ping sample for online/offline, latency, and packet loss, then performs SNMPv2c polling. Standard HOST-RESOURCES-MIB processor and memory OIDs are used when exposed. IF-MIB counters are sampled over time to calculate `eth0` RX/TX throughput.

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
- Latency is the collector-to-UniFi-API request latency, not ICMP latency to the physical device.
- CPU, memory, packet loss, and WAN RX/TX throughput are recorded only when the API payload exposes recognizable numeric fields.
- Packet loss is not synthesized for UniFi devices because the Site Manager device inventory does not guarantee a per-device loss metric.

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

## SNMP Notes

SNMP support starts with common vendor-neutral OIDs:

- CPU: HOST-RESOURCES-MIB `hrProcessorLoad`, averaged across processors.
- RAM: HOST-RESOURCES-MIB storage rows whose description contains memory/RAM.
- eth0 throughput: IF-MIB high-capacity octet counters when present, falling back to classic 32-bit octet counters.

If a device does not expose these standard MIBs or does not name its interface `eth0`, Pulse leaves the value empty instead of guessing. SNMPv3 credentials can be stored in the UI, but the backend polling path currently enables SNMPv2c/community collection first because that is the common homelab baseline.
