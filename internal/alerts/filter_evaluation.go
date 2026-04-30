package alerts

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rcourtman/pulse-go-rewrite/internal/models"
	"github.com/rs/zerolog/log"
)

// evaluateFilterCondition evaluates a single filter condition against a guest
func (m *Manager) evaluateFilterCondition(guest interface{}, condition FilterCondition) bool {
	switch g := guest.(type) {
	case models.VM:
		return m.evaluateVMCondition(g, condition)
	case models.Container:
		return m.evaluateContainerCondition(g, condition)
	default:
		return false
	}
}

// guestMetrics holds common metrics for filter evaluation
type guestMetrics struct {
	CPU        float64 // CPU usage as percentage (0-100)
	MemUsage   float64 // Memory usage percentage
	DiskUsage  float64 // Disk usage percentage
	DiskRead   int64   // Bytes/s
	DiskWrite  int64   // Bytes/s
	NetworkIn  int64   // Bytes/s
	NetworkOut int64   // Bytes/s
	Name       string
	Node       string
	ID         string
	Status     string
}

// extractGuestMetrics extracts common metrics from a VM or Container
func extractGuestMetrics(guest interface{}) (guestMetrics, bool) {
	switch g := guest.(type) {
	case models.VM:
		return guestMetrics{
			CPU:        g.CPU * 100,
			MemUsage:   g.Memory.Usage,
			DiskUsage:  g.Disk.Usage,
			DiskRead:   g.DiskRead,
			DiskWrite:  g.DiskWrite,
			NetworkIn:  g.NetworkIn,
			NetworkOut: g.NetworkOut,
			Name:       g.Name,
			Node:       g.Node,
			ID:         g.ID,
			Status:     g.Status,
		}, true
	case models.Container:
		return guestMetrics{
			CPU:        g.CPU * 100,
			MemUsage:   g.Memory.Usage,
			DiskUsage:  g.Disk.Usage,
			DiskRead:   g.DiskRead,
			DiskWrite:  g.DiskWrite,
			NetworkIn:  g.NetworkIn,
			NetworkOut: g.NetworkOut,
			Name:       g.Name,
			Node:       g.Node,
			ID:         g.ID,
			Status:     g.Status,
		}, true
	default:
		return guestMetrics{}, false
	}
}

// evaluateGuestCondition evaluates a filter condition against guest metrics
func evaluateGuestCondition(metrics guestMetrics, condition FilterCondition) bool {
	switch condition.Type {
	case "metric":
		value := 0.0
		switch strings.ToLower(condition.Field) {
		case "cpu":
			value = metrics.CPU
		case "memory":
			value = metrics.MemUsage
		case "disk":
			value = metrics.DiskUsage
		case "diskread":
			value = float64(metrics.DiskRead) / 1024 / 1024 // Convert to MB/s
		case "diskwrite":
			value = float64(metrics.DiskWrite) / 1024 / 1024
		case "networkin":
			value = float64(metrics.NetworkIn) / 1024 / 1024
		case "networkout":
			value = float64(metrics.NetworkOut) / 1024 / 1024
		default:
			return false
		}
		return evaluateNumericCondition(value, condition)

	case "text":
		searchValue := strings.ToLower(fmt.Sprintf("%v", condition.Value))
		switch strings.ToLower(condition.Field) {
		case "name":
			return strings.Contains(strings.ToLower(metrics.Name), searchValue)
		case "node":
			return strings.Contains(strings.ToLower(metrics.Node), searchValue)
		case "vmid":
			return strings.Contains(metrics.ID, searchValue)
		}

	case "raw":
		if condition.RawText != "" {
			term := strings.ToLower(condition.RawText)
			return strings.Contains(strings.ToLower(metrics.Name), term) ||
				strings.Contains(metrics.ID, term) ||
				strings.Contains(strings.ToLower(metrics.Node), term) ||
				strings.Contains(strings.ToLower(metrics.Status), term)
		}
	}

	return false
}

// evaluateNumericCondition evaluates a numeric comparison
func evaluateNumericCondition(value float64, condition FilterCondition) bool {
	condValue, ok := condition.Value.(float64)
	if !ok {
		// Try to convert from int
		if intVal, ok := condition.Value.(int); ok {
			condValue = float64(intVal)
		} else {
			return false
		}
	}

	switch condition.Operator {
	case ">":
		return value > condValue
	case "<":
		return value < condValue
	case ">=":
		return value >= condValue
	case "<=":
		return value <= condValue
	case "=", "==":
		return value >= condValue-0.5 && value <= condValue+0.5
	}
	return false
}

// evaluateVMCondition evaluates a filter condition against a VM
func (m *Manager) evaluateVMCondition(vm models.VM, condition FilterCondition) bool {
	metrics, _ := extractGuestMetrics(vm)
	return evaluateGuestCondition(metrics, condition)
}

