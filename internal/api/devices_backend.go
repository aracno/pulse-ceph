package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/rcourtman/pulse-go-rewrite/internal/utils"
	"github.com/rs/zerolog/log"
)

type deviceCheckType string
type managedDeviceStatus string

const (
	deviceCheckPing  deviceCheckType     = "ping"
	deviceCheckUniFi deviceCheckType     = "unifi"
	deviceCheckSNMP  deviceCheckType     = "snmp"
	deviceOnline     managedDeviceStatus = "online"
	deviceWarning    managedDeviceStatus = "warning"
	deviceOffline    managedDeviceStatus = "offline"
	deviceUnknown    managedDeviceStatus = "unknown"
)

type deviceCheck struct {
	ID              string          `json:"id"`
	Type            deviceCheckType `json:"type"`
	Name            string          `json:"name"`
	Enabled         bool            `json:"enabled"`
	IntervalSeconds int             `json:"intervalSeconds"`
	Host            string          `json:"host,omitempty"`
	APIProfile      string          `json:"apiProfile,omitempty"`
	APIKey          string          `json:"apiKey,omitempty"`
	APIKeyHint      string          `json:"apiKeyHint,omitempty"`
	SiteFilter      string          `json:"siteFilter,omitempty"`
	SNMPVersion     string          `json:"snmpVersion,omitempty"`
	CommunityHint   string          `json:"communityHint,omitempty"`
	Credential      string          `json:"credential,omitempty"`
	Username        string          `json:"username,omitempty"`
	AuthProtocol    string          `json:"authProtocol,omitempty"`
	PrivacyProtocol string          `json:"privacyProtocol,omitempty"`
	TimeoutMs       int             `json:"timeoutMs,omitempty"`
	Retries         int             `json:"retries,omitempty"`
	Notes           string          `json:"notes,omitempty"`
	CreatedAt       string          `json:"createdAt"`
	LastCheckedAt   string          `json:"lastCheckedAt,omitempty"`
	LastError       string          `json:"lastError,omitempty"`
}

type managedDevice struct {
	ID              string              `json:"id"`
	AccountID       string              `json:"accountId"`
	AccountType     deviceCheckType     `json:"accountType"`
	Name            string              `json:"name"`
	Host            string              `json:"host"`
	Type            string              `json:"type"`
	Vendor          string              `json:"vendor,omitempty"`
	Model           string              `json:"model,omitempty"`
	Site            string              `json:"site,omitempty"`
	Status          managedDeviceStatus `json:"status"`
	LatencyMs       *float64            `json:"latencyMs,omitempty"`
	PacketLoss      *float64            `json:"packetLoss,omitempty"`
	Uptime          string              `json:"uptime,omitempty"`
	UptimeSeconds   *float64            `json:"uptimeSeconds,omitempty"`
	FirmwareVersion string              `json:"firmwareVersion,omitempty"`
	LastSeen        string              `json:"lastSeen,omitempty"`
	LastCheckedAt   string              `json:"lastCheckedAt,omitempty"`
	Notes           string              `json:"notes,omitempty"`
	Raw             map[string]any      `json:"raw,omitempty"`
}

type deviceAlertSettings struct {
	Enabled               bool                       `json:"enabled"`
	OfflineEnabled        bool                       `json:"offlineEnabled"`
	LatencyEnabled        bool                       `json:"latencyEnabled"`
	LatencyWarnMs         float64                    `json:"latencyWarnMs"`
	PacketLossEnabled     bool                       `json:"packetLossEnabled"`
	PacketLossWarnPct     float64                    `json:"packetLossWarnPct"`
	UptimeEnabled         bool                       `json:"uptimeEnabled"`
	UptimeMinSeconds      float64                    `json:"uptimeMinSeconds"`
	CheckOverrides        map[string]bool            `json:"checkOverrides,omitempty"`
	DeviceOverrides       map[string]bool            `json:"deviceOverrides,omitempty"`
	DeviceRules           map[string]deviceAlertRule `json:"deviceRules,omitempty"`
	LastEvaluatedAt       string                     `json:"lastEvaluatedAt,omitempty"`
	LastEvaluationSummary map[string]int             `json:"lastEvaluationSummary,omitempty"`
}

type deviceAlertRule struct {
	OfflineEnabled    *bool    `json:"offlineEnabled,omitempty"`
	UptimeEnabled     *bool    `json:"uptimeEnabled,omitempty"`
	UptimeMinSeconds  *float64 `json:"uptimeMinSeconds,omitempty"`
	LatencyEnabled    *bool    `json:"latencyEnabled,omitempty"`
	LatencyWarnMs     *float64 `json:"latencyWarnMs,omitempty"`
	PacketLossEnabled *bool    `json:"packetLossEnabled,omitempty"`
	PacketLossWarnPct *float64 `json:"packetLossWarnPct,omitempty"`
}

type devicesState struct {
	Checks    []deviceCheck       `json:"checks"`
	Devices   []managedDevice     `json:"devices"`
	Alerts    deviceAlertSettings `json:"alerts"`
	UpdatedAt string              `json:"updatedAt"`
}

