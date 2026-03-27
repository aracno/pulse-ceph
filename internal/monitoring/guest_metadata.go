package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rcourtman/pulse-go-rewrite/internal/config"
	"github.com/rcourtman/pulse-go-rewrite/internal/models"
	"github.com/rcourtman/pulse-go-rewrite/pkg/proxmox"
	"github.com/rs/zerolog/log"
)

const (
	guestMetadataCacheTTL    = 5 * time.Minute
	guestMetadataEmptyTTL    = 30 * time.Second
	defaultGuestMetadataHold = 15 * time.Second

	// Guest agent timeout defaults (configurable via environment variables)
	// Increased from 3-5s to 10-15s to handle high-load environments better (refs #592)
	defaultGuestAgentFSInfoTimeout   = 15 * time.Second // GUEST_AGENT_FSINFO_TIMEOUT
	defaultGuestAgentNetworkTimeout  = 10 * time.Second // GUEST_AGENT_NETWORK_TIMEOUT
	defaultGuestAgentOSInfoTimeout   = 10 * time.Second // GUEST_AGENT_OSINFO_TIMEOUT
	defaultGuestAgentVersionTimeout  = 10 * time.Second // GUEST_AGENT_VERSION_TIMEOUT
	defaultGuestAgentVMBudget        = 30 * time.Second // GUEST_AGENT_VM_BUDGET
	defaultGuestAgentVMMaxConcurrent = 8                // GUEST_AGENT_VM_MAX_CONCURRENT
	defaultGuestAgentRetries         = 1                // GUEST_AGENT_RETRIES (0 = no retry, 1 = one retry)
	defaultGuestAgentRetryDelay      = 500 * time.Millisecond

	// Skip OS info calls after this many consecutive failures to avoid triggering buggy guest agents (refs #692)
	guestAgentOSInfoFailureThreshold = 3
)

// guestMetadataCacheEntry holds cached guest agent metadata for a VM.
type guestMetadataCacheEntry struct {
	ipAddresses        []string
	networkInterfaces  []models.GuestNetworkInterface
	osName             string
	osVersion          string
	agentVersion       string
	fetchedAt          time.Time
	osInfoFailureCount int  // Track consecutive OS info failures
	osInfoSkip         bool // Skip OS info calls after repeated failures (refs #692)
}

func guestMetadataCacheHasUsefulData(entry guestMetadataCacheEntry) bool {
	return len(entry.ipAddresses) > 0 ||
		len(entry.networkInterfaces) > 0 ||
		entry.osName != "" ||
		entry.osVersion != "" ||
		entry.agentVersion != ""
}

func guestMetadataCacheHasCompleteNetworkData(entry guestMetadataCacheEntry) bool {
	return len(entry.networkInterfaces) > 0
}

func guestMetadataCacheHasNetworkData(entry guestMetadataCacheEntry) bool {
	return len(entry.ipAddresses) > 0 || len(entry.networkInterfaces) > 0
}

func guestMetadataCacheEntryTTL(entry guestMetadataCacheEntry) time.Duration {
	// Treat identity-only and IP-only metadata as incomplete so VMs that answered
	// guest-info/version or partial network calls but not full interface inventory
	// are retried soon instead of freezing incomplete VM Summary data for minutes.
	if guestMetadataCacheHasCompleteNetworkData(entry) {
		return guestMetadataCacheTTL
	}
	return guestMetadataEmptyTTL
}

func cloneGuestDisks(src []models.Disk) []models.Disk {
	if len(src) == 0 {
		return nil
	}
	return append([]models.Disk(nil), src...)
}

func (m *Monitor) tryReserveGuestMetadataFetch(key string, now time.Time) bool {
	if m == nil {
		return false
	}
	m.guestMetadataLimiterMu.Lock()
	defer m.guestMetadataLimiterMu.Unlock()

	if next, ok := m.guestMetadataLimiter[key]; ok && now.Before(next) {
		return false
	}
	hold := m.guestMetadataHoldDuration
	if hold <= 0 {
		hold = defaultGuestMetadataHold
	}
	m.guestMetadataLimiter[key] = now.Add(hold)
	return true
}

