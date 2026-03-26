package monitoring

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rcourtman/pulse-go-rewrite/internal/models"
	"github.com/rs/zerolog/log"
)

const (
	minRunningVMsForRepeatedMemoryCheck = 4
	minRepeatedVMsForSuspicion          = 3
	minRepeatedMemoryShare              = 0.60
	maxRepeatedMemorySampleNames        = 5
)

type repeatedVMMemoryUsage struct {
	suspicious      bool
	signature       string
	runningCount    int
	repeatedCount   int
	repeatedMemUsed int64
	sourceBreakdown []string
	sampleVMNames   []string
}

type repeatedMemoryGroup struct {
	count   int
	sources map[string]int
	names   []string
}

func filterVMsByInstance(vms []models.VM, instanceName string) []models.VM {
	filtered := make([]models.VM, 0, len(vms))
	for _, vm := range vms {
		if vm.Instance == instanceName {
			filtered = append(filtered, vm)
		}
	}
	return filtered
}

func detectRepeatedVMMemoryUsage(vms []models.VM) repeatedVMMemoryUsage {
	groups := make(map[int64]*repeatedMemoryGroup)
	runningCount := 0

	for _, vm := range vms {
		if vm.Type != "qemu" || vm.Status != "running" || vm.Memory.Total <= 0 || vm.Memory.Used <= 0 {
			continue
		}
		runningCount++

		group, ok := groups[vm.Memory.Used]
		if !ok {
			group = &repeatedMemoryGroup{
				sources: make(map[string]int),
			}
			groups[vm.Memory.Used] = group
		}
		group.count++
		source := strings.TrimSpace(vm.MemorySource)
		if source == "" {
			source = "unknown"
		}
		group.sources[source]++
		if len(group.names) < maxRepeatedMemorySampleNames {
			name := strings.TrimSpace(vm.Name)
			if name == "" {
				name = vm.ID
			}
			group.names = append(group.names, name)
		}
	}

	if runningCount < minRunningVMsForRepeatedMemoryCheck {
		return repeatedVMMemoryUsage{runningCount: runningCount}
	}

	var topUsed int64
	var topGroup *repeatedMemoryGroup
	for used, group := range groups {
		if topGroup == nil || group.count > topGroup.count {
			topUsed = used
			topGroup = group
		}
	}
	if topGroup == nil {
		return repeatedVMMemoryUsage{runningCount: runningCount}
	}

	share := float64(topGroup.count) / float64(runningCount)
	if topGroup.count < minRepeatedVMsForSuspicion || share < minRepeatedMemoryShare {
		return repeatedVMMemoryUsage{runningCount: runningCount}
	}

	breakdown := formatMemorySourceBreakdown(topGroup.sources)
	sampleNames := append([]string(nil), topGroup.names...)
	sort.Strings(sampleNames)

	return repeatedVMMemoryUsage{
		suspicious:      true,
		signature:       fmt.Sprintf("%d:%d:%s", topUsed, topGroup.count, strings.Join(breakdown, ",")),
		runningCount:    runningCount,
		repeatedCount:   topGroup.count,
		repeatedMemUsed: topUsed,
		sourceBreakdown: breakdown,
		sampleVMNames:   sampleNames,
	}
}

