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
	cluster.InconsistentPGs = 2

	m.CheckCephCluster(cluster)
	if active := m.GetActiveAlerts(); len(active) != 3 {
		t.Fatalf("expected three Ceph alerts, got %#v", active)
	}

	cfg := m.GetConfig()
	cfg.DisableAllCeph = true
	m.UpdateConfig(cfg)
	m.CheckCephCluster(cluster)

	if active := m.GetActiveAlerts(); len(active) != 0 {
		t.Fatalf("expected disabled Ceph alerts to clear, got %#v", active)
	}
}

func TestCheckCephClusterPGInconsistentAlert(t *testing.T) {
	m := NewManager()
	cluster := testCephCluster()
	cluster.InconsistentPGs = 4

	m.CheckCephCluster(cluster)

	active := m.GetActiveAlerts()
	alert := findAlertByType(active, "ceph-pg-inconsistent")
	if alert == nil {
		t.Fatalf("expected ceph-pg-inconsistent alert, got %#v", active)
	}
	if alert.Level != AlertLevelCritical {
		t.Fatalf("level = %s, want critical", alert.Level)
	}
	if alert.Value != 4 {
		t.Fatalf("value = %v, want 4", alert.Value)
	}

	cluster.InconsistentPGs = 0
	m.CheckCephCluster(cluster)

	if active := m.GetActiveAlerts(); len(active) != 0 {
		t.Fatalf("expected inconsistent PG alert to resolve, got %#v", active)
	}
}

func TestCheckCephClusterIndividualOSDAlertAndDisable(t *testing.T) {
	m := NewManager()
	cluster := testCephCluster()
	cluster.OSDs = []models.CephOSD{
		{ID: 0, Name: "osd.0", Host: "pve1", Up: true, In: true},
		{ID: 1, Name: "osd.1", Host: "pve2", Up: false, In: true},
		{ID: 2, Name: "osd.2", Host: "pve3", Up: true, In: false},
	}

	m.CheckCephCluster(cluster)

	active := m.GetActiveAlerts()
	if len(active) != 2 {
		t.Fatalf("expected two individual OSD alerts, got %#v", active)
	}
	if alert := findAlertByType(active, "ceph-osd-state"); alert == nil {
		t.Fatalf("expected ceph-osd-state alert, got %#v", active)
	}

	cfg := m.GetConfig()
	cfg.Overrides[cephOSDResourceID(cluster.ID, 1)] = ThresholdConfig{Disabled: true}
	m.UpdateConfig(cfg)
	if updated := m.GetConfig(); !updated.Overrides[cephOSDResourceID(cluster.ID, 1)].Disabled {
		t.Fatalf("expected disabled override for osd.1 to be preserved, overrides=%#v", updated.Overrides)
	}
	m.CheckCephCluster(cluster)

	active = m.GetActiveAlerts()
	if len(active) != 1 {
		t.Fatalf("expected one OSD alert after disabling osd.1, got %#v", active)
	}
	if active[0].ResourceID != cephOSDResourceID(cluster.ID, 2) {
		t.Fatalf("remaining alert resource = %q, want osd.2", active[0].ResourceID)
	}
}

func TestCheckCephClusterCategoryDisables(t *testing.T) {
	m := NewManager()
	cluster := testCephCluster()
	cluster.Health = "HEALTH_ERR"
	cluster.OSDs = []models.CephOSD{{ID: 1, Name: "osd.1", Up: false, In: true}}
	cluster.InconsistentPGs = 1

	cfg := m.GetConfig()
	cfg.Overrides[cluster.ID] = ThresholdConfig{
		CephDisableHealth: true,
		CephDisableOSD:    true,
		CephDisablePG:     true,
	}
	m.UpdateConfig(cfg)
	m.CheckCephCluster(cluster)

	if active := m.GetActiveAlerts(); len(active) != 0 {
		t.Fatalf("expected category-disabled Ceph alerts to be suppressed, got %#v", active)
	}
}
