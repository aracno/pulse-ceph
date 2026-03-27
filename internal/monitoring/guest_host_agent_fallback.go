package monitoring

import (
	"strings"

	"github.com/rcourtman/pulse-go-rewrite/internal/models"
)

func buildLinkedVMHostAgentMap(hosts []models.Host) map[string]models.Host {
	if len(hosts) == 0 {
		return nil
	}

	linked := make(map[string]models.Host)
	for _, host := range hosts {
		if strings.TrimSpace(host.LinkedVMID) == "" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(host.Status), "online") {
			continue
		}
		linked[host.LinkedVMID] = host
	}
	return linked
}

func resolveGuestDiskFromLinkedHostAgent(guestID string, vmIDToHostAgent map[string]models.Host) (models.Disk, []models.Disk, bool) {
	if guestID == "" || len(vmIDToHostAgent) == 0 {
		return models.Disk{}, nil, false
	}

	host, ok := vmIDToHostAgent[guestID]
	if !ok {
		return models.Disk{}, nil, false
	}

	summary, ok := models.SummaryDisk(host.Disks)
	if !ok {
		return models.Disk{}, nil, false
	}

	return models.Disk{
		Total:      summary.Total,
		Used:       summary.Used,
		Free:       summary.Free,
		Usage:      summary.Usage,
		Mountpoint: summary.Mountpoint,
		Type:       summary.Type,
		Device:     summary.Device,
	}, cloneGuestDisks(host.Disks), true
}
