package alerts

import (
	"testing"
	"time"

	"github.com/rcourtman/pulse-go-rewrite/internal/models"
)

func findAlertByType(active []Alert, alertType string) *Alert {
	for i := range active {
		if active[i].Type == alertType {
			return &active[i]
		}
	}
	return nil
}

func testCephCluster() models.CephCluster {
	return models.CephCluster{
		ID:           "homelab-fsid",
		Instance:     "homelab",
		Name:         "Ceph",
		FSID:         "fsid",
		Health:       "HEALTH_OK",
		NumOSDs:      3,
		NumOSDsUp:    3,
		NumOSDsIn:    3,
		UsagePercent: 42,
		LastUpdated:  time.Now(),
	}
}

func TestCheckCephClusterHealthAlert(t *testing.T) {
	m := NewManager()
	cluster := testCephCluster()
	cluster.Health = "HEALTH_WARN"
	cluster.HealthMessage = "1 pool has degraded objects"

	m.CheckCephCluster(cluster)

	active := m.GetActiveAlerts()
	alert := findAlertByType(active, "ceph-health")
	if alert == nil {
		t.Fatalf("expected ceph-health alert, got %#v", active)
	}
	if alert.Level != AlertLevelWarning {
		t.Fatalf("level = %s, want warning", alert.Level)
	}
	if alert.ResourceID != cluster.ID {
		t.Fatalf("resource id = %q, want %q", alert.ResourceID, cluster.ID)
	}

	cluster.Health = "HEALTH_OK"
	cluster.HealthMessage = ""
	m.CheckCephCluster(cluster)

	if active := m.GetActiveAlerts(); len(active) != 0 {
		t.Fatalf("expected Ceph health alert to resolve, got %#v", active)
	}
}

func TestCheckCephClusterOSDAlert(t *testing.T) {
	m := NewManager()
	cluster := testCephCluster()
	cluster.NumOSDsUp = 2
	cluster.NumOSDsIn = 2

	m.CheckCephCluster(cluster)

	active := m.GetActiveAlerts()
	alert := findAlertByType(active, "ceph-osd-state")
	if alert == nil {
		t.Fatalf("expected ceph-osd-state alert, got %#v", active)
	}
	if alert.Level != AlertLevelCritical {
		t.Fatalf("level = %s, want critical for down OSD", alert.Level)
	}
	if got := alert.Metadata["osdsDown"]; got != 1 {
		t.Fatalf("osdsDown metadata = %#v, want 1", got)
	}

	cluster.NumOSDsUp = 3
	cluster.NumOSDsIn = 3
	m.CheckCephCluster(cluster)

	if active := m.GetActiveAlerts(); len(active) != 0 {
		t.Fatalf("expected OSD alert to resolve, got %#v", active)
	}
}

func TestCheckCephClusterDisabledClearsAlerts(t *testing.T) {
	m := NewManager()
	cluster := testCephCluster()
	cluster.Health = "HEALTH_ERR"
	cluster.NumOSDsUp = 2

	m.CheckCephCluster(cluster)
	if active := m.GetActiveAlerts(); len(active) != 2 {
		t.Fatalf("expected two Ceph alerts, got %#v", active)
	}

	cfg := m.GetConfig()
	cfg.DisableAllCeph = true
	m.UpdateConfig(cfg)
	m.CheckCephCluster(cluster)

	if active := m.GetActiveAlerts(); len(active) != 0 {
		t.Fatalf("expected disabled Ceph alerts to clear, got %#v", active)
	}
}
