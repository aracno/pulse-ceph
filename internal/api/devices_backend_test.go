package api

import "testing"

func TestNormalizeUniFiDevicesFlattensOfficialEnvelope(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id":   "host-1",
				"name": "PYXI-UDMPRO",
				"devices": []any{
					map[string]any{
						"id":          "dev-1",
						"name":        "PYXI-SW",
						"ipAddress":   "192.168.1.84",
						"model":       "USW Pro HD 24",
						"productLine": "switch",
						"status":      "UPDATE_AVAILABLE",
						"metrics": map[string]any{
							"packetLoss": float64(0.5),
						},
						"site": map[string]any{
							"name": "Homelab",
						},
						"version": "7.1.26",
					},
					map[string]any{
						"id":          "dev-2",
						"displayName": "U7 Pro",
						"ip":          "192.168.1.165",
						"shortname":   "U7-Pro",
						"productLine": "accessPoint",
						"status":      "ONLINE",
						"siteName":    "Homelab",
					},
				},
			},
		},
		"httpStatusCode": float64(200),
	}

	devices := normalizeUniFiDevices(payload, "check-unifi", "")
	if got, want := len(devices), 2; got != want {
		t.Fatalf("expected %d UniFi devices, got %d: %#v", want, got, devices)
	}
	byName := map[string]managedDevice{}
	for _, device := range devices {
		byName[device.Name] = device
	}
	sw, ok := byName["PYXI-SW"]
	if !ok {
		t.Fatalf("expected switch name to be preserved, got %#v", devices)
	}
	if sw.Host != "192.168.1.84" {
		t.Fatalf("expected switch host from ipAddress, got %q", sw.Host)
	}
	if sw.Type != "switch" {
		t.Fatalf("expected switch type, got %q", sw.Type)
	}
	if sw.Status != deviceWarning {
		t.Fatalf("expected update state to map to warning, got %q", sw.Status)
	}
	if sw.Site != "Homelab" {
		t.Fatalf("expected site name from nested site, got %q", sw.Site)
	}
	if sw.PacketLoss == nil || *sw.PacketLoss != 0.5 {
		t.Fatalf("expected UniFi packet loss metric to be retained when exposed, got %#v", sw.PacketLoss)
	}
	if _, ok := byName["U7 Pro"]; !ok {
		t.Fatalf("expected access point displayName to be preserved, got %#v", devices)
	}
}

func TestNormalizeUniFiDevicesHandlesUIDBAndReportedState(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"devices": map[string]any{
				"uidb": []any{
					map[string]any{
						"_id": "64aa",
						"mac": "00:11:22:33:44:55",
						"reportedState": map[string]any{
							"name":     "US 8 PoE 150W",
							"hostname": "fallback-hostname",
							"model":    "US-8-150W",
						},
						"ipAddrs": []any{"192.168.1.81"},
						"online":  false,
					},
				},
			},
		},
	}

	devices := normalizeUniFiDevices(payload, "check-unifi", "")
	if got, want := len(devices), 1; got != want {
		t.Fatalf("expected %d UIDB device, got %d: %#v", want, got, devices)
	}
	device := devices[0]
	if device.Name != "US 8 PoE 150W" {
		t.Fatalf("expected reportedState.name, got %q", device.Name)
	}
	if device.Host != "192.168.1.81" {
		t.Fatalf("expected host from ipAddrs, got %q", device.Host)
	}
	if device.Status != deviceOffline {
		t.Fatalf("expected online=false to map to offline, got %q", device.Status)
	}
}

