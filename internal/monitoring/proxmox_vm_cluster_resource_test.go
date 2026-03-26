package monitoring

import (
	"context"
	"testing"
	"time"

	"github.com/rcourtman/pulse-go-rewrite/internal/models"
	"github.com/rcourtman/pulse-go-rewrite/pkg/proxmox"
)

type slowGuestAgentClusterClient struct {
	stubPVEClient
	resources []proxmox.ClusterResource
	fsDelay   time.Duration
}

type emptyFSInfoClusterClient struct {
	stubPVEClient
	resources []proxmox.ClusterResource
}

func (c *slowGuestAgentClusterClient) GetClusterResources(ctx context.Context, resourceType string) ([]proxmox.ClusterResource, error) {
	return c.resources, nil
}

func (c *slowGuestAgentClusterClient) GetVMStatus(ctx context.Context, node string, vmid int) (*proxmox.VMStatus, error) {
	return &proxmox.VMStatus{
		MaxMem: 8 * 1024,
		Mem:    4 * 1024,
		Agent:  proxmox.VMAgentField{Value: 1},
	}, nil
}

func (c *slowGuestAgentClusterClient) GetVMFSInfo(ctx context.Context, node string, vmid int) ([]proxmox.VMFileSystem, error) {
	select {
	case <-time.After(c.fsDelay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []proxmox.VMFileSystem{{
		Mountpoint: "/",
		Type:       "ext4",
		TotalBytes: 100 * 1024 * 1024 * 1024,
		UsedBytes:  40 * 1024 * 1024 * 1024,
		Disk:       "/dev/vda",
	}}, nil
}

func (c *emptyFSInfoClusterClient) GetClusterResources(ctx context.Context, resourceType string) ([]proxmox.ClusterResource, error) {
	return c.resources, nil
}

func (c *emptyFSInfoClusterClient) GetVMStatus(ctx context.Context, node string, vmid int) (*proxmox.VMStatus, error) {
	return &proxmox.VMStatus{
		MaxMem: 8 * 1024,
		Mem:    4 * 1024,
		Agent:  proxmox.VMAgentField{Value: 1},
	}, nil
}

func (c *emptyFSInfoClusterClient) GetVMFSInfo(ctx context.Context, node string, vmid int) ([]proxmox.VMFileSystem, error) {
	return []proxmox.VMFileSystem{}, nil
}

func TestGuestAgentFSInfoBudgetHonorsConfiguredTimeouts(t *testing.T) {
	t.Parallel()

	m := &Monitor{
		guestAgentFSInfoTimeout: 15 * time.Second,
		guestAgentRetries:       1,
	}

	budget := m.guestAgentFSInfoBudget()
	if budget < 30*time.Second {
		t.Fatalf("guestAgentFSInfoBudget() = %s, want at least 30s", budget)
	}
}

func TestPollVMsAndContainersEfficientCompletesDiskQueriesWithinPollBudget(t *testing.T) {
	t.Setenv("PULSE_DATA_DIR", t.TempDir())

	client := &slowGuestAgentClusterClient{
		fsDelay: 60 * time.Millisecond,
		resources: []proxmox.ClusterResource{
			{Type: "qemu", Node: "node1", VMID: 100, Name: "vm100", Status: "running", MaxMem: 8 * 1024, Mem: 4 * 1024, MaxDisk: 100 * 1024 * 1024 * 1024, MaxCPU: 4},
			{Type: "qemu", Node: "node1", VMID: 101, Name: "vm101", Status: "running", MaxMem: 8 * 1024, Mem: 4 * 1024, MaxDisk: 100 * 1024 * 1024 * 1024, MaxCPU: 4},
			{Type: "qemu", Node: "node1", VMID: 102, Name: "vm102", Status: "running", MaxMem: 8 * 1024, Mem: 4 * 1024, MaxDisk: 100 * 1024 * 1024 * 1024, MaxCPU: 4},
			{Type: "qemu", Node: "node1", VMID: 103, Name: "vm103", Status: "running", MaxMem: 8 * 1024, Mem: 4 * 1024, MaxDisk: 100 * 1024 * 1024 * 1024, MaxCPU: 4},
		},
	}

	mon := newTestPVEMonitor("pve1")
	defer mon.alertManager.Stop()
	defer mon.notificationMgr.Stop()

	mon.rateTracker = NewRateTracker()
	mon.guestMetadataCache = make(map[string]guestMetadataCacheEntry)
	mon.guestMetadataLimiter = make(map[string]time.Time)
	mon.vmRRDMemCache = make(map[string]rrdMemCacheEntry)
	mon.vmAgentMemCache = make(map[string]agentMemCacheEntry)
	mon.guestAgentFSInfoTimeout = 250 * time.Millisecond
	mon.guestAgentNetworkTimeout = 250 * time.Millisecond
	mon.guestAgentOSInfoTimeout = 250 * time.Millisecond
	mon.guestAgentVersionTimeout = 250 * time.Millisecond
	mon.guestAgentRetries = 0
	mon.guestAgentWorkSlots = make(chan struct{}, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()

	if ok := mon.pollVMsAndContainersEfficient(ctx, "pve1", "", false, client, map[string]string{"node1": "online"}); !ok {
		t.Fatal("pollVMsAndContainersEfficient() returned false")
	}

	state := mon.state.GetSnapshot()
	if len(state.VMs) != 4 {
		t.Fatalf("expected 4 VMs, got %d", len(state.VMs))
	}
	for _, vm := range state.VMs {
		if vm.Disk.Total <= 0 || vm.Disk.Usage <= 0 {
			t.Fatalf("expected guest-agent disk data for %s, got total=%d usage=%.2f", vm.Name, vm.Disk.Total, vm.Disk.Usage)
		}
	}
}

func TestPollVMsAndContainersEfficientCarriesForwardPreviousIndividualDisks(t *testing.T) {
	t.Setenv("PULSE_DATA_DIR", t.TempDir())

	client := &emptyFSInfoClusterClient{
		resources: []proxmox.ClusterResource{
			{Type: "qemu", Node: "node1", VMID: 100, Name: "vm100", Status: "running", MaxMem: 8 * 1024, Mem: 4 * 1024, MaxDisk: 100 * 1024 * 1024 * 1024, MaxCPU: 4},
		},
	}

	mon := newTestPVEMonitor("pve1")
	defer mon.alertManager.Stop()
	defer mon.notificationMgr.Stop()

	mon.rateTracker = NewRateTracker()
	mon.guestMetadataCache = make(map[string]guestMetadataCacheEntry)
	mon.guestMetadataLimiter = make(map[string]time.Time)
	mon.vmRRDMemCache = make(map[string]rrdMemCacheEntry)
	mon.vmAgentMemCache = make(map[string]agentMemCacheEntry)
	mon.guestAgentWorkSlots = make(chan struct{}, 2)

	prevVM := models.VM{
		ID:       makeGuestID("pve1", "node1", 100),
		VMID:     100,
		Name:     "vm100",
		Node:     "node1",
		Instance: "pve1",
		Type:     "qemu",
		Status:   "running",
		Disk: models.Disk{
			Total: 100 * 1024 * 1024 * 1024,
			Used:  40 * 1024 * 1024 * 1024,
			Free:  60 * 1024 * 1024 * 1024,
			Usage: 40,
		},
		Disks: []models.Disk{
			{
				Total:      100 * 1024 * 1024 * 1024,
				Used:       40 * 1024 * 1024 * 1024,
				Free:       60 * 1024 * 1024 * 1024,
				Usage:      40,
				Mountpoint: "/",
				Type:       "ext4",
				Device:     "/dev/vda",
			},
		},
	}
	mon.state.UpdateVMs([]models.VM{prevVM})

	if ok := mon.pollVMsAndContainersEfficient(context.Background(), "pve1", "", false, client, map[string]string{"node1": "online"}); !ok {
		t.Fatal("pollVMsAndContainersEfficient() returned false")
	}

	state := mon.state.GetSnapshot()
	if len(state.VMs) != 1 {
		t.Fatalf("expected 1 VM, got %d", len(state.VMs))
	}

	vm := state.VMs[0]
	if len(vm.Disks) != 1 {
		t.Fatalf("expected previous individual disks to be preserved, got %#v", vm.Disks)
	}
	if vm.Disks[0].Mountpoint != "/" || vm.Disks[0].Device != "/dev/vda" {
		t.Fatalf("unexpected carried-forward disk data: %#v", vm.Disks[0])
	}
	if vm.Disk.Usage != 40 {
		t.Fatalf("expected aggregate disk usage to be carried forward, got %.2f", vm.Disk.Usage)
	}
}