type devicesStore struct {
	mu     sync.RWMutex
	path   string
	state  devicesState
	client *http.Client
}

type uniFiISPMetric struct {
	HostID     string
	SiteID     string
	MetricTime string
	LatencyMs  *float64
	PacketLoss *float64
	WANUptime  *float64
}

func newDevicesStore(dataPath string) *devicesStore {
	store := &devicesStore{
		path:   filepath.Join(dataPath, "devices.json"),
		client: unifiProxyHTTPClient,
		state: devicesState{
			Checks:  []deviceCheck{defaultDeviceCheck()},
			Devices: []managedDevice{},
			Alerts:  defaultDeviceAlertSettings(),
		},
	}
	if err := store.load(); err != nil {
		log.Warn().Err(err).Msg("Failed to load devices store; using defaults")
		_ = store.saveLocked()
	}
	return store
}

func defaultDeviceCheck() deviceCheck {
	now := time.Now().UTC().Format(time.RFC3339)
	return deviceCheck{
		ID:              "account-ping-default",
		Type:            deviceCheckPing,
		Name:            "Default Ping",
		Enabled:         true,
		IntervalSeconds: 30,
		TimeoutMs:       1500,
		Retries:         2,
		Notes:           "Baseline reachability check used when no API or SNMP source is needed.",
		CreatedAt:       now,
	}
}

func defaultDeviceAlertSettings() deviceAlertSettings {
	return deviceAlertSettings{
		Enabled:           true,
		OfflineEnabled:    true,
		LatencyEnabled:    true,
		LatencyWarnMs:     150,
		PacketLossEnabled: true,
		PacketLossWarnPct: 5,
		UptimeEnabled:     false,
		UptimeMinSeconds:  300,
		CheckOverrides:    map[string]bool{},
		DeviceOverrides:   map[string]bool{},
		DeviceRules:       map[string]deviceAlertRule{},
	}
}

func (s *devicesStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.saveLocked()
		}
		return err
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return err
	}
	s.normalizeLocked()
	return nil
}

func (s *devicesStore) normalizeLocked() {
	if len(s.state.Checks) == 0 {
		s.state.Checks = []deviceCheck{defaultDeviceCheck()}
	}
	if s.state.Alerts.LatencyEnabled && s.state.Alerts.LatencyWarnMs <= 0 {
		s.state.Alerts.LatencyWarnMs = 150
	}
	if s.state.Alerts.PacketLossEnabled && s.state.Alerts.PacketLossWarnPct <= 0 {
		s.state.Alerts.PacketLossWarnPct = 5
	}
	if s.state.Alerts.UptimeEnabled && s.state.Alerts.UptimeMinSeconds <= 0 {
		s.state.Alerts.UptimeMinSeconds = 300
	}
	if s.state.Alerts.CheckOverrides == nil {
		s.state.Alerts.CheckOverrides = map[string]bool{}
	}
	if s.state.Alerts.DeviceOverrides == nil {
		s.state.Alerts.DeviceOverrides = map[string]bool{}
	}
	if s.state.Alerts.DeviceRules == nil {
		s.state.Alerts.DeviceRules = map[string]deviceAlertRule{}
	}
	for i := range s.state.Checks {
		check := &s.state.Checks[i]
		if check.ID == "" {
			check.ID = utils.GenerateID("account")
		}
		if check.Name == "" {
			check.Name = string(check.Type)
		}
		if check.IntervalSeconds <= 0 {
			check.IntervalSeconds = 60
		}
		if check.CreatedAt == "" {
			check.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		check.APIKeyHint = secretHint(check.APIKey, check.APIKeyHint, 6)
		check.CommunityHint = secretHint(check.Credential, check.CommunityHint, 4)
	}
	for i := range s.state.Devices {
		device := &s.state.Devices[i]
		if device.ID == "" {
			device.ID = utils.GenerateID("device")
		}
		if device.Status == "" {
			device.Status = deviceUnknown
		}
	}
}

func secretHint(secret, current string, size int) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return current
	}
	if len(secret) <= size {
		return secret
	}
	return secret[len(secret)-size:]
}

func (s *devicesStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	s.normalizeLocked()
	s.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *devicesStore) snapshot() devicesState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := cloneDevicesState(s.state)
	redactDevicesState(&state)
	return state
}

func cloneDevicesState(state devicesState) devicesState {
	out := state
	out.Checks = append([]deviceCheck(nil), state.Checks...)
	out.Devices = append([]managedDevice(nil), state.Devices...)
	out.Alerts.CheckOverrides = cloneBoolMap(state.Alerts.CheckOverrides)
	out.Alerts.DeviceOverrides = cloneBoolMap(state.Alerts.DeviceOverrides)
	out.Alerts.DeviceRules = cloneDeviceRuleMap(state.Alerts.DeviceRules)
	out.Alerts.LastEvaluationSummary = cloneIntMap(state.Alerts.LastEvaluationSummary)
	return out
}

func cloneBoolMap(input map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range input {
		out[k] = v
	}
	return out
}

func cloneDeviceRuleMap(input map[string]deviceAlertRule) map[string]deviceAlertRule {
	out := map[string]deviceAlertRule{}
	for k, v := range input {
		out[k] = v
	}
	return out
}