func (m *Monitor) scheduleNextGuestMetadataFetch(key string, now time.Time) {
	if m == nil {
		return
	}
	interval := m.guestMetadataMinRefresh
	if interval <= 0 {
		interval = config.DefaultGuestMetadataMinRefresh
	}
	jitter := m.guestMetadataRefreshJitter
	if jitter > 0 && m.rng != nil {
		interval += time.Duration(m.rng.Int63n(int64(jitter)))
	}
	m.guestMetadataLimiterMu.Lock()
	m.guestMetadataLimiter[key] = now.Add(interval)
	m.guestMetadataLimiterMu.Unlock()
}

func (m *Monitor) scheduleGuestMetadataFetchForEntry(key string, now time.Time, entry guestMetadataCacheEntry) {
	if m == nil {
		return
	}
	if !guestMetadataCacheHasCompleteNetworkData(entry) {
		m.guestMetadataLimiterMu.Lock()
		m.guestMetadataLimiter[key] = now.Add(guestMetadataEmptyTTL)
		m.guestMetadataLimiterMu.Unlock()
		return
	}
	m.scheduleNextGuestMetadataFetch(key, now)
}

func (m *Monitor) deferGuestMetadataRetry(key string, now time.Time) {
	if m == nil {
		return
	}
	backoff := m.guestMetadataRetryBackoff
	if backoff <= 0 {
		backoff = config.DefaultGuestMetadataRetryBackoff
	}
	m.guestMetadataLimiterMu.Lock()
	m.guestMetadataLimiter[key] = now.Add(backoff)
	m.guestMetadataLimiterMu.Unlock()
}

