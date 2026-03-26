package monitoring

import (
	"context"
	"testing"
	"time"

	"github.com/rcourtman/pulse-go-rewrite/internal/alerts"
	"github.com/rcourtman/pulse-go-rewrite/internal/config"
	"github.com/rcourtman/pulse-go-rewrite/internal/models"
	"github.com/rcourtman/pulse-go-rewrite/pkg/proxmox"
)

type fakeDockerChecker struct{}

func (f *fakeDockerChecker) CheckDockerInContainer(ctx context.Context, node string, vmid int) (bool, error) {
	return false, nil
}

func TestMonitorGetConfig(t *testing.T) {
	cfg := &config.Config{DataPath: "/tmp/pulse-test"}
	monitor := &Monitor{config: cfg}

	if got := monitor.GetConfig(); got != cfg {
		t.Fatalf("GetConfig = %v, want %v", got, cfg)
	}
}

func TestMonitorSetGetDockerChecker(t *testing.T) {
	monitor := &Monitor{}
	checker := &fakeDockerChecker{}

	monitor.SetDockerChecker(checker)
	if got := monitor.GetDockerChecker(); got != checker {
		t.Fatalf("GetDockerChecker = %v, want %v", got, checker)
	}

	monitor.SetDockerChecker(nil)
	if got := monitor.GetDockerChecker(); got != nil {
		t.Fatalf("GetDockerChecker = %v, want nil", got)
	}
}

func TestMonitorGetDockerHosts(t *testing.T) {
	monitor := &Monitor{state: models.NewState()}
	monitor.state.UpsertDockerHost(models.DockerHost{ID: "host-1", Hostname: "host-1"})

	hosts := monitor.GetDockerHosts()
	if len(hosts) != 1 {
		t.Fatalf("GetDockerHosts length = %d, want 1", len(hosts))
	}
	if hosts[0].ID != "host-1" {
		t.Fatalf("GetDockerHosts[0].ID = %q, want %q", hosts[0].ID, "host-1")
	}
}

func TestMonitorGetDockerHostsNilReceiver(t *testing.T) {
	var monitor *Monitor
	if got := monitor.GetDockerHosts(); got != nil {
		t.Fatalf("GetDockerHosts = %v, want nil", got)
	}
}

func TestMonitorLinkHostAgent(t *testing.T) {
	monitor := &Monitor{state: models.NewState()}

	if err := monitor.LinkHostAgent("", "node-1"); err == nil {
		t.Fatalf("expected error on empty host ID")
	}
	if err := monitor.LinkHostAgent("host-1", ""); err == nil {
		t.Fatalf("expected error on empty node ID")
	}

	monitor.state.UpsertHost(models.Host{ID: "host-1", Hostname: "host-1"})
	monitor.state.UpdateNodes([]models.Node{{ID: "node-1", Name: "node-1"}})

	if err := monitor.LinkHostAgent("host-1", "node-1"); err != nil {
		t.Fatalf("LinkHostAgent error: %v", err)
	}

	hosts := monitor.state.GetHosts()
	if len(hosts) != 1 || hosts[0].LinkedNodeID != "node-1" {
		t.Fatalf("LinkedNodeID = %q, want %q", hosts[0].LinkedNodeID, "node-1")
	}
	if len(monitor.state.Nodes) != 1 || monitor.state.Nodes[0].LinkedHostAgentID != "host-1" {
		t.Fatalf("LinkedHostAgentID = %q, want %q", monitor.state.Nodes[0].LinkedHostAgentID, "host-1")
	}
}

func TestMonitorInvalidateAgentProfileCache(t *testing.T) {
	monitor := &Monitor{
		agentProfileCache: &agentProfileCacheEntry{
			profiles: []models.AgentProfile{{ID: "profile-1"}},
			loadedAt: time.Now(),
		},
	}

	monitor.InvalidateAgentProfileCache()
	if monitor.agentProfileCache != nil {
		t.Fatalf("expected cache to be cleared")
	}
}

