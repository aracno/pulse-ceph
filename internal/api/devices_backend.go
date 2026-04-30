package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	CPUUsage        *float64            `json:"cpuUsage,omitempty"`
	MemoryUsage     *float64            `json:"memoryUsage,omitempty"`
	LatencyMs       *float64            `json:"latencyMs,omitempty"`
	PacketLoss      *float64            `json:"packetLoss,omitempty"`
	Uptime          string              `json:"uptime,omitempty"`
	FirmwareVersion string              `json:"firmwareVersion,omitempty"`
	LastSeen        string              `json:"lastSeen,omitempty"`
	LastCheckedAt   string              `json:"lastCheckedAt,omitempty"`
	Notes           string              `json:"notes,omitempty"`
	Raw             map[string]any      `json:"raw,omitempty"`
}

type deviceAlertSettings struct {
	Enabled               bool            `json:"enabled"`
	OfflineEnabled        bool            `json:"offlineEnabled"`
	WarningEnabled        bool            `json:"warningEnabled"`
	LatencyEnabled        bool            `json:"latencyEnabled"`
	LatencyWarnMs         float64         `json:"latencyWarnMs"`
	PacketLossEnabled     bool            `json:"packetLossEnabled"`
	PacketLossWarnPct     float64         `json:"packetLossWarnPct"`
	FirmwareEnabled       bool            `json:"firmwareEnabled"`
	CheckOverrides        map[string]bool `json:"checkOverrides,omitempty"`
	DeviceOverrides       map[string]bool `json:"deviceOverrides,omitempty"`
	LastEvaluatedAt       string          `json:"lastEvaluatedAt,omitempty"`
	LastEvaluationSummary map[string]int  `json:"lastEvaluationSummary,omitempty"`
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
		WarningEnabled:    true,
		LatencyEnabled:    true,
		LatencyWarnMs:     150,
		PacketLossEnabled: true,
		PacketLossWarnPct: 5,
		FirmwareEnabled:   true,
		CheckOverrides:    map[string]bool{},
		DeviceOverrides:   map[string]bool{},
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
	if s.state.Alerts.LatencyWarnMs == 0 {
		s.state.Alerts = defaultDeviceAlertSettings()
	}
	if s.state.Alerts.CheckOverrides == nil {
		s.state.Alerts.CheckOverrides = map[string]bool{}
	}
	if s.state.Alerts.DeviceOverrides == nil {
		s.state.Alerts.DeviceOverrides = map[string]bool{}
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
	return s.saveLocked()
}

func (s *devicesStore) updateAlerts(alerts deviceAlertSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if alerts.LatencyWarnMs <= 0 {
		alerts.LatencyWarnMs = 150
	}
	if alerts.PacketLossWarnPct <= 0 {
		alerts.PacketLossWarnPct = 5
	}
	if alerts.CheckOverrides == nil {
		alerts.CheckOverrides = map[string]bool{}
	}
	if alerts.DeviceOverrides == nil {
		alerts.DeviceOverrides = map[string]bool{}
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
	response, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var errPayload map[string]any
		_ = json.NewDecoder(response.Body).Decode(&errPayload)
		return nil, fmt.Errorf("UniFi API returned %s", response.Status)
	}
	var body any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, err
	}
	return normalizeUniFiDevices(body, check.ID, check.SiteFilter), nil
}

func pollPingDevice(device managedDevice, check deviceCheck) (managedDevice, error) {
	start := time.Now()
	timeout := check.TimeoutMs
	if timeout <= 0 {
		timeout = 1500
	}
	host := strings.TrimSpace(device.Host)
	if host == "" {
		return device, fmt.Errorf("device address is empty")
	}
	cmd := "ping"
	args := []string{"-c", "1", "-W", strconv.Itoa(max(1, timeout/1000)), host}
	if runtime.GOOS == "windows" {
		args = []string{"-n", "1", "-w", strconv.Itoa(timeout), host}
	}
	if err := exec.Command(cmd, args...).Run(); err != nil {
		now := time.Now().UTC().Format(time.RFC3339)
		device.Status = deviceOffline
		device.LastCheckedAt = now
		return device, nil
	}
	latency := float64(time.Since(start).Milliseconds())
	packetLoss := float64(0)
	now := time.Now().UTC().Format(time.RFC3339)
	device.Status = deviceOnline
	device.LatencyMs = &latency
	device.PacketLoss = &packetLoss
	device.LastSeen = now
	device.LastCheckedAt = now
	return device, nil
}

func pollSNMPDevice(device managedDevice, check deviceCheck) (managedDevice, error) {
	timeout := time.Duration(check.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	host := device.Host
	if !strings.Contains(host, ":") {
		host = net.JoinHostPort(host, "161")
	}
	conn, err := net.DialTimeout("udp", host, timeout)
	now := time.Now().UTC().Format(time.RFC3339)
	device.LastCheckedAt = now
	if err != nil {
		device.Status = deviceOffline
		return device, nil
	}
	_ = conn.Close()
	device.Status = deviceOnline
	device.LastSeen = now
	return device, nil
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
	summary := map[string]int{"offline": 0, "warning": 0, "latency": 0, "packetLoss": 0}
	cfg := s.state.Alerts
	if cfg.Enabled {
		for _, device := range s.state.Devices {
			if cfg.DeviceOverrides[device.ID] || cfg.CheckOverrides[device.AccountID] {
				continue
			}
			if cfg.OfflineEnabled && device.Status == deviceOffline {
				summary["offline"]++
			}
			if cfg.WarningEnabled && device.Status == deviceWarning {
				summary["warning"]++
			}
			if cfg.LatencyEnabled && device.LatencyMs != nil && *device.LatencyMs >= cfg.LatencyWarnMs {
				summary["latency"]++
			}
			if cfg.PacketLossEnabled && device.PacketLoss != nil && *device.PacketLoss >= cfg.PacketLossWarnPct {
				summary["packetLoss"]++
			}
		}
	}
	s.state.Alerts.LastEvaluatedAt = time.Now().UTC().Format(time.RFC3339)
	s.state.Alerts.LastEvaluationSummary = summary
	_ = s.saveLocked()
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
			for _, key := range []string{"devices", "uidb"} {
				if nested, ok := typed[key]; ok {
					walk(nested)
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
		stringValue(record, "productLine"),
		stringValue(record, "networkDeviceType"),
		model,
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