func (m *Monitor) acquireGuestMetadataSlot(ctx context.Context) bool {
	if m == nil || m.guestMetadataSlots == nil {
		return true
	}
	select {
	case m.guestMetadataSlots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (m *Monitor) releaseGuestMetadataSlot() {
	if m == nil || m.guestMetadataSlots == nil {
		return
	}
	select {
	case <-m.guestMetadataSlots:
	default:
	}
}

func (m *Monitor) acquireGuestAgentWorkSlot(ctx context.Context) bool {
	if m == nil || m.guestAgentWorkSlots == nil {
		return true
	}
	select {
	case m.guestAgentWorkSlots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (m *Monitor) releaseGuestAgentWorkSlot() {
	if m == nil || m.guestAgentWorkSlots == nil {
		return
	}
	select {
	case <-m.guestAgentWorkSlots:
	default:
	}
}

func guestAgentCallBudget(timeout time.Duration, retries int) time.Duration {
	if timeout <= 0 {
		return 0
	}
	if retries < 0 {
		retries = 0
	}
	return (timeout * time.Duration(retries+1)) + (defaultGuestAgentRetryDelay * time.Duration(retries))
}

func (m *Monitor) guestAgentMetadataBudget() time.Duration {
	if m == nil {
		return defaultGuestAgentVMBudget
	}

	retries := m.guestAgentRetries
	budget := guestAgentCallBudget(m.guestAgentNetworkTimeout, retries) +
		guestAgentCallBudget(m.guestAgentOSInfoTimeout, retries) +
		guestAgentCallBudget(m.guestAgentVersionTimeout, retries)
	if budget < defaultGuestAgentVMBudget {
		budget = defaultGuestAgentVMBudget
	}
	return budget
}

func (m *Monitor) guestAgentFSInfoBudget() time.Duration {
	if m == nil {
		return defaultGuestAgentVMBudget
	}

	budget := guestAgentCallBudget(m.guestAgentFSInfoTimeout, m.guestAgentRetries)
	if budget < defaultGuestAgentVMBudget {
		budget = defaultGuestAgentVMBudget
	}
	return budget
}

func (m *Monitor) guestAgentVMContext(parent context.Context) (context.Context, context.CancelFunc) {
	return m.guestAgentContextWithBudget(parent, m.guestAgentMetadataBudget())
}

func (m *Monitor) guestAgentContextWithBudget(parent context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	if budget <= 0 {
		if m != nil && m.guestAgentVMBudget > 0 {
			budget = m.guestAgentVMBudget
		} else {
			budget = defaultGuestAgentVMBudget
		}
	}
	if parent == nil {
		return context.WithTimeout(context.Background(), budget)
	}

	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.WithCancel(parent)
		}
		if remaining < budget {
			budget = remaining
		}
	}

	return context.WithTimeout(parent, budget)
}

func (m *Monitor) runGuestAgentVMWork(parent context.Context, instanceName, nodeName, vmName string, vmid int, fn func(context.Context)) {
	if fn == nil {
		return
	}

	ctx, cancel := m.guestAgentVMContext(parent)
	defer cancel()
	if !m.acquireGuestAgentWorkSlot(ctx) {
		return
	}
	defer m.releaseGuestAgentWorkSlot()

	defer func() {
		if recovered := recover(); recovered != nil {
			log.Warn().
				Str("instance", instanceName).
				Str("node", nodeName).
				Str("vm", vmName).
				Int("vmid", vmid).
				Interface("panic", recovered).
				Msg("Recovered from guest agent processing failure; continuing with remaining VMs")
		}
	}()

	fn(ctx)
}

func (m *Monitor) runGuestAgentFSInfoWork(parent context.Context, instanceName, nodeName, vmName string, vmid int, fn func(context.Context)) {
	if fn == nil {
		return
	}

	ctx, cancel := m.guestAgentContextWithBudget(parent, m.guestAgentFSInfoBudget())
	defer cancel()
	if !m.acquireGuestAgentWorkSlot(ctx) {
		return
	}
	defer m.releaseGuestAgentWorkSlot()

	defer func() {
		if recovered := recover(); recovered != nil {
			log.Warn().
				Str("instance", instanceName).
				Str("node", nodeName).
				Str("vm", vmName).
				Int("vmid", vmid).
				Interface("panic", recovered).
				Msg("Recovered from guest agent filesystem processing failure; continuing with remaining VMs")
		}
	}()

	fn(ctx)
}

// retryGuestAgentCall executes a guest agent API call with timeout and retry logic (refs #592)
func (m *Monitor) retryGuestAgentCall(ctx context.Context, timeout time.Duration, maxRetries int, fn func(context.Context) (interface{}, error)) (interface{}, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		result, err := fn(callCtx)
		cancel()

		if err == nil {
			return result, nil
		}

		lastErr = err

		// Don't retry non-timeout errors or if this was the last attempt
		if attempt >= maxRetries || !strings.Contains(err.Error(), "timeout") {
			break
		}

		// Brief delay before retry to avoid hammering the API
		select {
		case <-time.After(defaultGuestAgentRetryDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, lastErr
}

func (m *Monitor) fetchGuestAgentMetadata(ctx context.Context, client PVEClientInterface, instanceName, nodeName, vmName string, vmid int, vmStatus *proxmox.VMStatus, allowWithoutStatus bool) ([]string, []models.GuestNetworkInterface, string, string, string) {
	key := guestMetadataCacheKey(instanceName, nodeName, vmid)
	now := time.Now()

	m.guestMetadataMu.RLock()
	cached, ok := m.guestMetadataCache[key]
	m.guestMetadataMu.RUnlock()

	agentAvailable := client != nil && ((vmStatus != nil && vmStatus.Agent.Value > 0) || allowWithoutStatus)
	if !agentAvailable {
		if ok && now.Sub(cached.fetchedAt) < guestMetadataCacheEntryTTL(cached) {
			return cloneStringSlice(cached.ipAddresses), cloneGuestNetworkInterfaces(cached.networkInterfaces), cached.osName, cached.osVersion, cached.agentVersion
		}
		m.clearGuestMetadataCache(instanceName, nodeName, vmid)
		return nil, nil, "", "", ""
	}

	if ok && now.Sub(cached.fetchedAt) < guestMetadataCacheEntryTTL(cached) {
		return cloneStringSlice(cached.ipAddresses), cloneGuestNetworkInterfaces(cached.networkInterfaces), cached.osName, cached.osVersion, cached.agentVersion
	}

	needsFetch := !ok || now.Sub(cached.fetchedAt) >= guestMetadataCacheEntryTTL(cached)
	if !needsFetch {
		return cloneStringSlice(cached.ipAddresses), cloneGuestNetworkInterfaces(cached.networkInterfaces), cached.osName, cached.osVersion, cached.agentVersion
	}

	reserved := m.tryReserveGuestMetadataFetch(key, now)
	if !reserved && ok {
		return cloneStringSlice(cached.ipAddresses), cloneGuestNetworkInterfaces(cached.networkInterfaces), cached.osName, cached.osVersion, cached.agentVersion
	}
	if !reserved && !ok {
		reserved = true
	}

	// Start with cached values as fallback in case new calls fail
	ipAddresses := cloneStringSlice(cached.ipAddresses)
	networkIfaces := cloneGuestNetworkInterfaces(cached.networkInterfaces)
	osName := cached.osName
	osVersion := cached.osVersion
	agentVersion := cached.agentVersion

	if reserved {
		if !m.acquireGuestMetadataSlot(ctx) {
			m.deferGuestMetadataRetry(key, time.Now())
			return ipAddresses, networkIfaces, osName, osVersion, agentVersion
		}
		defer m.releaseGuestMetadataSlot()
	}

	// Network interfaces with configurable timeout and retry (refs #592)
	interfaces, err := m.retryGuestAgentCall(ctx, m.guestAgentNetworkTimeout, m.guestAgentRetries, func(ctx context.Context) (interface{}, error) {
		return client.GetVMNetworkInterfaces(ctx, nodeName, vmid)
	})
	if err != nil {
		log.Debug().
			Str("instance", instanceName).
			Str("vm", vmName).
			Int("vmid", vmid).
			Err(err).
			Msg("Guest agent network interfaces unavailable")
	} else if ifaces, ok := interfaces.([]proxmox.VMNetworkInterface); ok && len(ifaces) > 0 {
		processedIPs, processedIfaces := processGuestNetworkInterfaces(ifaces)
		if len(processedIPs) > 0 || len(processedIfaces) > 0 {
			ipAddresses, networkIfaces = processedIPs, processedIfaces
		} else if len(cached.ipAddresses) == 0 && len(cached.networkInterfaces) == 0 {
			ipAddresses = nil
			networkIfaces = nil
		} else {
			log.Debug().
				Str("instance", instanceName).
				Str("vm", vmName).
				Int("vmid", vmid).
				Msg("Guest agent returned empty network metadata; preserving last known interfaces")
		}
	} else {
		if len(cached.ipAddresses) == 0 && len(cached.networkInterfaces) == 0 {
			ipAddresses = nil
			networkIfaces = nil
		}
	}

	// OS info with configurable timeout and retry (refs #592)
	// Skip OS info calls if we've seen repeated failures (refs #692 - OpenBSD qemu-ga issue)
	osInfoFailureCount := cached.osInfoFailureCount
	osInfoSkip := cached.osInfoSkip

	if !osInfoSkip {
		agentInfoRaw, err := m.retryGuestAgentCall(ctx, m.guestAgentOSInfoTimeout, m.guestAgentRetries, func(ctx context.Context) (interface{}, error) {
			return client.GetVMAgentInfo(ctx, nodeName, vmid)
		})
		if err != nil {
			if isGuestAgentOSInfoUnsupportedError(err) {
				osInfoSkip = true
				osInfoFailureCount = guestAgentOSInfoFailureThreshold
				log.Warn().
					Str("instance", instanceName).
					Str("vm", vmName).
					Int("vmid", vmid).
					Err(err).
					Msg("Guest agent OS info unsupported (missing os-release). Skipping future calls to avoid qemu-ga issues (refs #692)")
			} else {
				osInfoFailureCount++
				if osInfoFailureCount >= guestAgentOSInfoFailureThreshold {
					osInfoSkip = true
					log.Info().
						Str("instance", instanceName).
						Str("vm", vmName).
						Int("vmid", vmid).
						Int("failureCount", osInfoFailureCount).
						Msg("Guest agent OS info consistently fails, skipping future calls to avoid triggering buggy guest agents")
				} else {
					log.Debug().
						Str("instance", instanceName).
						Str("vm", vmName).
						Int("vmid", vmid).
						Int("failureCount", osInfoFailureCount).
						Err(err).
						Msg("Guest agent OS info unavailable")
				}
			}
		} else if agentInfo, ok := agentInfoRaw.(map[string]interface{}); ok && len(agentInfo) > 0 {
			extractedOSName, extractedOSVersion := extractGuestOSInfo(agentInfo)
			if extractedOSName != "" || extractedOSVersion != "" {
				osName, osVersion = extractedOSName, extractedOSVersion
			}
			osInfoFailureCount = 0 // Reset on success
			osInfoSkip = false
		} else if cached.osName == "" && cached.osVersion == "" {
			osName = ""
			osVersion = ""
		}
	} else {
		// Skipping OS info call due to repeated failures
		log.Debug().
			Str("instance", instanceName).
			Str("vm", vmName).
			Int("vmid", vmid).
			Msg("Skipping guest agent OS info call (disabled after repeated failures)")
	}

	// Agent version with configurable timeout and retry (refs #592)
	versionRaw, err := m.retryGuestAgentCall(ctx, m.guestAgentVersionTimeout, m.guestAgentRetries, func(ctx context.Context) (interface{}, error) {
		return client.GetVMAgentVersion(ctx, nodeName, vmid)
	})
	if err != nil {
		log.Debug().
			Str("instance", instanceName).
			Str("vm", vmName).
			Int("vmid", vmid).
			Err(err).
			Msg("Guest agent version unavailable")
	} else if version, ok := versionRaw.(string); ok && version != "" {
		agentVersion = version
	} else if cached.agentVersion == "" {
		agentVersion = ""
	}

	entry := guestMetadataCacheEntry{
		ipAddresses:        cloneStringSlice(ipAddresses),
		networkInterfaces:  cloneGuestNetworkInterfaces(networkIfaces),
		osName:             osName,
		osVersion:          osVersion,
		agentVersion:       agentVersion,
		fetchedAt:          time.Now(),
		osInfoFailureCount: osInfoFailureCount,
		osInfoSkip:         osInfoSkip,
	}

	m.guestMetadataMu.Lock()
	if m.guestMetadataCache == nil {
		m.guestMetadataCache = make(map[string]guestMetadataCacheEntry)
	}
	m.guestMetadataCache[key] = entry
	m.guestMetadataMu.Unlock()
	if reserved {
		m.scheduleGuestMetadataFetchForEntry(key, time.Now(), entry)
	}

	return ipAddresses, networkIfaces, osName, osVersion, agentVersion
}

func guestMetadataCacheKey(instanceName, nodeName string, vmid int) string {
	return fmt.Sprintf("%s|%s|%d", instanceName, nodeName, vmid)
}

func (m *Monitor) clearGuestMetadataCache(instanceName, nodeName string, vmid int) {
	if m == nil {
		return
	}

	key := guestMetadataCacheKey(instanceName, nodeName, vmid)
	m.guestMetadataMu.Lock()
	if m.guestMetadataCache != nil {
		delete(m.guestMetadataCache, key)
	}
	m.guestMetadataMu.Unlock()
}

func cloneStringSlice(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

func cloneGuestNetworkInterfaces(src []models.GuestNetworkInterface) []models.GuestNetworkInterface {
	if len(src) == 0 {
		return nil
	}
	dst := make([]models.GuestNetworkInterface, len(src))
	for i, iface := range src {
		dst[i] = iface
		if len(iface.Addresses) > 0 {
			dst[i].Addresses = cloneStringSlice(iface.Addresses)
		}
	}
	return dst
}

func processGuestNetworkInterfaces(raw []proxmox.VMNetworkInterface) ([]string, []models.GuestNetworkInterface) {
	ipSet := make(map[string]struct{})
	ipAddresses := make([]string, 0)
	guestIfaces := make([]models.GuestNetworkInterface, 0, len(raw))

	for _, iface := range raw {
		ifaceName := strings.TrimSpace(iface.Name)
		mac := strings.TrimSpace(iface.HardwareAddr)

		addrSet := make(map[string]struct{})
		addresses := make([]string, 0, len(iface.IPAddresses))

		for _, addr := range iface.IPAddresses {
			ip := strings.TrimSpace(addr.Address)
			if ip == "" {
				continue
			}
			lower := strings.ToLower(ip)
			if strings.HasPrefix(ip, "127.") || strings.HasPrefix(lower, "fe80") || ip == "::1" {
				continue
			}

			if _, exists := addrSet[ip]; !exists {
				addrSet[ip] = struct{}{}
				addresses = append(addresses, ip)
			}

			if _, exists := ipSet[ip]; !exists {
				ipSet[ip] = struct{}{}
				ipAddresses = append(ipAddresses, ip)
			}
		}

		if len(addresses) > 1 {
			sort.Strings(addresses)
		}

		rxBytes := parseInterfaceStat(iface.Statistics, "rx-bytes")
		txBytes := parseInterfaceStat(iface.Statistics, "tx-bytes")

		if ifaceName == "" && mac == "" && len(addresses) > 0 && rxBytes == 0 && txBytes == 0 {
			// Preserve discovered guest IPs, but do not treat a nameless/anonymous
			// interface record as complete interface inventory.
			continue
		}

		if len(addresses) == 0 && rxBytes == 0 && txBytes == 0 {
			if len(iface.IPAddresses) > 0 {
				continue
			}
			lowerName := strings.ToLower(ifaceName)
			if ifaceName == "" || lowerName == "lo" || lowerName == "loopback" {
				continue
			}
			if mac == "" || mac == "00:00:00:00:00:00" {
				continue
			}
		}

		guestIfaces = append(guestIfaces, models.GuestNetworkInterface{
			Name:      ifaceName,
			MAC:       mac,
			Addresses: addresses,
			RXBytes:   rxBytes,
			TXBytes:   txBytes,
		})
	}

	if len(ipAddresses) > 1 {
		sort.Strings(ipAddresses)
	}

	if len(guestIfaces) > 1 {
		sort.SliceStable(guestIfaces, func(i, j int) bool {
			return guestIfaces[i].Name < guestIfaces[j].Name
		})
	}

	return ipAddresses, guestIfaces
}

func parseInterfaceStat(stats interface{}, key string) int64 {
	if stats == nil {
		return 0
	}
	statsMap, ok := stats.(map[string]interface{})
	if !ok {
		return 0
	}
	val, ok := statsMap[key]
	if !ok {
		return 0
	}
	return anyToInt64(val)
}

func extractGuestOSInfo(data map[string]interface{}) (string, string) {
	if data == nil {
		return "", ""
	}

	if result, ok := data["result"]; ok {
		if resultMap, ok := result.(map[string]interface{}); ok {
			data = resultMap
		}
	}

	name := stringValue(data["name"])
	prettyName := stringValue(data["pretty-name"])
	version := stringValue(data["version"])
	versionID := stringValue(data["version-id"])

	osName := name
	if osName == "" {
		osName = prettyName
	}
	if osName == "" {
		osName = stringValue(data["id"])
	}

	osVersion := version
	if osVersion == "" && versionID != "" {
		osVersion = versionID
	}
	if osVersion == "" && prettyName != "" && prettyName != osName {
		osVersion = prettyName
	}
	if osVersion == "" {
		osVersion = stringValue(data["kernel-release"])
	}
	if osVersion == osName {
		osVersion = ""
	}

	return osName, osVersion
}

func isGuestAgentOSInfoUnsupportedError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	// OpenBSD qemu-ga emits "Failed to open file '/etc/os-release'" (refs #692)
	if strings.Contains(msg, "os-release") &&
		(strings.Contains(msg, "failed to open file") || strings.Contains(msg, "no such file or directory")) {
		return true
	}

	// Some Proxmox builds bubble up "unsupported command: guest-get-osinfo"
	if strings.Contains(msg, "guest-get-osinfo") && strings.Contains(msg, "unsupported") {
		return true
	}

	return false
}

func stringValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	case float32:
		return strings.TrimSpace(strconv.FormatFloat(float64(v), 'f', -1, 32))
	case int:
		return strconv.Itoa(v)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	default:
		return ""
	}
}

func anyToInt64(val interface{}) int64 {
	switch v := val.(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint32:
		return int64(v)
	case uint64:
		if v > math.MaxInt64 {
			return math.MaxInt64
		}
		return int64(v)
	case float32:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		if v == "" {
			return 0
		}
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parsed
		}
		if parsedFloat, err := strconv.ParseFloat(v, 64); err == nil {
			return int64(parsedFloat)
		}
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return parsed
		}
		if parsedFloat, err := v.Float64(); err == nil {
			return int64(parsedFloat)
		}
	}
	return 0
}