func TestMonitorMarkDockerHostPendingUninstall(t *testing.T) {
	monitor := &Monitor{state: models.NewState()}

	if _, err := monitor.MarkDockerHostPendingUninstall(""); err == nil {
		t.Fatalf("expected error on empty host ID")
	}
	if _, err := monitor.MarkDockerHostPendingUninstall("missing"); err == nil {
		t.Fatalf("expected error on missing host")
	}

	monitor.state.UpsertDockerHost(models.DockerHost{ID: "host-1", Hostname: "host-1"})
	host, err := monitor.MarkDockerHostPendingUninstall("host-1")
	if err != nil {
		t.Fatalf("MarkDockerHostPendingUninstall error: %v", err)
	}
	if !host.PendingUninstall {
		t.Fatalf("expected PendingUninstall to be true")
	}

	hosts := monitor.state.GetDockerHosts()
	if len(hosts) != 1 || !hosts[0].PendingUninstall {
		t.Fatalf("state PendingUninstall = %v, want true", hosts[0].PendingUninstall)
	}
}

func TestEnsureClusterEndpointURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"https://node.example:8006", "https://node.example:8006"},
		{"node.example", "https://node.example:8006"},
		{"node.example:9006", "https://node.example:9006"},
		{"  node.example  ", "https://node.example:8006"},
	}

	for _, tt := range tests {
		if got := ensureClusterEndpointURL(tt.input); got != tt.expected {
			t.Fatalf("ensureClusterEndpointURL(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestClusterEndpointEffectiveURL(t *testing.T) {
	endpoint := config.ClusterEndpoint{
		Host: "node.local",
		IP:   "10.0.0.1",
	}

	if got := clusterEndpointEffectiveURL(endpoint, true, ""); got != "https://node.local:8006" {
		t.Fatalf("verifySSL host preference = %q, want %q", got, "https://node.local:8006")
	}

	endpoint.Host = ""
	if got := clusterEndpointEffectiveURL(endpoint, true, ""); got != "https://10.0.0.1:8006" {
		t.Fatalf("verifySSL fallback to IP = %q, want %q", got, "https://10.0.0.1:8006")
	}

	endpoint.Host = "node.local"
	if got := clusterEndpointEffectiveURL(endpoint, false, ""); got != "https://10.0.0.1:8006" {
		t.Fatalf("non-SSL IP preference = %q, want %q", got, "https://10.0.0.1:8006")
	}

	endpoint.IPOverride = "192.168.1.10"
	if got := clusterEndpointEffectiveURL(endpoint, false, ""); got != "https://192.168.1.10:8006" {
		t.Fatalf("override IP preference = %q, want %q", got, "https://192.168.1.10:8006")
	}

	endpoint.Fingerprint = "ep-fingerprint"
	if got := clusterEndpointEffectiveURL(endpoint, true, ""); got != "https://192.168.1.10:8006" {
		t.Fatalf("per-endpoint fingerprint should allow IP override, got %q", got)
	}

	endpoint.Fingerprint = ""
	if got := clusterEndpointEffectiveURL(endpoint, true, "cluster-base-fingerprint"); got != "https://node.local:8006" {
		t.Fatalf("base fingerprint must not force IP routing for other cluster nodes, got %q", got)
	}

	endpoint = config.ClusterEndpoint{}
	if got := clusterEndpointEffectiveURL(endpoint, true, ""); got != "" {
		t.Fatalf("empty endpoint = %q, want empty", got)
	}
}

func TestBuildClusterClientEndpoints_PrefersOverrideWhenEndpointFingerprintPresent(t *testing.T) {
	pve := config.PVEInstance{
		Name:        "cluster-a",
		Host:        "https://cluster-a.local:8006",
		VerifySSL:   true,
		IsCluster:   true,
		ClusterName: "cluster-a",
		ClusterEndpoints: []config.ClusterEndpoint{
			{
				NodeName:    "node1",
				Host:        "https://node1.local:8006",
				IP:          "10.15.5.11",
				IPOverride:  "10.15.2.11",
				Fingerprint: "node1-fp",
			},
		},
	}

	endpoints, fingerprints := buildClusterClientEndpoints(pve)

	if len(endpoints) != 2 {
		t.Fatalf("expected endpoint plus main host fallback, got %d", len(endpoints))
	}
	if endpoints[0] != "https://10.15.2.11:8006" {
		t.Fatalf("expected endpoint override URL first, got %q", endpoints[0])
	}
	if fingerprints["https://10.15.2.11:8006"] != "node1-fp" {
		t.Fatalf("expected fingerprint to follow effective endpoint URL, got %q", fingerprints["https://10.15.2.11:8006"])
	}
}

func TestProxmoxDiskMatchesExclude(t *testing.T) {
	tests := []struct {
		name     string
		disk     proxmox.Disk
		patterns []string
		want     bool
	}{
		{
			name:     "matches devpath directly",
			disk:     proxmox.Disk{DevPath: "/dev/sda"},
			patterns: []string{"/dev/sda"},
			want:     true,
		},
		{
			name:     "matches basename from devpath",
			disk:     proxmox.Disk{DevPath: "/dev/sda"},
			patterns: []string{"sda"},
			want:     true,
		},
		{
			name:     "does not match different device",
			disk:     proxmox.Disk{DevPath: "/dev/sdb"},
			patterns: []string{"sda"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := proxmoxDiskMatchesExclude(tt.disk, tt.patterns); got != tt.want {
				t.Fatalf("proxmoxDiskMatchesExclude(%+v, %v) = %t, want %t", tt.disk, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestBuildPhysicalDisksForNodeOmitsExcludedDisksAndClearsAlerts(t *testing.T) {
	monitor := &Monitor{alertManager: alerts.NewManager()}
	t.Cleanup(func() {
		monitor.alertManager.Stop()
	})

	failingDisk := proxmox.Disk{
		DevPath: "/dev/sda",
		Model:   "Samsung SSD",
		Health:  "FAILED",
		Wearout: 1,
	}
	healthyDisk := proxmox.Disk{
		DevPath: "/dev/sdb",
		Model:   "WD Blue",
		Health:  "PASSED",
		Wearout: 100,
	}

	monitor.alertManager.CheckDiskHealth("inst", "node1", failingDisk)
	if got := len(monitor.alertManager.GetActiveAlerts()); got == 0 {
		t.Fatalf("expected a failing disk alert before exclusion handling")
	}

	disks := monitor.buildPhysicalDisksForNode(
		"inst",
		"node1",
		[]proxmox.Disk{failingDisk, healthyDisk},
		[]string{"sda"},
		time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC),
	)

	if len(disks) != 1 {
		t.Fatalf("expected 1 physical disk after exclusion, got %d", len(disks))
	}
	if disks[0].DevPath != "/dev/sdb" {
		t.Fatalf("expected only /dev/sdb to remain, got %q", disks[0].DevPath)
	}

	for _, alert := range monitor.alertManager.GetActiveAlerts() {
		if alert.ID == "disk-health-inst-node1-/dev/sda" || alert.ID == "disk-wearout-inst-node1-/dev/sda" {
			t.Fatalf("expected excluded disk alerts to be cleared, still found %+v", alert)
		}
	}
}

func TestBuildClusterClientEndpoints_FallsBackToMainHostWhenOnlyBaseFingerprintExists(t *testing.T) {
	pve := config.PVEInstance{
		Name:        "cluster-a",
		Host:        "https://cluster-a.example.com:8006",
		Fingerprint: "cluster-base-fp",
		VerifySSL:   true,
		IsCluster:   true,
		ClusterName: "cluster-a",
		ClusterEndpoints: []config.ClusterEndpoint{
			{
				NodeName: "node1",
				Host:     "node1",
				IP:       "10.15.5.11",
			},
			{
				NodeName: "node2",
				Host:     "node2",
				IP:       "10.15.5.12",
			},
		},
	}

	endpoints, fingerprints := buildClusterClientEndpoints(pve)

	if len(endpoints) != 1 {
		t.Fatalf("expected only the main host fallback endpoint, got %d", len(endpoints))
	}
	if endpoints[0] != "https://cluster-a.example.com:8006" {
		t.Fatalf("expected main host fallback, got %q", endpoints[0])
	}
	if len(fingerprints) != 0 {
		t.Fatalf("expected no per-endpoint fingerprints, got %v", fingerprints)
	}
}