// evaluateContainerCondition evaluates a filter condition against a Container
func (m *Manager) evaluateContainerCondition(ct models.Container, condition FilterCondition) bool {
	metrics, _ := extractGuestMetrics(ct)
	return evaluateGuestCondition(metrics, condition)
}

// evaluateFilterStack evaluates a filter stack against a guest
func (m *Manager) evaluateFilterStack(guest interface{}, stack FilterStack) bool {
	if len(stack.Filters) == 0 {
		return true
	}

	results := make([]bool, len(stack.Filters))
	for i, filter := range stack.Filters {
		results[i] = m.evaluateFilterCondition(guest, filter)
	}

	// Apply logical operator
	if stack.LogicalOperator == "AND" {
		for _, result := range results {
			if !result {
				return false
			}
		}
		return true
	}
	// OR
	for _, result := range results {
		if result {
			return true
		}
	}
	return false
}

type guestOverrideIdentity struct {
	instance string
	node     string
	vmid     int
}

func stableGuestOverrideKey(instance, node string, vmid int) string {
	instance = strings.TrimSpace(instance)
	node = strings.TrimSpace(node)
	if instance == "" {
		instance = node
	}
	return fmt.Sprintf("%s-%d", instance, vmid)
}

func parseCanonicalGuestKey(guestID string) (guestOverrideIdentity, bool) {
	parts := strings.Split(strings.TrimSpace(guestID), ":")
	if len(parts) != 3 {
		return guestOverrideIdentity{}, false
	}

	vmid, err := strconv.Atoi(parts[2])
	if err != nil {
		return guestOverrideIdentity{}, false
	}

	instance := strings.TrimSpace(parts[0])
	node := strings.TrimSpace(parts[1])
	if strings.EqualFold(node, "osd") {
		return guestOverrideIdentity{}, false
	}
	if instance == "" {
		instance = node
	}
	if instance == "" || node == "" {
		return guestOverrideIdentity{}, false
	}

	return guestOverrideIdentity{
		instance: instance,
		node:     node,
		vmid:     vmid,
	}, true
}

func guestOverrideIdentityFromGuestOrID(guest interface{}, guestID string) (guestOverrideIdentity, bool) {
	switch g := guest.(type) {
	case models.VM:
		instance := strings.TrimSpace(g.Instance)
		node := strings.TrimSpace(g.Node)
		if instance == "" {
			instance = node
		}
		if instance == "" || node == "" || g.VMID <= 0 {
			return guestOverrideIdentity{}, false
		}
		return guestOverrideIdentity{
			instance: instance,
			node:     node,
			vmid:     g.VMID,
		}, true
	case models.Container:
		instance := strings.TrimSpace(g.Instance)
		node := strings.TrimSpace(g.Node)
		if instance == "" {
			instance = node
		}
		if instance == "" || node == "" || g.VMID <= 0 {
			return guestOverrideIdentity{}, false
		}
		return guestOverrideIdentity{
			instance: instance,
			node:     node,
			vmid:     g.VMID,
		}, true
	default:
		return parseCanonicalGuestKey(guestID)
	}
}

func isCanonicalGuestOverrideKey(key string, ident guestOverrideIdentity) bool {
	parsed, ok := parseCanonicalGuestKey(key)
	if !ok {
		return false
	}
	return parsed.instance == ident.instance && parsed.vmid == ident.vmid
}

func (m *Manager) lookupGuestOverride(guest interface{}, guestID string) (ThresholdConfig, bool) {
	if guestID = strings.TrimSpace(guestID); guestID != "" {
		if override, exists := m.config.Overrides[guestID]; exists {
			ident, ok := guestOverrideIdentityFromGuestOrID(guest, guestID)
			if !ok || ident.instance == ident.node {
				return override, true
			}

			stableKey := stableGuestOverrideKey(ident.instance, ident.node, ident.vmid)
			if guestID != stableKey {
				// Cluster guest overrides should live under the stable instance-vmid key so
				// they survive node moves. Preserve the override in-memory immediately.
				m.config.Overrides[stableKey] = override
				delete(m.config.Overrides, guestID)
			}
			return override, true
		}
	}

	ident, ok := guestOverrideIdentityFromGuestOrID(guest, guestID)
	if !ok {
		return ThresholdConfig{}, false
	}

	stableKey := stableGuestOverrideKey(ident.instance, ident.node, ident.vmid)
	if override, exists := m.config.Overrides[stableKey]; exists {
		return override, true
	}

	if ident.instance != ident.node {
		legacyKey := fmt.Sprintf("%s-%s-%d", ident.instance, ident.node, ident.vmid)
		if legacyOverride, legacyExists := m.config.Overrides[legacyKey]; legacyExists {
			log.Info().
				Str("legacyID", legacyKey).
				Str("newID", stableKey).
				Msg("Migrating guest override from legacy cluster-node ID format")

			m.config.Overrides[stableKey] = legacyOverride
			delete(m.config.Overrides, legacyKey)
			return legacyOverride, true
		}

		for key, override := range m.config.Overrides {
			if !isCanonicalGuestOverrideKey(key, ident) {
				continue
			}
			m.config.Overrides[stableKey] = override
			delete(m.config.Overrides, key)

			log.Info().
				Str("legacyID", key).
				Str("newID", stableKey).
				Msg("Migrating clustered guest override from node-bound canonical key to stable key")

			return override, true
		}
	}

	return ThresholdConfig{}, false
}