func cloneIntMap(input map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range input {
		out[k] = v
	}
	return out
}

func redactDevicesState(state *devicesState) {
	for i := range state.Checks {
		state.Checks[i] = redactDeviceCheck(state.Checks[i])
	}
}

func redactDeviceCheck(check deviceCheck) deviceCheck {
	check.APIKey = ""
	check.Credential = ""
	return check
}

func (s *devicesStore) findCheck(id string) (deviceCheck, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, check := range s.state.Checks {
		if check.ID == id {
			return check, true
		}
	}
	return deviceCheck{}, false
}

func (s *devicesStore) upsertCheck(input deviceCheck) (deviceCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	if input.ID == "" {
		input.ID = utils.GenerateID("account")
	}
	if input.CreatedAt == "" {
		input.CreatedAt = now
	}
	if input.IntervalSeconds <= 0 {
		input.IntervalSeconds = 60
	}
	input.APIKeyHint = secretHint(input.APIKey, input.APIKeyHint, 6)
	input.CommunityHint = secretHint(input.Credential, input.CommunityHint, 4)

	for i, existing := range s.state.Checks {
		if existing.ID == input.ID {
			if input.APIKey == "" {
				input.APIKey = existing.APIKey
				input.APIKeyHint = existing.APIKeyHint
			}
			if input.Credential == "" {
				input.Credential = existing.Credential
				input.CommunityHint = existing.CommunityHint
			}
			input.LastCheckedAt = existing.LastCheckedAt
			input.LastError = existing.LastError
			s.state.Checks[i] = input
			return input, s.saveLocked()
		}
	}
	s.state.Checks = append(s.state.Checks, input)
	return input, s.saveLocked()
}

func (s *devicesStore) deleteCheck(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "account-ping-default" {
		return nil
	}
	checks := s.state.Checks[:0]
	for _, check := range s.state.Checks {
		if check.ID != id {
			checks = append(checks, check)
		}
	}
	s.state.Checks = checks
	devices := s.state.Devices[:0]
	for _, device := range s.state.Devices {
		if device.AccountID != id {
			devices = append(devices, device)
		}
	}
	s.state.Devices = devices
	delete(s.state.Alerts.CheckOverrides, id)
	return s.saveLocked()
}

func (s *devicesStore) upsertDevice(input managedDevice) (managedDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	if input.ID == "" {
		input.ID = utils.GenerateID("device")
	}
	if input.Status == "" {
		input.Status = deviceUnknown
	}
	if input.LastSeen == "" {
		input.LastSeen = now
	}
	if input.AccountType == "" {
		for _, check := range s.state.Checks {
			if check.ID == input.AccountID {
				input.AccountType = check.Type
				break
			}
		}
	}
	for i, existing := range s.state.Devices {
		if existing.ID == input.ID {
			if input.Raw == nil {
				input.Raw = existing.Raw
			}
			s.state.Devices[i] = input
			return input, s.saveLocked()
		}
	}
	s.state.Devices = append(s.state.Devices, input)
	return input, s.saveLocked()
}

func (s *devicesStore) deleteDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	devices := s.state.Devices[:0]
	for _, device := range s.state.Devices {
		if device.ID != id {
			devices = append(devices, device)
		}
	}
	s.state.Devices = devices
	delete(s.state.Alerts.DeviceOverrides, id)
	delete(s.state.Alerts.DeviceRules, id)
	return s.saveLocked()
}

func (s *devicesStore) updateAlerts(alerts deviceAlertSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if alerts.LatencyEnabled && alerts.LatencyWarnMs <= 0 {
		alerts.LatencyWarnMs = 150
	}
	if alerts.PacketLossEnabled && alerts.PacketLossWarnPct <= 0 {
		alerts.PacketLossWarnPct = 5
	}
	if alerts.UptimeEnabled && alerts.UptimeMinSeconds <= 0 {
		alerts.UptimeMinSeconds = 300
	}
	if alerts.CheckOverrides == nil {
		alerts.CheckOverrides = map[string]bool{}
	}
	if alerts.DeviceOverrides == nil {
		alerts.DeviceOverrides = map[string]bool{}
	}
	if alerts.DeviceRules == nil {
		alerts.DeviceRules = map[string]deviceAlertRule{}
	}
	alerts.LastEvaluatedAt = s.state.Alerts.LastEvaluatedAt
	alerts.LastEvaluationSummary = s.state.Alerts.LastEvaluationSummary
	s.state.Alerts = alerts
	return s.saveLocked()
}