func formatMemorySourceBreakdown(counts map[string]int) []string {
	type sourceCount struct {
		source string
		count  int
	}
	entries := make([]sourceCount, 0, len(counts))
	for source, count := range counts {
		entries = append(entries, sourceCount{source: source, count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count == entries[j].count {
			return entries[i].source < entries[j].source
		}
		return entries[i].count > entries[j].count
	})

	breakdown := make([]string, 0, len(entries))
	for _, entry := range entries {
		breakdown = append(breakdown, fmt.Sprintf("%s:%d", entry.source, entry.count))
	}
	return breakdown
}

func (m *Monitor) logSuspiciousRepeatedVMMemoryUsage(instanceName string, currentVMs []models.VM, previousVMs []models.VM) {
	current := detectRepeatedVMMemoryUsage(currentVMs)
	if !current.suspicious {
		return
	}

	previous := detectRepeatedVMMemoryUsage(previousVMs)
	if previous.suspicious && previous.signature == current.signature {
		return
	}

	log.Warn().
		Str("instance", instanceName).
		Int("runningVMs", current.runningCount).
		Int("repeatedVMs", current.repeatedCount).
		Int64("repeatedMemoryUsedBytes", current.repeatedMemUsed).
		Strs("memorySources", current.sourceBreakdown).
		Strs("sampleVMs", current.sampleVMNames).
		Msg("Suspicious repeated VM memory-used pattern detected")
}

func stabilizeSuspiciousRepeatedVMMemory(vms []models.VM, alertVMs []models.VM, snapshots []GuestMemorySnapshot, previousVMs []models.VM, now time.Time) int {
	current := detectRepeatedLowTrustVMMemoryUsage(vms, snapshots)
	if !current.suspicious {
		current = detectRepeatedVMMemoryUsage(vms)
	}
	if !current.suspicious {
		return 0
	}

	prevByID := make(map[string]models.VM, len(previousVMs))
	for _, prev := range previousVMs {
		prevByID[prev.ID] = prev
	}

	applied := 0
	for i := range vms {
		vm := &vms[i]
		if vm.Type != "qemu" || vm.Status != "running" || vm.Memory.Total <= 0 || vm.Memory.Used != current.repeatedMemUsed {
			continue
		}
		if vmMemorySourceReliability(vm.MemorySource) > vmMemorySourceReliabilityFallback {
			continue
		}

		prev, ok := prevByID[vm.ID]
		if !ok || !shouldStabilizeSuspiciousRepeatedVMMemory(prev, *vm, now) {
			continue
		}

		currentBalloon := vm.Memory.Balloon
		vm.Memory = prev.Memory
		if currentBalloon > 0 {
			vm.Memory.Balloon = currentBalloon
		}
		vm.MemorySource = "previous-snapshot"

		if len(alertVMs) == len(vms) {
			alertVMs[i].Memory = vm.Memory
			alertVMs[i].MemorySource = vm.MemorySource
		}

		if len(snapshots) == len(vms) {
			snapshots[i].Memory = vm.Memory
			snapshots[i].MemorySource = vm.MemorySource
			snapshots[i].Notes = appendSnapshotNote(snapshots[i].Notes, "preserved-previous-memory-after-repeated-low-trust-pattern")
		}

		applied++
	}

	if applied == 0 {
		return 0
	}

	log.Warn().
		Int("runningVMs", current.runningCount).
		Int("repeatedVMs", current.repeatedCount).
		Int("stabilizedVMs", applied).
		Int64("repeatedMemoryUsedBytes", current.repeatedMemUsed).
		Strs("memorySources", current.sourceBreakdown).
		Strs("sampleVMs", current.sampleVMNames).
		Msg("Stabilized suspicious repeated VM memory-used pattern with previous readings")

	return applied
}

func detectRepeatedLowTrustVMMemoryUsage(vms []models.VM, snapshots []GuestMemorySnapshot) repeatedVMMemoryUsage {
	if len(vms) == 0 || len(vms) != len(snapshots) {
		return repeatedVMMemoryUsage{}
	}

	groups := make(map[int64]*repeatedMemoryGroup)
	runningCount := 0

	for i := range vms {
		vm := vms[i]
		if vm.Type != "qemu" || vm.Status != "running" || vm.Memory.Total <= 0 {
			continue
		}
		runningCount++

		used, source, ok := lowTrustMemoryCandidate(vm, snapshots[i])
		if !ok || used <= 0 {
			continue
		}

		group, exists := groups[used]
		if !exists {
			group = &repeatedMemoryGroup{sources: make(map[string]int)}
			groups[used] = group
		}
		group.count++
		group.sources[source]++
		if len(group.names) < maxRepeatedMemorySampleNames {
			name := strings.TrimSpace(vm.Name)
			if name == "" {
				name = vm.ID
			}
			group.names = append(group.names, name)
		}
	}

	if runningCount < minRunningVMsForRepeatedMemoryCheck {
		return repeatedVMMemoryUsage{runningCount: runningCount}
	}

	var topUsed int64
	var topGroup *repeatedMemoryGroup
	for used, group := range groups {
		if topGroup == nil || group.count > topGroup.count {
			topUsed = used
			topGroup = group
		}
	}
	if topGroup == nil {
		return repeatedVMMemoryUsage{runningCount: runningCount}
	}

	share := float64(topGroup.count) / float64(runningCount)
	if topGroup.count < minRepeatedVMsForSuspicion || share < minRepeatedMemoryShare {
		return repeatedVMMemoryUsage{runningCount: runningCount}
	}

	breakdown := formatMemorySourceBreakdown(topGroup.sources)
	sampleNames := append([]string(nil), topGroup.names...)
	sort.Strings(sampleNames)

	return repeatedVMMemoryUsage{
		suspicious:      true,
		signature:       fmt.Sprintf("%d:%d:%s", topUsed, topGroup.count, strings.Join(breakdown, ",")),
		runningCount:    runningCount,
		repeatedCount:   topGroup.count,
		repeatedMemUsed: topUsed,
		sourceBreakdown: breakdown,
		sampleVMNames:   sampleNames,
	}
}

func lowTrustMemoryCandidate(vm models.VM, snapshot GuestMemorySnapshot) (int64, string, bool) {
	source := strings.TrimSpace(snapshot.MemorySource)
	if source == "" {
		source = strings.TrimSpace(vm.MemorySource)
	}

	switch {
	case source == "status-mem":
		return clampLowTrustMemoryCandidate(vm.Memory.Total, snapshot.Raw.StatusMem, "status-mem")
	case source == "status-freemem":
		if snapshot.Raw.StatusFreeMem == 0 || uint64(vm.Memory.Total) < snapshot.Raw.StatusFreeMem {
			return 0, "", false
		}
		return int64(uint64(vm.Memory.Total) - snapshot.Raw.StatusFreeMem), "status-freemem", true
	case source == "listing-mem" || source == "cluster-resources":
		return clampLowTrustMemoryCandidate(vm.Memory.Total, snapshot.Raw.ListingMem, source)
	case source == "meminfo-total-minus-used":
		if snapshot.Raw.MemInfoTotalMinusUsed == 0 || uint64(vm.Memory.Total) < snapshot.Raw.MemInfoTotalMinusUsed {
			return 0, "", false
		}
		return int64(uint64(vm.Memory.Total) - snapshot.Raw.MemInfoTotalMinusUsed), "meminfo-total-minus-used", true
	case source == "previous-snapshot":
		if !snapshotHasNote(snapshot.Notes, "preserved-previous-memory-after-low-trust-fallback") &&
			!snapshotHasNote(snapshot.Notes, "preserved-previous-memory-after-repeated-low-trust-pattern") {
			return 0, "", false
		}
		switch {
		case snapshot.Raw.StatusMem > 0:
			return clampLowTrustMemoryCandidate(vm.Memory.Total, snapshot.Raw.StatusMem, "status-mem")
		case snapshot.Raw.MemInfoTotalMinusUsed > 0 && uint64(vm.Memory.Total) >= snapshot.Raw.MemInfoTotalMinusUsed:
			return int64(uint64(vm.Memory.Total) - snapshot.Raw.MemInfoTotalMinusUsed), "meminfo-total-minus-used", true
		case snapshot.Raw.ListingMem > 0:
			return clampLowTrustMemoryCandidate(vm.Memory.Total, snapshot.Raw.ListingMem, "listing-mem")
		}
	}

	return 0, "", false
}

func clampLowTrustMemoryCandidate(total int64, used uint64, source string) (int64, string, bool) {
	if total <= 0 || used == 0 {
		return 0, "", false
	}
	if used > uint64(total) {
		used = uint64(total)
	}
	return int64(used), source, true
}

func snapshotHasNote(notes []string, target string) bool {
	for _, note := range notes {
		if note == target {
			return true
		}
	}
	return false
}

func shouldStabilizeSuspiciousRepeatedVMMemory(prev, current models.VM, now time.Time) bool {
	if current.Type != "qemu" || current.Status != "running" || prev.Type != "qemu" || prev.Status != "running" {
		return false
	}
	if prev.Memory.Total <= 0 || prev.Memory.Used < 0 {
		return false
	}
	if prev.LastSeen.IsZero() || now.Sub(prev.LastSeen) > vmMemoryCarryForwardMaxAge {
		return false
	}
	if current.Memory.Total > 0 && prev.Memory.Total > 0 && prev.Memory.Total != current.Memory.Total {
		return false
	}

	prevReliability := vmMemorySourceReliability(prev.MemorySource)
	if prev.MemorySource != "previous-snapshot" && prevReliability < vmMemorySourceReliabilityTrusted {
		return false
	}
	if vmMemorySourceReliability(current.MemorySource) > vmMemorySourceReliabilityFallback {
		return false
	}
	if prev.Memory.Usage > 0 && absFloat64(prev.Memory.Usage-current.Memory.Usage) < vmMemoryCarryForwardMinUsageDelta {
		return false
	}

	return true
}

func appendSnapshotNote(notes []string, note string) []string {
	for _, existing := range notes {
		if existing == note {
			return notes
		}
	}
	return append(notes, note)
}

func absFloat64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
