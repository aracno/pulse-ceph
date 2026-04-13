package alerts

import (
	"fmt"
	"testing"
	"time"

	"github.com/rcourtman/pulse-go-rewrite/internal/models"
)

func testGuestDiskCleanupConfig() AlertConfig {
	return AlertConfig{
		Enabled: true,
		GuestDefaults: ThresholdConfig{
			Disk: &HysteresisThreshold{Trigger: 90, Clear: 85},
		},
		TimeThreshold:  0,
		TimeThresholds: map[string]int{},
		Overrides:      make(map[string]ThresholdConfig),
	}
}

func TestCheckGuestClearsStalePerDiskAlertsWhenDiskSetChanges(t *testing.T) {
	m := newTestManager(t)
	m.UpdateConfig(testGuestDiskCleanupConfig())
	m.mu.Lock()
	m.config.ActivationState = ActivationActive
	m.config.TimeThreshold = 0
	m.config.TimeThresholds = map[string]int{}
	m.mu.Unlock()

	guestID := "cluster:node:101"
	staleResourceID := guestID + "-disk-old-root"
	staleAlertID := fmt.Sprintf("%s-disk", staleResourceID)

	m.mu.Lock()
	m.activeAlerts[staleAlertID] = &Alert{
		ID:           staleAlertID,
		Type:         "disk",
		ResourceID:   staleResourceID,
		ResourceName: "test-vm",
		Node:         "node",
		Instance:     "cluster",
		StartTime:    time.Now().Add(-time.Hour),
		LastSeen:     time.Now().Add(-time.Minute),
	}
	m.mu.Unlock()

	vm := models.VM{
		ID:       guestID,
		Name:     "test-vm",
		Node:     "node",
		Instance: "cluster",
		Status:   "running",
		CPU:      0.05,
		Memory: models.Memory{
			Usage: 20,
		},
		Disk: models.Disk{
			Usage: 20,
		},
		Disks: []models.Disk{
			{
				Mountpoint: "/",
				Device:     "scsi0",
				Total:      100,
				Used:       20,
				Free:       80,
				Usage:      20,
			},
		},
	}

	m.CheckGuest(vm, "cluster")

	m.mu.RLock()
	_, exists := m.activeAlerts[staleAlertID]
	m.mu.RUnlock()
	if exists {
		t.Fatalf("expected stale per-disk guest alert %q to be cleared", staleAlertID)
	}
}

func TestCheckGuestClearsPerDiskAlertsWhenGuestStopsRunning(t *testing.T) {
	m := newTestManager(t)
	m.UpdateConfig(testGuestDiskCleanupConfig())
	m.mu.Lock()
	m.config.ActivationState = ActivationActive
	m.config.TimeThreshold = 0
	m.config.TimeThresholds = map[string]int{}
	m.mu.Unlock()

	guestID := "cluster:node:102"
	staleResourceID := guestID + "-disk-root-scsi0"
	staleAlertID := fmt.Sprintf("%s-disk", staleResourceID)

	m.mu.Lock()
	m.activeAlerts[staleAlertID] = &Alert{
		ID:           staleAlertID,
		Type:         "disk",
		ResourceID:   staleResourceID,
		ResourceName: "test-vm",
		Node:         "node",
		Instance:     "cluster",
		StartTime:    time.Now().Add(-time.Hour),
		LastSeen:     time.Now().Add(-time.Minute),
	}
	m.mu.Unlock()

	vm := models.VM{
		ID:       guestID,
		Name:     "test-vm",
		Node:     "node",
		Instance: "cluster",
		Status:   "stopped",
	}

	m.CheckGuest(vm, "cluster")

	m.mu.RLock()
	_, exists := m.activeAlerts[staleAlertID]
	m.mu.RUnlock()
	if exists {
		t.Fatalf("expected per-disk guest alert %q to be cleared for stopped guest", staleAlertID)
	}
}
