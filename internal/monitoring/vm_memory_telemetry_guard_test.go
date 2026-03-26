package monitoring

import (
	"testing"
	"time"

	"github.com/rcourtman/pulse-go-rewrite/internal/models"
)

func TestDetectRepeatedVMMemoryUsage(t *testing.T) {
	tests := []struct {
		name            string
		vms             []models.VM
		wantSuspicious  bool
		wantRepeated    int
		wantRunning     int
		wantRepeatedMem int64
	}{
		{
			name: "detects suspicious repeated memory used values",
			vms: []models.VM{
				{Name: "vm1", Type: "qemu", Status: "running", MemorySource: "status-mem", Memory: models.Memory{Total: 8 << 30, Used: 3 << 30}},
				{Name: "vm2", Type: "qemu", Status: "running", MemorySource: "status-mem", Memory: models.Memory{Total: 8 << 30, Used: 3 << 30}},
				{Name: "vm3", Type: "qemu", Status: "running", MemorySource: "status-mem", Memory: models.Memory{Total: 8 << 30, Used: 3 << 30}},
				{Name: "vm4", Type: "qemu", Status: "running", MemorySource: "status-mem", Memory: models.Memory{Total: 8 << 30, Used: 3 << 30}},
				{Name: "vm5", Type: "qemu", Status: "running", MemorySource: "rrd-memavailable", Memory: models.Memory{Total: 8 << 30, Used: 2 << 30}},
			},
			wantSuspicious:  true,
			wantRepeated:    4,
			wantRunning:     5,
			wantRepeatedMem: 3 << 30,
		},
		{
			name: "ignores patterns under share threshold",
			vms: []models.VM{
				{Name: "vm1", Type: "qemu", Status: "running", MemorySource: "status-mem", Memory: models.Memory{Total: 8 << 30, Used: 3 << 30}},
				{Name: "vm2", Type: "qemu", Status: "running", MemorySource: "status-mem", Memory: models.Memory{Total: 8 << 30, Used: 3 << 30}},
				{Name: "vm3", Type: "qemu", Status: "running", MemorySource: "rrd-memavailable", Memory: models.Memory{Total: 8 << 30, Used: 2 << 30}},
				{Name: "vm4", Type: "qemu", Status: "running", MemorySource: "rrd-memavailable", Memory: models.Memory{Total: 8 << 30, Used: 1 << 30}},
			},
			wantSuspicious: false,
			wantRepeated:   0,
			wantRunning:    4,
		},
		{
			name: "ignores when not enough running qemu guests",
			vms: []models.VM{
				{Name: "vm1", Type: "qemu", Status: "running", MemorySource: "status-mem", Memory: models.Memory{Total: 8 << 30, Used: 3 << 30}},
				{Name: "vm2", Type: "qemu", Status: "running", MemorySource: "status-mem", Memory: models.Memory{Total: 8 << 30, Used: 3 << 30}},
				{Name: "vm3", Type: "qemu", Status: "stopped", MemorySource: "powered-off", Memory: models.Memory{Total: 8 << 30, Used: 0}},
			},
			wantSuspicious: false,
			wantRepeated:   0,
			wantRunning:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectRepeatedVMMemoryUsage(tt.vms)
			if got.suspicious != tt.wantSuspicious {
				t.Fatalf("suspicious = %v, want %v", got.suspicious, tt.wantSuspicious)
			}
			if got.repeatedCount != tt.wantRepeated {
				t.Fatalf("repeatedCount = %d, want %d", got.repeatedCount, tt.wantRepeated)
			}
			if got.runningCount != tt.wantRunning {
				t.Fatalf("runningCount = %d, want %d", got.runningCount, tt.wantRunning)
			}
			if got.repeatedMemUsed != tt.wantRepeatedMem {
				t.Fatalf("repeatedMemUsed = %d, want %d", got.repeatedMemUsed, tt.wantRepeatedMem)
			}
		})
	}
}

func TestFilterVMsByInstance(t *testing.T) {
	vms := []models.VM{
		{ID: "a", Instance: "pve1"},
		{ID: "b", Instance: "pve2"},
		{ID: "c", Instance: "pve1"},
	}

	filtered := filterVMsByInstance(vms, "pve1")
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2", len(filtered))
	}
	if filtered[0].ID != "a" || filtered[1].ID != "c" {
		t.Fatalf("unexpected filtered result: %#v", filtered)
	}
}

