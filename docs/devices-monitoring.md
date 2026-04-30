# Devices Monitoring

This note captures the intended shape of Pulse network device monitoring.

## Scope

Devices covers manageable network hardware:

- UniFi consoles, gateways, switches, and access points.
- Generic managed switches, routers, modems, and appliances reachable through SNMP.

Docker remains supported internally by agents and backend data paths, but Docker management is intentionally hidden from the main UI in this fork.

## UI Workflow

The UI is organized around two concepts:

- Monitoring checks in `Settings -> Platforms -> Devices`.
- Device inventory in the top-level `Devices` tab.

Checks describe how data should be collected:

- `Ping`: default baseline check for reachability, latency, and packet loss.
- `UniFi`: one or more UniFi Site Manager API checks.
- `SNMP`: one or more SNMP checks for managed network hardware.

Checks can be edited in place from Settings. Ping checks intentionally do not have a default target because the target belongs to each device entry.

The `Devices` tab contains an `Add device` wizard. The wizard first selects one configured check, then captures device identity and address details. Added devices are shown in the Devices inventory with source-aware health cards.

For UniFi checks, Settings exposes official API profiles from the UniFi Site Manager API documentation. The device wizard can query the official endpoint `GET https://api.ui.com/v1/devices` using the configured `X-API-Key`, then use the returned device list to prefill identity, model, site, and address fields.

Device state is refreshed automatically from the selected check interval. Ping and SNMP checks currently use a browser-side state simulation until backend collectors exist. UniFi checks use the real Site Manager device list when an API key is present, with a warning state if a configured device is no longer returned.

The current UI persists this draft configuration in browser local storage so the workflow is usable while the backend collector is being built. Production polling should move check storage to the backend secret store before real credentials are used.

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