// getGuestThresholds returns the appropriate thresholds for a guest
// Priority: Guest-specific overrides > Custom rules (by priority) > Global defaults
func (m *Manager) getGuestThresholds(guest interface{}, guestID string) ThresholdConfig {
	// Start with defaults
	thresholds := m.config.GuestDefaults

	// Check custom rules (sorted by priority, highest first)
	var applicableRule *CustomAlertRule
	highestPriority := -1

	for i := range m.config.CustomRules {
		rule := &m.config.CustomRules[i]
		if !rule.Enabled {
			continue
		}

		// Check if this rule applies to the guest
		if m.evaluateFilterStack(guest, rule.FilterConditions) {
			if rule.Priority > highestPriority {
				applicableRule = rule
				highestPriority = rule.Priority
			}
		}
	}

	// Apply custom rule thresholds if found
	if applicableRule != nil {
		if applicableRule.Thresholds.CPU != nil {
			thresholds.CPU = ensureHysteresisThreshold(applicableRule.Thresholds.CPU)
		} else if applicableRule.Thresholds.CPULegacy != nil {
			thresholds.CPU = m.convertLegacyThreshold(applicableRule.Thresholds.CPULegacy)
		}
		if applicableRule.Thresholds.Memory != nil {
			thresholds.Memory = ensureHysteresisThreshold(applicableRule.Thresholds.Memory)
		} else if applicableRule.Thresholds.MemoryLegacy != nil {
			thresholds.Memory = m.convertLegacyThreshold(applicableRule.Thresholds.MemoryLegacy)
		}
		if applicableRule.Thresholds.Disk != nil {
			thresholds.Disk = ensureHysteresisThreshold(applicableRule.Thresholds.Disk)
		} else if applicableRule.Thresholds.DiskLegacy != nil {
			thresholds.Disk = m.convertLegacyThreshold(applicableRule.Thresholds.DiskLegacy)
		}
		if applicableRule.Thresholds.DiskRead != nil {
			thresholds.DiskRead = ensureHysteresisThreshold(applicableRule.Thresholds.DiskRead)
		} else if applicableRule.Thresholds.DiskReadLegacy != nil {
			thresholds.DiskRead = m.convertLegacyThreshold(applicableRule.Thresholds.DiskReadLegacy)
		}
		if applicableRule.Thresholds.DiskWrite != nil {
			thresholds.DiskWrite = ensureHysteresisThreshold(applicableRule.Thresholds.DiskWrite)
		} else if applicableRule.Thresholds.DiskWriteLegacy != nil {
			thresholds.DiskWrite = m.convertLegacyThreshold(applicableRule.Thresholds.DiskWriteLegacy)
		}
		if applicableRule.Thresholds.NetworkIn != nil {
			thresholds.NetworkIn = ensureHysteresisThreshold(applicableRule.Thresholds.NetworkIn)
		} else if applicableRule.Thresholds.NetworkInLegacy != nil {
			thresholds.NetworkIn = m.convertLegacyThreshold(applicableRule.Thresholds.NetworkInLegacy)
		}
		if applicableRule.Thresholds.NetworkOut != nil {
			thresholds.NetworkOut = ensureHysteresisThreshold(applicableRule.Thresholds.NetworkOut)
		} else if applicableRule.Thresholds.NetworkOutLegacy != nil {
			thresholds.NetworkOut = m.convertLegacyThreshold(applicableRule.Thresholds.NetworkOutLegacy)
		}
		if applicableRule.Thresholds.DisableConnectivity {
			thresholds.DisableConnectivity = true
		}
		if applicableRule.Thresholds.Backup != nil {
			thresholds.Backup = applicableRule.Thresholds.Backup
		}
		if applicableRule.Thresholds.Snapshot != nil {
			thresholds.Snapshot = applicableRule.Thresholds.Snapshot
		}

		log.Debug().
			Str("guest", guestID).
			Str("rule", applicableRule.Name).
			Int("priority", applicableRule.Priority).
			Msg("Applied custom alert rule")
	}

	// Finally check guest-specific overrides (highest priority).
	// Cluster guest overrides should resolve via a stable instance-vmid key so they
	// continue to apply after node moves. Older node-bound formats are migrated lazily.
	override, exists := m.lookupGuestOverride(guest, guestID)

	if exists {
		// Apply the disabled flag if set
		if override.Disabled {
			thresholds.Disabled = true
		}
		if override.DisableConnectivity {
			thresholds.DisableConnectivity = true
		}

		if override.CPU != nil {
			thresholds.CPU = ensureHysteresisThreshold(override.CPU)
		} else if override.CPULegacy != nil {
			thresholds.CPU = m.convertLegacyThreshold(override.CPULegacy)
		}
		if override.Memory != nil {
			thresholds.Memory = ensureHysteresisThreshold(override.Memory)
		} else if override.MemoryLegacy != nil {
			thresholds.Memory = m.convertLegacyThreshold(override.MemoryLegacy)
		}
		if override.Disk != nil {
			thresholds.Disk = ensureHysteresisThreshold(override.Disk)
		} else if override.DiskLegacy != nil {
			thresholds.Disk = m.convertLegacyThreshold(override.DiskLegacy)
		}
		if override.DiskRead != nil {
			thresholds.DiskRead = ensureHysteresisThreshold(override.DiskRead)
		} else if override.DiskReadLegacy != nil {
			thresholds.DiskRead = m.convertLegacyThreshold(override.DiskReadLegacy)
		}
		if override.DiskWrite != nil {
			thresholds.DiskWrite = ensureHysteresisThreshold(override.DiskWrite)
		} else if override.DiskWriteLegacy != nil {
			thresholds.DiskWrite = m.convertLegacyThreshold(override.DiskWriteLegacy)
		}
		if override.NetworkIn != nil {
			thresholds.NetworkIn = ensureHysteresisThreshold(override.NetworkIn)
		} else if override.NetworkInLegacy != nil {
			thresholds.NetworkIn = m.convertLegacyThreshold(override.NetworkInLegacy)
		}
		if override.NetworkOut != nil {
			thresholds.NetworkOut = ensureHysteresisThreshold(override.NetworkOut)
		} else if override.NetworkOutLegacy != nil {
			thresholds.NetworkOut = m.convertLegacyThreshold(override.NetworkOutLegacy)
		}
		if override.Backup != nil {
			thresholds.Backup = override.Backup
		}
		if override.Snapshot != nil {
			thresholds.Snapshot = override.Snapshot
		}
	}

	return thresholds
}

