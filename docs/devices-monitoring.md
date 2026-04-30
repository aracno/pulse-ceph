# Devices Monitoring

This note captures the intended shape of Pulse network device monitoring.

## Scope

Devices covers manageable network hardware:

- UniFi consoles, gateways, switches, and access points.
- Generic managed switches, routers, modems, and appliances reachable through SNMP.

Docker remains supported internally by agents and backend data paths, but Docker management is intentionally hidden from the main UI in this fork.

## Collection Strategy

Use two independent collectors:

- UniFi Site Manager API for UniFi deployments.
- SNMP for non-UniFi devices or local-only equipment.

The frontend Devices page expects normalized `ManagedDevice` records with:

- Identity: `id`, `name`, `hostname`, `ip`, `mac`, `model`, `vendor`, `type`, `site`.
- Source: `unifi`, `snmp`, or `manual`.
- Health: `status`, `cpuUsage`, `memoryUsage`, `temperatureC`, `uptime`, `firmwareVersion`, `lastSeen`.

## UniFi API

Prefer the official UniFi Site Manager API because it is documented and stable for cloud-managed UniFi sites.

Useful endpoints:

- `GET https://api.ui.com/v1/hosts`
- `GET https://api.ui.com/v1/sites`
- `GET https://api.ui.com/v1/devices`
- `GET https://api.ui.com/ea/isp-metrics/{type}`

Authentication uses the `X-API-Key` header. Responses are wrapped in a standard object with `data`, `httpStatusCode`, and `traceId`.

The API notes that some nested structures can vary by UniFi OS or Network Server version, so the collector should parse defensively and ignore unknown fields.

## SNMP

SNMP should collect conservative baseline indicators first:

- Reachability.
- Uptime.
- Interface status and throughput.
- CPU and memory when exposed by the device MIB.
- Temperature when exposed by the device MIB.

Credentials should be stored server-side, not in browser storage. SNMPv3 should be preferred when available; SNMPv2c can be supported for homelab gear that only exposes community-based polling.

## Next Backend Step

Add a server-side devices configuration store, then start collectors from that persisted config. The frontend already exposes the intended settings surface, but real polling should only be enabled after secrets are persisted through the backend secret/encryption path.