func TestNormalizeUniFiDevicesEnrichesHostMetadataAndISPMetrics(t *testing.T) {
	devicesPayload := map[string]any{
		"data": []any{
			map[string]any{
				"hostId":   "host-pyxi",
				"hostName": "PYXI-UDMPRO",
				"devices": []any{
					map[string]any{
						"id":          "70A74168C9EE",
						"mac":         "70A74168C9EE",
						"name":        "PYXI-UDMPRO",
						"model":       "UDM SE",
						"ip":          "10.0.0.103",
						"productLine": "network",
						"status":      "online",
						"isConsole":   true,
					},
					map[string]any{
						"id":          "switch-1",
						"name":        "PYXI-SW",
						"model":       "USW Pro 24 PoE",
						"ip":          "192.168.1.84",
						"productLine": "network",
						"status":      "online",
					},
				},
			},
		},
	}
	metricsPayload := map[string]any{
		"data": []any{
			map[string]any{
				"hostId": "host-pyxi",
				"siteId": "site-home",
				"periods": []any{
					map[string]any{
						"metricTime": "2026-05-01T05:35:00Z",
						"data": map[string]any{
							"wan": map[string]any{
								"avgLatency": float64(55),
								"packetLoss": float64(1),
								"uptime":     float64(100),
							},
						},
					},
				},
			},
		},
	}

	devices := normalizeUniFiDevices(devicesPayload, "check-unifi", "")
	applyUniFiISPMetrics(devices, normalizeUniFiISPMetrics(metricsPayload))

	byName := map[string]managedDevice{}
	for _, device := range devices {
		byName[device.Name] = device
	}
	gateway := byName["PYXI-UDMPRO"]
	if gateway.Type != "gateway" {
		t.Fatalf("expected UDM model to map to gateway, got %q", gateway.Type)
	}
	if gateway.LatencyMs == nil || *gateway.LatencyMs != 55 {
		t.Fatalf("expected ISP latency metric, got %#v", gateway.LatencyMs)
	}
	if gateway.PacketLoss == nil || *gateway.PacketLoss != 1 {
		t.Fatalf("expected ISP packet loss metric, got %#v", gateway.PacketLoss)
	}
	if byName["PYXI-SW"].LatencyMs != nil {
		t.Fatalf("expected switch not to inherit gateway latency, got %#v", byName["PYXI-SW"].LatencyMs)
	}
}

func TestNormalizeUniFiDevicesSiteFilter(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{"name": "Kept", "mac": "aa", "siteName": "Homelab"},
			map[string]any{"name": "Skipped", "mac": "bb", "siteName": "Office"},
		},
	}

	devices := normalizeUniFiDevices(payload, "check-unifi", "home")
	if got, want := len(devices), 1; got != want {
		t.Fatalf("expected %d filtered device, got %d: %#v", want, got, devices)
	}
	if devices[0].Name != "Kept" {
		t.Fatalf("expected Homelab device to remain, got %#v", devices[0])
	}
}

func TestDevicesStorePersistsAndRedactsSecrets(t *testing.T) {
	dataPath := t.TempDir()
	store := newDevicesStore(dataPath)
	saved, err := store.upsertCheck(deviceCheck{
		ID:              "unifi-main",
		Type:            deviceCheckUniFi,
		Name:            "UniFi Homelab",
		Enabled:         true,
		IntervalSeconds: 60,
		Host:            "https://api.ui.com",
		APIKey:          "secret-api-key",
	})
	if err != nil {
		t.Fatalf("upsert check: %v", err)
	}
	if saved.APIKey == "" {
		t.Fatalf("expected internal upsert result to retain API key")
	}

	snapshot := store.snapshot()
	if got := snapshot.Checks[1].APIKey; got != "" {
		t.Fatalf("expected snapshot API key to be redacted, got %q", got)
	}
	if got := snapshot.Checks[1].APIKeyHint; got != "pi-key" {
		t.Fatalf("expected API key hint to be retained, got %q", got)
	}

	reloaded := newDevicesStore(dataPath)
	check, ok := reloaded.findCheck("unifi-main")
	if !ok {
		t.Fatalf("expected persisted check to reload")
	}
	if check.APIKey != "secret-api-key" {
		t.Fatalf("expected persisted secret for backend routines, got %q", check.APIKey)
	}
}