func (s *devicesStore) pollDue(force bool) devicesState {
	s.mu.RLock()
	state := cloneDevicesState(s.state)
	s.mu.RUnlock()

	now := time.Now()
	checks := map[string]deviceCheck{}
	for _, check := range state.Checks {
		checks[check.ID] = check
	}
	unifiCache := map[string][]managedDevice{}

	for _, device := range state.Devices {
		check, ok := checks[device.AccountID]
		if !ok || !check.Enabled {
			continue
		}
		if !force && device.LastCheckedAt != "" {
			if last, err := time.Parse(time.RFC3339, device.LastCheckedAt); err == nil && now.Sub(last) < time.Duration(check.IntervalSeconds)*time.Second {
				continue
			}
		}

		var updated managedDevice
		var checkErr error
		switch check.Type {
		case deviceCheckUniFi:
			discovered, ok := unifiCache[check.ID]
			if !ok {
				discovered, checkErr = s.discoverUniFi(check)
				unifiCache[check.ID] = discovered
			}
			updated = mergeUniFiDevice(device, discovered)
		case deviceCheckSNMP:
			updated, checkErr = pollSNMPDevice(device, check)
		default:
			updated, checkErr = pollPingDevice(device, check)
		}
		if checkErr != nil {
			updated = device
			updated.Status = deviceWarning
			updated.LastCheckedAt = now.UTC().Format(time.RFC3339)
			updated.Notes = checkErr.Error()
		}
		_, _ = s.upsertDevice(updated)
		s.markCheckPolled(check.ID, checkErr)
	}
	s.evaluateDeviceAlerts()
	return s.snapshot()
}

func (s *devicesStore) runPoller() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.pollDue(false)
	}
}

func (s *devicesStore) markCheckPolled(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Checks {
		if s.state.Checks[i].ID == id {
			s.state.Checks[i].LastCheckedAt = time.Now().UTC().Format(time.RFC3339)
			if err != nil {
				s.state.Checks[i].LastError = err.Error()
			} else {
				s.state.Checks[i].LastError = ""
			}
			_ = s.saveLocked()
			return
		}
	}
}

func (s *devicesStore) discoverUniFi(check deviceCheck) ([]managedDevice, error) {
	if strings.TrimSpace(check.APIKey) == "" {
		return nil, fmt.Errorf("UniFi API key is required")
	}
	payload := unifiProxyRequest{BaseURL: check.Host, Endpoint: "/v1/devices", APIKey: check.APIKey}
	target, err := buildUniFiProxyURL(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", check.APIKey)
	start := time.Now()
	response, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	apiLatency := float64(time.Since(start).Milliseconds())
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var errPayload map[string]any
		_ = json.NewDecoder(response.Body).Decode(&errPayload)
		return nil, fmt.Errorf("UniFi API returned %s", response.Status)
	}
	var body any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, err
	}
	devices := normalizeUniFiDevices(body, check.ID, check.SiteFilter)
	if metrics, err := s.fetchUniFiISPMetrics(check); err == nil {
		applyUniFiISPMetrics(devices, metrics)
	} else {
		log.Debug().Err(err).Msg("Unable to fetch UniFi ISP metrics")
	}
	for i := range devices {
		raw := devices[i].Raw
		if raw == nil {
			raw = map[string]any{}
		}
		raw["_pulseAPILatencyMs"] = apiLatency
		devices[i].Raw = raw
	}
	return devices, nil
}

func (s *devicesStore) fetchUniFiISPMetrics(check deviceCheck) (map[string]uniFiISPMetric, error) {
	payload := unifiProxyRequest{BaseURL: check.Host, Endpoint: "/ea/isp-metrics/5m", APIKey: check.APIKey}
	target, err := buildUniFiProxyURL(payload)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	query.Set("duration", "24h")
	parsed.RawQuery = query.Encode()
	req, err := http.NewRequest(http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", check.APIKey)
	response, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("UniFi ISP metrics returned %s", response.Status)
	}
	var body any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, err
	}
	return normalizeUniFiISPMetrics(body), nil
}