func TestStabilizeSuspiciousRepeatedVMMemory(t *testing.T) {
	now := time.Now()
	total := int64(8 << 30)

	current := []models.VM{
		{
			ID:           "vm1",
			Name:         "vm1",
			Type:         "qemu",
			Status:       "running",
			MemorySource: "status-mem",
			Memory:       models.Memory{Total: total, Used: total, Usage: 100, Balloon: 2 << 30},
			LastSeen:     now,
		},
		{
			ID:           "vm2",
			Name:         "vm2",
			Type:         "qemu",
			Status:       "running",
			MemorySource: "status-mem",
			Memory:       models.Memory{Total: total, Used: total, Usage: 100},
			LastSeen:     now,
		},
		{
			ID:           "vm3",
			Name:         "vm3",
			Type:         "qemu",
			Status:       "running",
			MemorySource: "status-mem",
			Memory:       models.Memory{Total: total, Used: total, Usage: 100},
			LastSeen:     now,
		},
		{
			ID:           "vm4",
			Name:         "vm4",
			Type:         "qemu",
			Status:       "running",
			MemorySource: "rrd-memavailable",
			Memory:       models.Memory{Total: total, Used: 2 << 30, Free: 6 << 30, Usage: 25},
			LastSeen:     now,
		},
	}
	alertVMs := append([]models.VM(nil), current...)
	snapshots := []GuestMemorySnapshot{
		{Name: "vm1", Status: "running", RetrievedAt: now, MemorySource: "status-mem", Memory: current[0].Memory},
		{Name: "vm2", Status: "running", RetrievedAt: now, MemorySource: "status-mem", Memory: current[1].Memory},
		{Name: "vm3", Status: "running", RetrievedAt: now, MemorySource: "status-mem", Memory: current[2].Memory},
		{Name: "vm4", Status: "running", RetrievedAt: now, MemorySource: "rrd-memavailable", Memory: current[3].Memory},
	}
	previous := []models.VM{
		{
			ID:           "vm1",
			Name:         "vm1",
			Type:         "qemu",
			Status:       "running",
			MemorySource: "guest-agent-meminfo",
			Memory:       models.Memory{Total: total, Used: 3 << 30, Free: 5 << 30, Usage: 37.5, Balloon: 1 << 30},
			LastSeen:     now,
		},
		{
			ID:           "vm2",
			Name:         "vm2",
			Type:         "qemu",
			Status:       "running",
			MemorySource: "previous-snapshot",
			Memory:       models.Memory{Total: total, Used: 4 << 30, Free: 4 << 30, Usage: 50},
			LastSeen:     now,
		},
		{
			ID:           "vm3",
			Name:         "vm3",
			Type:         "qemu",
			Status:       "running",
			MemorySource: "status-mem",
			Memory:       models.Memory{Total: total, Used: 7 << 30, Free: 1 << 30, Usage: 87.5},
			LastSeen:     now,
		},
	}

	applied := stabilizeSuspiciousRepeatedVMMemory(current, alertVMs, snapshots, previous, now)
	if applied != 2 {
		t.Fatalf("applied = %d, want 2", applied)
	}

	if current[0].MemorySource != "previous-snapshot" || current[0].Memory.Used != 3<<30 {
		t.Fatalf("vm1 memory = %#v source=%q, want previous-snapshot with preserved used", current[0].Memory, current[0].MemorySource)
	}
	if current[0].Memory.Balloon != 2<<30 {
		t.Fatalf("vm1 balloon = %d, want current balloon preserved", current[0].Memory.Balloon)
	}
	if current[1].MemorySource != "previous-snapshot" || current[1].Memory.Used != 4<<30 {
		t.Fatalf("vm2 memory = %#v source=%q, want previous-snapshot with preserved used", current[1].Memory, current[1].MemorySource)
	}
	if current[2].Memory.Used != total || current[2].MemorySource != "status-mem" {
		t.Fatalf("vm3 should remain low-trust current reading, got %#v source=%q", current[2].Memory, current[2].MemorySource)
	}
	if alertVMs[0].Memory.Used != current[0].Memory.Used || alertVMs[0].MemorySource != "previous-snapshot" {
		t.Fatalf("alert VM not stabilized: %#v source=%q", alertVMs[0].Memory, alertVMs[0].MemorySource)
	}
	if snapshots[0].Memory.Used != current[0].Memory.Used || snapshots[0].MemorySource != "previous-snapshot" {
		t.Fatalf("snapshot not stabilized: %#v source=%q", snapshots[0].Memory, snapshots[0].MemorySource)
	}
	if len(snapshots[0].Notes) != 1 || snapshots[0].Notes[0] != "preserved-previous-memory-after-repeated-low-trust-pattern" {
		t.Fatalf("snapshot notes = %#v, want stabilization note", snapshots[0].Notes)
	}
}

func TestStabilizeSuspiciousRepeatedVMMemory_IgnoresTrustedPattern(t *testing.T) {
	now := time.Now()
	total := int64(8 << 30)

	current := []models.VM{
		{ID: "vm1", Type: "qemu", Status: "running", MemorySource: "rrd-memavailable", Memory: models.Memory{Total: total, Used: 3 << 30, Usage: 37.5}, LastSeen: now},
		{ID: "vm2", Type: "qemu", Status: "running", MemorySource: "rrd-memavailable", Memory: models.Memory{Total: total, Used: 3 << 30, Usage: 37.5}, LastSeen: now},
		{ID: "vm3", Type: "qemu", Status: "running", MemorySource: "rrd-memavailable", Memory: models.Memory{Total: total, Used: 3 << 30, Usage: 37.5}, LastSeen: now},
		{ID: "vm4", Type: "qemu", Status: "running", MemorySource: "rrd-memavailable", Memory: models.Memory{Total: total, Used: 2 << 30, Usage: 25}, LastSeen: now},
	}

	applied := stabilizeSuspiciousRepeatedVMMemory(current, nil, nil, current, now)
	if applied != 0 {
		t.Fatalf("applied = %d, want 0", applied)
	}
	if current[0].MemorySource != "rrd-memavailable" {
		t.Fatalf("memory source changed unexpectedly to %q", current[0].MemorySource)
	}
}