// tryLegacyOverrideMigration attempts to find and migrate legacy override formats
// into the canonical guest ID format (instance:node:vmid).
func (m *Manager) tryLegacyOverrideMigration(guest interface{}, guestID string) (ThresholdConfig, bool) {
	var node string
	var vmid int
	var instance string

	// Extract node, vmid, and instance from the guest object
	switch g := guest.(type) {
	case models.VM:
		node = g.Node
		vmid = g.VMID
		instance = g.Instance
	case models.Container:
		node = g.Node
		vmid = g.VMID
		instance = g.Instance
	default:
		// Not a VM or container, no legacy migration possible
		return ThresholdConfig{}, false
	}

	// Try legacy format: instance-node-VMID
	if instance != node {
		legacyID := fmt.Sprintf("%s-%s-%d", instance, node, vmid)
		if legacyOverride, legacyExists := m.config.Overrides[legacyID]; legacyExists {
			log.Info().
				Str("legacyID", legacyID).
				Str("newID", guestID).
				Msg("Migrating guest override from legacy ID format")

			// Move to new ID
			m.config.Overrides[guestID] = legacyOverride
			delete(m.config.Overrides, legacyID)

			return legacyOverride, true
		}
	}

	// Try legacy clustered format: instance-VMID
	if instance != "" {
		legacyID := fmt.Sprintf("%s-%d", instance, vmid)
		if legacyOverride, legacyExists := m.config.Overrides[legacyID]; legacyExists {
			log.Info().
				Str("legacyID", legacyID).
				Str("newID", guestID).
				Msg("Migrating guest override from legacy cluster ID format")

			m.config.Overrides[guestID] = legacyOverride
			delete(m.config.Overrides, legacyID)

			return legacyOverride, true
		}
	}

	// Try standalone format: node-VMID
	if instance == node {
		legacyID := fmt.Sprintf("%s-%d", node, vmid)
		if legacyOverride, legacyExists := m.config.Overrides[legacyID]; legacyExists {
			log.Info().
				Str("legacyID", legacyID).
				Str("newID", guestID).
				Msg("Migrating guest override from legacy standalone ID format")

			// Move to new ID
			m.config.Overrides[guestID] = legacyOverride
			delete(m.config.Overrides, legacyID)

			return legacyOverride, true
		}
	}

	return ThresholdConfig{}, false
}