func pollPingDevice(device managedDevice, check deviceCheck) (managedDevice, error) {
	timeout := check.TimeoutMs
	if timeout <= 0 {
		timeout = 1500
	}
	host := strings.TrimSpace(device.Host)
	if host == "" {
		return device, fmt.Errorf("device address is empty")
	}
	samples := check.Retries + 1
	if samples < 3 {
		samples = 3
	}
	if samples > 10 {
		samples = 10
	}
	var successes int
	var totalLatency time.Duration
	for i := 0; i < samples; i++ {
		elapsed, ok := runPingProbe(host, timeout)
		if ok {
			successes++
			totalLatency += elapsed
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	packetLoss := float64(samples-successes) / float64(samples) * 100
	device.PacketLoss = roundedFloatPtr(packetLoss, 1)
	device.LastCheckedAt = now
	raw := ensureDeviceRaw(device.Raw)
	raw["_pulsePing"] = map[string]any{
		"samples":   samples,
		"successes": successes,
		"checkedAt": now,
	}
	if successes == 0 {
		device.Status = deviceOffline
		device.LatencyMs = nil
		device.Raw = raw
		return device, nil
	}
	latency := float64(totalLatency.Milliseconds()) / float64(successes)
	previousStatus := device.Status
	device.Status = deviceOnline
	device.LatencyMs = roundedFloatPtr(latency, 1)
	setOnlineSince(&device, raw, now, previousStatus)
	device.LastSeen = now
	device.Raw = raw
	return device, nil
}

func pollSNMPDevice(device managedDevice, check deviceCheck) (managedDevice, error) {
	host, port := splitSNMPHostPort(device.Host)
	pingTarget := device
	pingTarget.Host = host
	polled, err := pollPingDevice(pingTarget, check)
	polled.Host = device.Host
	if err != nil || polled.Status == deviceOffline {
		return polled, err
	}
	timeout := time.Duration(check.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	conn, err := net.DialTimeout("udp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	now := time.Now().UTC().Format(time.RFC3339)
	polled.LastCheckedAt = now
	if err != nil {
		polled.Status = deviceWarning
		polled.Notes = firstNonEmpty(polled.Notes, "SNMP port is not reachable")
		return polled, nil
	}
	_ = conn.Close()
	if strings.EqualFold(check.SNMPVersion, "v3") {
		polled.Status = deviceWarning
		polled.Notes = "SNMPv3 credentials are stored, but v3 polling is not enabled yet; using ping reachability only"
		return polled, nil
	}
	community := strings.TrimSpace(check.Credential)
	if community == "" {
		community = "public"
	}
	params := &gosnmp.GoSNMP{
		Target:    host,
		Port:      uint16(port),
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   timeout,
		Retries:   max(0, check.Retries),
	}
	if err := params.Connect(); err != nil {
		polled.Status = deviceWarning
		polled.Notes = "SNMP polling failed: " + err.Error()
		return polled, nil
	}
	defer params.Conn.Close()
	applySNMPUptime(params, &polled)
	polled.Status = deviceOnline
	polled.LastSeen = now
	return polled, nil
}

func runPingProbe(host string, timeoutMs int) (time.Duration, bool) {
	start := time.Now()
	cmd := "ping"
	args := []string{"-c", "1", "-W", strconv.Itoa(max(1, timeoutMs/1000)), host}
	if runtime.GOOS == "windows" {
		args = []string{"-n", "1", "-w", strconv.Itoa(timeoutMs), host}
	}
	if err := exec.Command(cmd, args...).Run(); err != nil {
		return 0, false
	}
	return time.Since(start), true
}

func splitSNMPHostPort(input string) (string, int) {
	host := strings.TrimSpace(input)
	port := 161
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
		if value, err := strconv.Atoi(parsedPort); err == nil && value > 0 {
			port = value
		}
	}
	return host, port
}

func applySNMPUptime(params *gosnmp.GoSNMP, device *managedDevice) {
	uptimeTicks := snmpGetFloat(params, ".1.3.6.1.2.1.1.3.0")
	if uptimeTicks == nil {
		return
	}
	uptimeSeconds := *uptimeTicks / 100
	device.UptimeSeconds = roundedFloatPtr(uptimeSeconds, 1)
	device.Uptime = formatDurationSeconds(uptimeSeconds)
	raw := ensureDeviceRaw(device.Raw)
	raw["_pulseSNMP"] = map[string]any{
		"sysUpTimeTicks": *uptimeTicks,
		"sysUpTimeOID":   ".1.3.6.1.2.1.1.3.0",
	}
	device.Raw = raw
}

func snmpGetFloat(params *gosnmp.GoSNMP, oid string) *float64 {
	result, err := params.Get([]string{oid})
	if err != nil || len(result.Variables) == 0 {
		return nil
	}
	if value, ok := snmpFloatValue(result.Variables[0].Value); ok {
		return &value
	}
	return nil
}

func snmpFloatValue(input any) (float64, bool) {
	switch value := input.(type) {
	case int:
		return float64(value), true
	case uint:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint64:
		return float64(value), true
	case uint32:
		return float64(value), true
	case float64:
		return value, true
	case []byte:
		parsed, err := strconv.ParseFloat(string(value), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func mergeUniFiDevice(current managedDevice, discovered []managedDevice) managedDevice {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range discovered {
		if unifiMatch(current, item) {
			item.ID = current.ID
			item.AccountID = current.AccountID
			item.AccountType = current.AccountType
			if current.Notes != "" && item.Notes == "" {
				item.Notes = current.Notes
			}
			item.LastCheckedAt = now
			if item.Status == deviceOnline || item.Status == deviceWarning {
				item.LastSeen = now
			}
			return item
		}
	}
	current.Status = deviceWarning
	current.LastCheckedAt = now
	return current
}

func unifiMatch(a, b managedDevice) bool {
	candidatesA := []string{a.Host, a.Name}
	if a.Raw != nil {
		candidatesA = append(candidatesA, stringValue(a.Raw, "id"), stringValue(a.Raw, "_id"), stringValue(a.Raw, "mac"), stringValue(a.Raw, "ip"))
	}
	candidatesB := []string{b.Host, b.Name}
	if b.Raw != nil {
		candidatesB = append(candidatesB, stringValue(b.Raw, "id"), stringValue(b.Raw, "_id"), stringValue(b.Raw, "mac"), stringValue(b.Raw, "ip"))
	}
	for _, left := range candidatesA {
		left = strings.ToLower(strings.TrimSpace(left))
		if left == "" {
			continue
		}
		for _, right := range candidatesB {
			if left == strings.ToLower(strings.TrimSpace(right)) {
				return true
			}
		}
	}
	return false
}

func (s *devicesStore) evaluateDeviceAlerts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	summary := map[string]int{"offline": 0, "uptime": 0, "latency": 0, "packetLoss": 0}
	cfg := s.state.Alerts
	if cfg.Enabled {
		for _, device := range s.state.Devices {
			if cfg.DeviceOverrides[device.ID] || cfg.CheckOverrides[device.AccountID] {
				continue
			}
			rule := effectiveDeviceAlertRule(cfg, device.ID)
			if rule.OfflineEnabled && device.Status == deviceOffline {
				summary["offline"]++
			}
			if rule.LatencyEnabled && device.LatencyMs != nil && *device.LatencyMs >= rule.LatencyWarnMs {
				summary["latency"]++
			}
			if rule.PacketLossEnabled && device.PacketLoss != nil && *device.PacketLoss >= rule.PacketLossWarnPct {
				summary["packetLoss"]++
			}
			if rule.UptimeEnabled && device.Status == deviceOnline && device.UptimeSeconds != nil && *device.UptimeSeconds >= 0 && *device.UptimeSeconds <= rule.UptimeMinSeconds {
				summary["uptime"]++
			}
		}
	}
	s.state.Alerts.LastEvaluatedAt = time.Now().UTC().Format(time.RFC3339)
	s.state.Alerts.LastEvaluationSummary = summary
	_ = s.saveLocked()
}

type effectiveDeviceAlerts struct {
	OfflineEnabled    bool
	UptimeEnabled     bool
	UptimeMinSeconds  float64
	LatencyEnabled    bool
	LatencyWarnMs     float64
	PacketLossEnabled bool
	PacketLossWarnPct float64
}

func effectiveDeviceAlertRule(cfg deviceAlertSettings, deviceID string) effectiveDeviceAlerts {
	out := effectiveDeviceAlerts{
		OfflineEnabled:    cfg.OfflineEnabled,
		UptimeEnabled:     cfg.UptimeEnabled,
		UptimeMinSeconds:  cfg.UptimeMinSeconds,
		LatencyEnabled:    cfg.LatencyEnabled,
		LatencyWarnMs:     cfg.LatencyWarnMs,
		PacketLossEnabled: cfg.PacketLossEnabled,
		PacketLossWarnPct: cfg.PacketLossWarnPct,
	}
	rule, ok := cfg.DeviceRules[deviceID]
	if !ok {
		return out
	}
	if rule.OfflineEnabled != nil {
		out.OfflineEnabled = *rule.OfflineEnabled
	}
	if rule.UptimeEnabled != nil {
		out.UptimeEnabled = *rule.UptimeEnabled
	}
	if rule.UptimeMinSeconds != nil {
		out.UptimeMinSeconds = *rule.UptimeMinSeconds
	}
	if rule.LatencyEnabled != nil {
		out.LatencyEnabled = *rule.LatencyEnabled
	}
	if rule.LatencyWarnMs != nil {
		out.LatencyWarnMs = *rule.LatencyWarnMs
	}
	if rule.PacketLossEnabled != nil {
		out.PacketLossEnabled = *rule.PacketLossEnabled
	}
	if rule.PacketLossWarnPct != nil {
		out.PacketLossWarnPct = *rule.PacketLossWarnPct
	}
	return out
}

func normalizeUniFiDevices(payload any, accountID string, siteFilter string) []managedDevice {
	var roots []map[string]any
	for _, item := range extractUniFiRecords(payload) {
		if record, ok := item.(map[string]any); ok {
			roots = append(roots, record)
		}
	}
	out := make([]managedDevice, 0, len(roots))
	seen := map[string]bool{}
	for idx, record := range roots {
		device := normalizeUniFiRecord(record, accountID, idx)
		if siteFilter != "" && !strings.Contains(strings.ToLower(device.Site), strings.ToLower(siteFilter)) {
			continue
		}
		key := strings.ToLower(firstNonEmpty(device.Host, stringValue(device.Raw, "mac"), device.Name))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, device)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func normalizeUniFiISPMetrics(payload any) map[string]uniFiISPMetric {
	metrics := map[string]uniFiISPMetric{}
	for _, item := range extractUniFiMetricRecords(payload) {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		hostID := stringValue(record, "hostId")
		if hostID == "" {
			continue
		}
		period := latestUniFiMetricPeriod(record["periods"])
		if period == nil {
			continue
		}
		data := mapValue(period, "data")
		wan := mapValue(data, "wan")
		metric := uniFiISPMetric{
			HostID:     hostID,
			SiteID:     stringValue(record, "siteId"),
			MetricTime: stringValue(period, "metricTime"),
			LatencyMs:  numberFromPaths(wan, [][]string{{"avgLatency"}, {"latency"}, {"averageLatency"}}),
			PacketLoss: numberFromPaths(wan, [][]string{{"packetLoss"}, {"packet_loss"}}),
			WANUptime:  numberFromPaths(wan, [][]string{{"uptime"}, {"wanUptime"}}),
		}
		metrics[hostID] = metric
	}
	return metrics
}

func extractUniFiMetricRecords(payload any) []any {
	var records []any
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			if data, ok := typed["data"]; ok {
				walk(data)
			}
			if stringValue(typed, "hostId") != "" {
				if _, ok := typed["periods"]; ok {
					records = append(records, typed)
				}
			}
		}
	}
	walk(payload)
	return records
}

func latestUniFiMetricPeriod(value any) map[string]any {
	periods, ok := value.([]any)
	if !ok || len(periods) == 0 {
		return nil
	}
	for i := len(periods) - 1; i >= 0; i-- {
		if period, ok := periods[i].(map[string]any); ok {
			return period
		}
	}
	return nil
}

func applyUniFiISPMetrics(devices []managedDevice, metrics map[string]uniFiISPMetric) {
	for i := range devices {
		hostID := ""
		if devices[i].Raw != nil {
			hostID = stringValue(devices[i].Raw, "_pulseHostId")
		}
		if hostID == "" {
			continue
		}
		if !isUniFiGatewayMetricTarget(devices[i]) {
			continue
		}
		metric, ok := metrics[hostID]
		if !ok {
			continue
		}
		if metric.LatencyMs != nil {
			devices[i].LatencyMs = metric.LatencyMs
		}
		if metric.PacketLoss != nil {
			devices[i].PacketLoss = metric.PacketLoss
		}
		raw := devices[i].Raw
		if raw == nil {
			raw = map[string]any{}
		}
		raw["_pulseIspMetrics"] = map[string]any{
			"siteId":     metric.SiteID,
			"metricTime": metric.MetricTime,
			"wanUptime":  metric.WANUptime,
		}
		devices[i].Raw = raw
	}
}

func isUniFiGatewayMetricTarget(device managedDevice) bool {
	if device.Raw != nil && strings.EqualFold(stringValue(device.Raw, "isConsole"), "true") {
		return true
	}
	switch strings.ToLower(device.Type) {
	case "gateway", "router", "modem", "controller":
		return true
	default:
		return false
	}
}

func extractUniFiRecords(payload any) []any {
	var records []any
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			if data, ok := typed["data"]; ok {
				walk(data)
			}
			if enriched := enrichedUniFiHostDevices(typed); len(enriched) > 0 {
				for _, record := range enriched {
					records = append(records, record)
				}
			} else {
				for _, key := range []string{"devices", "uidb"} {
					if nested, ok := typed[key]; ok {
						walk(nested)
					}
				}
			}
			if looksLikeUniFiDevice(typed) {
				records = append(records, typed)
			}
			if uidb, ok := typed["uidb"].(map[string]any); ok {
				for _, item := range uidb {
					walk(item)
				}
			}
		}
	}
	walk(payload)
	return records
}

func enrichedUniFiHostDevices(record map[string]any) []any {
	hostID := stringValue(record, "hostId")
	devices, ok := record["devices"].([]any)
	if !ok || hostID == "" {
		return nil
	}
	out := make([]any, 0, len(devices))
	for _, item := range devices {
		device, ok := item.(map[string]any)
		if !ok {
			continue
		}
		clone := map[string]any{}
		for key, value := range device {
			clone[key] = value
		}
		clone["_pulseHostId"] = hostID
		clone["_pulseHostName"] = stringValue(record, "hostName")
		clone["_pulseHostUpdatedAt"] = stringValue(record, "updatedAt")
		out = append(out, clone)
	}
	return out
}

func looksLikeUniFiDevice(record map[string]any) bool {
	keys := []string{"mac", "ip", "ipAddress", "model", "modelName", "shortname", "productLine", "adopted", "version", "firmwareVersion", "uplink", "connectedAt"}
	for _, key := range keys {
		if _, ok := record[key]; ok {
			return true
		}
	}
	return false
}

func normalizeUniFiRecord(record map[string]any, accountID string, index int) managedDevice {
	reported := mapValue(record, "reportedState")
	site := mapValue(record, "site")
	meta := mapValue(record, "meta")
	names := []string{
		stringValue(record, "name"),
		stringValue(record, "displayName"),
		stringValue(record, "hostname"),
		stringValue(record, "alias"),
		stringValue(record, "deviceName"),
		stringValue(record, "customName"),
		stringValue(record, "label"),
		stringValue(reported, "name"),
		stringValue(reported, "hostname"),
		stringValue(meta, "name"),
	}
	model := firstNonEmpty(
		stringValue(record, "model"),
		stringValue(record, "modelName"),
		stringValue(record, "shortname"),
		stringValue(record, "productName"),
		stringValue(record, "modelDisplay"),
		stringValue(record, "hardwareModel"),
		stringValue(reported, "model"),
		stringValue(reported, "shortname"),
	)
	host := firstNonEmpty(
		stringValue(record, "ipAddress"),
		stringValue(record, "ip"),
		stringValue(record, "host"),
		firstStringFromArray(record["ipAddrs"]),
		stringValue(record, "mac"),
		stringValue(record, "id"),
		stringValue(record, "_id"),
	)
	rawType := firstNonEmpty(
		stringValue(record, "type"),
		stringValue(record, "category"),
		stringValue(record, "deviceType"),
		model,
		stringValue(record, "networkDeviceType"),
		stringValue(record, "productLine"),
	)
	status := normalizeUniFiStatus(firstNonEmpty(
		stringValue(record, "status"),
		stringValue(record, "state"),
		stringValue(record, "connectionState"),
		stringValue(record, "connectedState"),
		fmt.Sprint(record["online"]),
	))
	name := firstNonEmpty(names...)
	if name == "" {
		name = firstNonEmpty(model, host, fmt.Sprintf("UniFi device %d", index+1))
	}
	packetLoss := numberFromPaths(record, [][]string{
		{"packetLoss"}, {"packet_loss"}, {"wan", "packetLoss"}, {"metrics", "packetLoss"}, {"uplink", "packetLoss"},
	})
	startupTime := stringValue(record, "startupTime")
	uptimeSeconds := uptimeSecondsFromTimestamp(startupTime)
	uptime := ""
	if uptimeSeconds != nil {
		uptime = formatDurationSeconds(*uptimeSeconds)
	}
	return managedDevice{
		ID:              utils.GenerateID("unifi-device"),
		AccountID:       accountID,
		AccountType:     deviceCheckUniFi,
		Name:            name,
		Host:            host,
		Type:            mapUniFiDeviceType(rawType),
		Vendor:          "Ubiquiti",
		Model:           model,
		Site:            firstNonEmpty(stringValue(record, "siteName"), stringValue(record, "siteId"), stringValue(site, "name"), stringValue(site, "desc")),
		Status:          status,
		PacketLoss:      packetLoss,
		Uptime:          uptime,
		UptimeSeconds:   uptimeSeconds,
		FirmwareVersion: firstNonEmpty(stringValue(record, "version"), stringValue(record, "firmwareVersion"), stringValue(record, "firmware")),
		LastSeen:        firstNonEmpty(stringValue(record, "lastSeen"), stringValue(record, "lastConnectionStateChange")),
		Raw:             record,
	}
}

func normalizeUniFiStatus(value string) managedDeviceStatus {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == "false", strings.Contains(value, "offline"), strings.Contains(value, "disconnected"):
		return deviceOffline
	case strings.Contains(value, "update"), strings.Contains(value, "adopt"), strings.Contains(value, "pending"):
		return deviceWarning
	case value == "", value == "<nil>":
		return deviceUnknown
	default:
		return deviceOnline
	}
}

func mapUniFiDeviceType(raw string) string {
	raw = strings.ToLower(raw)
	switch {
	case strings.Contains(raw, "switch"), strings.Contains(raw, "usw"):
		return "switch"
	case strings.Contains(raw, "gateway"), strings.Contains(raw, "router"), strings.Contains(raw, "udm"), strings.Contains(raw, "ugw"):
		return "gateway"
	case strings.Contains(raw, "access"), strings.Contains(raw, "uap"), strings.Contains(raw, "ap"):
		return "access_point"
	case strings.Contains(raw, "console"), strings.Contains(raw, "cloud"), strings.Contains(raw, "ucore"):
		return "controller"
	default:
		return "other"
	}
}

func mapValue(record map[string]any, key string) map[string]any {
	if value, ok := record[key].(map[string]any); ok {
		return value
	}
	return nil
}

func stringValue(record map[string]any, key string) string {
	if record == nil {
		return ""
	}
	switch value := record[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		if value {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func numberValue(record map[string]any, key string) (float64, bool) {
	if record == nil {
		return 0, false
	}
	switch value := record[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "%")), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func numberFromPath(record map[string]any, path []string) (float64, bool) {
	current := record
	for i, key := range path {
		if i == len(path)-1 {
			return numberValue(current, key)
		}
		current = mapValue(current, key)
		if current == nil {
			return 0, false
		}
	}
	return 0, false
}

func numberFromPaths(record map[string]any, paths [][]string) *float64 {
	for _, path := range paths {
		if value, ok := numberFromPath(record, path); ok {
			return roundedFloatPtr(value, 1)
		}
	}
	return nil
}

func roundedFloatPtr(value float64, precision int) *float64 {
	if precision < 0 {
		precision = 0
	}
	scale := 1.0
	for i := 0; i < precision; i++ {
		scale *= 10
	}
	rounded := float64(int(value*scale+0.5)) / scale
	return &rounded
}

func firstStringFromArray(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ensureDeviceRaw(raw map[string]any) map[string]any {
	if raw != nil {
		return raw
	}
	return map[string]any{}
}

func setOnlineSince(device *managedDevice, raw map[string]any, checkedAt string, previousStatus managedDeviceStatus) {
	onlineSince := stringValue(raw, "_pulseOnlineSince")
	if previousStatus != deviceOnline || onlineSince == "" {
		onlineSince = checkedAt
		raw["_pulseOnlineSince"] = onlineSince
	}
	if parsed, err := time.Parse(time.RFC3339, onlineSince); err == nil {
		seconds := time.Since(parsed).Seconds()
		if seconds < 0 {
			seconds = 0
		}
		device.UptimeSeconds = roundedFloatPtr(seconds, 1)
		device.Uptime = formatDurationSeconds(seconds)
	}
}

func uptimeSecondsFromTimestamp(value string) *float64 {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	seconds := time.Since(parsed).Seconds()
	if seconds < 0 {
		seconds = 0
	}
	return roundedFloatPtr(seconds, 1)
}

func formatDurationSeconds(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	duration := time.Duration(seconds) * time.Second
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
