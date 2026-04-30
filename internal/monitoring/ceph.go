package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rcourtman/pulse-go-rewrite/internal/models"
	"github.com/rcourtman/pulse-go-rewrite/pkg/proxmox"
	"github.com/rs/zerolog/log"
)

// isCephStorageType returns true when the provided storage type represents a Ceph backend.
func isCephStorageType(storageType string) bool {
	switch strings.ToLower(strings.TrimSpace(storageType)) {
	case "rbd", "cephfs", "ceph":
		return true
	default:
		return false
	}
}

func cephPollContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= 15*time.Second {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, 15*time.Second)
}

func fetchCephClusterData(ctx context.Context, instanceName string, client PVEClientInterface) (*proxmox.CephStatus, *proxmox.CephDF, error) {
	cephCtx, cancel := cephPollContext(ctx)
	defer cancel()

	status, err := client.GetCephStatus(cephCtx)
	if err != nil {
		log.Debug().Err(err).Str("instance", instanceName).Msg("Ceph status unavailable - preserving previous Ceph state")
		return nil, nil, err
	}
	if status == nil {
		return nil, nil, nil
	}

	df, err := client.GetCephDF(cephCtx)
	if err != nil {
		log.Debug().Err(err).Str("instance", instanceName).Msg("Ceph DF unavailable - continuing with status-only data")
	}

	return status, df, nil
}

func normalizeCephPoolKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func cephPoolLookupCandidates(storage models.Storage) []string {
	candidates := make([]string, 0, 2)
	if pool := normalizeCephPoolKey(storage.Pool); pool != "" {
		candidates = append(candidates, pool)
	}
	if name := normalizeCephPoolKey(storage.Name); name != "" {
		candidates = append(candidates, name)
	}
	return slices.Compact(candidates)
}

func hydrateCephStorageUsageFromDF(storage []models.Storage, df *proxmox.CephDF) bool {
	if len(storage) == 0 || df == nil || len(df.Data.Pools) == 0 {
		return false
	}

	poolsByName := make(map[string]proxmox.CephDFPool, len(df.Data.Pools))
	for _, pool := range df.Data.Pools {
		key := normalizeCephPoolKey(pool.Name)
		if key == "" {
			continue
		}
		poolsByName[key] = pool
	}

	updated := false
	for idx := range storage {
		if !isCephStorageType(storage[idx].Type) {
			continue
		}

		var pool proxmox.CephDFPool
		found := false
		for _, candidate := range cephPoolLookupCandidates(storage[idx]) {
			match, ok := poolsByName[candidate]
			if !ok {
				continue
			}
			pool = match
			found = true
			break
		}
		if !found {
			continue
		}

		used := int64(pool.Stats.BytesUsed)
		free := int64(pool.Stats.MaxAvail)
		total := used + free
		if total <= 0 {
			continue
		}

		storage[idx].Used = used
		storage[idx].Free = free
		storage[idx].Total = total
		storage[idx].Usage = safePercentage(float64(used), float64(total))
		updated = true
	}

	return updated
}

// pollCephCluster gathers Ceph cluster information when Ceph-backed storage is detected.
func (m *Monitor) pollCephCluster(ctx context.Context, instanceName string, client PVEClientInterface, cephDetected bool) {
	if !cephDetected {
		// Clear any previously cached Ceph data for this instance.
		m.state.UpdateCephClustersForInstance(instanceName, []models.CephCluster{})
		return
	}

	status, df, err := fetchCephClusterData(ctx, instanceName, client)
	if err != nil {
		return
	}
	if status == nil {
		log.Debug().Str("instance", instanceName).Msg("Ceph status response empty - clearing cached Ceph state")
		m.state.UpdateCephClustersForInstance(instanceName, []models.CephCluster{})
		return
	}

	cluster := buildCephClusterModel(instanceName, status, df)
	if cluster.ID == "" {
		// Ensure the cluster has a stable identifier; fall back to instance name.
		cluster.ID = instanceName
	}

	m.state.UpdateCephClustersForInstance(instanceName, []models.CephCluster{cluster})
}

// buildCephClusterModel converts the proxmox Ceph responses into the shared model representation.
func buildCephClusterModel(instanceName string, status *proxmox.CephStatus, df *proxmox.CephDF) models.CephCluster {
	clusterID := instanceName
	if status.FSID != "" {
		clusterID = fmt.Sprintf("%s-%s", instanceName, status.FSID)
	}

	totalBytes := int64(status.PGMap.BytesTotal)
	usedBytes := int64(status.PGMap.BytesUsed)
	availBytes := int64(status.PGMap.BytesAvail)

	if df != nil {
		if stats := df.Data.Stats; stats.TotalBytes > 0 {
			totalBytes = int64(stats.TotalBytes)
			usedBytes = int64(stats.TotalUsedBytes)
			availBytes = int64(stats.TotalAvailBytes)
		}
	}

	usagePercent := safePercentage(float64(usedBytes), float64(totalBytes))

	pools := make([]models.CephPool, 0)
	if df != nil {
		for _, pool := range df.Data.Pools {
			pools = append(pools, models.CephPool{
				ID:             pool.ID,
				Name:           pool.Name,
				StoredBytes:    int64(pool.Stats.BytesUsed),
				AvailableBytes: int64(pool.Stats.MaxAvail),
				Objects:        int64(pool.Stats.Objects),
				PercentUsed:    pool.Stats.PercentUsed,
			})
		}
	}

	services := make([]models.CephServiceStatus, 0)
	if status.ServiceMap.Services != nil {
		for serviceType, definition := range status.ServiceMap.Services {
			running := 0
			total := 0
			var offline []string
			for daemonName, daemon := range definition.Daemons {
				total++
				if strings.EqualFold(daemon.Status, "running") || strings.EqualFold(daemon.Status, "active") {
					running++
					continue
				}
				label := daemonName
				if daemon.Host != "" {
					label = fmt.Sprintf("%s@%s", daemonName, daemon.Host)
				}
				offline = append(offline, label)
			}
			serviceStatus := models.CephServiceStatus{
				Type:    serviceType,
				Running: running,
				Total:   total,
			}
			if len(offline) > 0 {
				serviceStatus.Message = fmt.Sprintf("Offline: %s", strings.Join(offline, ", "))
			}
			services = append(services, serviceStatus)
		}
	}

	healthMsg := summarizeCephHealth(status)
	numMons := countCephMonitorDaemons(status)
	numMgrs := countCephManagerDaemons(status)
	osds := buildCephOSDModels(status)
	inconsistentPGs := countInconsistentPGs(status)

	cluster := models.CephCluster{
		ID:              clusterID,
		Instance:        instanceName,
		Name:            "Ceph",
		FSID:            status.FSID,
		Health:          status.Health.Status,
		HealthMessage:   healthMsg,
		TotalBytes:      totalBytes,
		UsedBytes:       usedBytes,
		AvailableBytes:  availBytes,
		UsagePercent:    usagePercent,
		NumMons:         numMons,
		NumMgrs:         numMgrs,
		NumOSDs:         status.OSDMap.NumOSDs,
		NumOSDsUp:       status.OSDMap.NumUpOSDs,
		NumOSDsIn:       status.OSDMap.NumInOSDs,
		NumPGs:          status.PGMap.NumPGs,
		InconsistentPGs: inconsistentPGs,
		OSDs:            osds,
		Pools:           pools,
		Services:        services,
		LastUpdated:     time.Now(),
	}

	return cluster
}

func buildCephOSDModels(status *proxmox.CephStatus) []models.CephOSD {
	if status == nil || len(status.OSDMap.OSDs) == 0 {
		return nil
	}

	osds := make([]models.CephOSD, 0, len(status.OSDMap.OSDs))
	for _, osd := range status.OSDMap.OSDs {
		name := strings.TrimSpace(osd.Name)
		if name == "" {
			name = fmt.Sprintf("osd.%d", osd.ID)
		}
		osds = append(osds, models.CephOSD{
			ID:     osd.ID,
			Name:   name,
			Host:   osd.Host,
			Up:     osd.Up > 0,
			In:     osd.In > 0,
			State:  append([]string(nil), osd.State...),
			Weight: osd.Weight,
		})
	}

	return osds
}

type cephNodeOSDClient interface {
	GetNodeCephOSDs(ctx context.Context, node string) ([]proxmox.CephOSDStatus, error)
}

type cephNodeOSDMetadataClient interface {
	GetNodeCephOSDMetadata(ctx context.Context, node string, osdID int) (*proxmox.CephOSDMetadata, error)
}

func enrichCephClusterOSDsFromNodes(ctx context.Context, client PVEClientInterface, nodes []proxmox.Node, cluster *models.CephCluster) {
	osdClient, ok := client.(cephNodeOSDClient)
	if !ok || cluster == nil || len(nodes) == 0 {
		return
	}
	metadataClient, _ := client.(cephNodeOSDMetadataClient)

	byID := make(map[int]models.CephOSD)
	for _, osd := range cluster.OSDs {
		byID[osd.ID] = osd
	}
	ensureCephOSDPlaceholders(cluster, byID)

	for _, node := range nodes {
		if strings.TrimSpace(node.Node) == "" {
			continue
		}
		nodeOSDs, err := osdClient.GetNodeCephOSDs(ctx, node.Node)
		if err != nil {
			log.Debug().Err(err).Str("node", node.Node).Msg("Failed to fetch Ceph OSDs for node")
			continue
		}
		localScope := cluster.NumOSDs == 0 || len(nodeOSDs) < cluster.NumOSDs || len(nodes) == 1
		for _, osd := range nodeOSDs {
			name := strings.TrimSpace(osd.Name)
			if name == "" {
				name = fmt.Sprintf("osd.%d", osd.ID)
			}
			host := strings.TrimSpace(osd.Host)
			if host == "" && localScope {
				host = node.Node
			}
			candidate := models.CephOSD{
				ID:     osd.ID,
				Name:   name,
				Host:   host,
				Up:     osd.Up > 0,
				In:     osd.In > 0,
				State:  append([]string(nil), osd.State...),
				Weight: osd.Weight,
			}
			existing, exists := byID[osd.ID]
			if !exists || existing.Host == "" || existing.Synthetic {
				byID[osd.ID] = candidate
			}
		}
	}

	if metadataClient != nil {
		enrichCephClusterOSDsFromMetadata(ctx, metadataClient, nodes, byID)
	}

	if len(byID) == 0 {
		return
	}
	enriched := make([]models.CephOSD, 0, len(byID))
	for _, osd := range byID {
		enriched = append(enriched, osd)
	}
	sort.Slice(enriched, func(i, j int) bool {
		return enriched[i].ID < enriched[j].ID
	})
	cluster.OSDs = enriched
}

func ensureCephOSDPlaceholders(cluster *models.CephCluster, osds map[int]models.CephOSD) {
	if cluster == nil || cluster.NumOSDs <= 0 {
		return
	}
	allUp := cluster.NumOSDsUp == cluster.NumOSDs
	allIn := cluster.NumOSDsIn == cluster.NumOSDs
	for id := 0; id < cluster.NumOSDs; id++ {
		if _, exists := osds[id]; exists {
			continue
		}
		osds[id] = models.CephOSD{
			ID:        id,
			Name:      fmt.Sprintf("osd.%d", id),
			Up:        allUp,
			In:        allIn,
			Synthetic: true,
		}
	}
}

func enrichCephClusterOSDsFromMetadata(ctx context.Context, client cephNodeOSDMetadataClient, nodes []proxmox.Node, osds map[int]models.CephOSD) {
	for id, osd := range osds {
		if strings.TrimSpace(osd.Host) != "" && !osd.Synthetic {
			continue
		}
		for _, node := range nodes {
			nodeName := strings.TrimSpace(node.Node)
			if nodeName == "" {
				continue
			}
			metadata, err := client.GetNodeCephOSDMetadata(ctx, nodeName, id)
			if err != nil {
				log.Debug().Err(err).Str("node", nodeName).Int("osd", id).Msg("Failed to fetch Ceph OSD metadata for node")
				continue
			}
			host := strings.TrimSpace(metadata.OSD.Hostname)
			if host == "" {
				host = nodeName
			}
			osd.Host = host
			osd.Synthetic = false
			if osd.Name == "" {
				osd.Name = fmt.Sprintf("osd.%d", id)
			}
			osds[id] = osd
			break
		}
	}
}

func countInconsistentPGs(status *proxmox.CephStatus) int {
	if status == nil {
		return 0
	}

	total := 0
	for _, state := range status.PGMap.PGsByState {
		if strings.Contains(strings.ToLower(state.StateName), "inconsistent") && state.Count > 0 {
			total += state.Count
		}
	}
	if total > 0 {
		return total
	}

	for name, check := range status.Health.Checks {
		text := strings.ToLower(name + " " + extractCephCheckSummary(check.Summary))
		for _, detail := range check.Detail {
			text += " " + strings.ToLower(detail.Message)
		}
		if !strings.Contains(text, "inconsistent") && !strings.Contains(text, "pg_damaged") {
			continue
		}
		if count := extractFirstPositiveInt(text); count > 0 {
			return count
		}
		return 1
	}

	for _, summary := range status.Health.Summary {
		text := strings.ToLower(summary.Summary + " " + summary.Message)
		if !strings.Contains(text, "inconsistent") && !strings.Contains(text, "pg_damaged") {
			continue
		}
		if count := extractFirstPositiveInt(text); count > 0 {
			return count
		}
		return 1
	}

	return 0
}

var firstPositiveIntPattern = regexp.MustCompile(`\b([1-9][0-9]*)\b`)

func extractFirstPositiveInt(value string) int {
	match := firstPositiveIntPattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return 0
	}
	parsed, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return parsed
}

func countCephMonitorDaemons(status *proxmox.CephStatus) int {
	if status == nil {
		return 0
	}
	if status.MonMap.NumMons > 0 {
		return status.MonMap.NumMons
	}
	return countServiceDaemons(status.ServiceMap.Services, "mon")
}

func countCephManagerDaemons(status *proxmox.CephStatus) int {
	if status == nil {
		return 0
	}
	if status.MgrMap.NumMgrs > 0 {
		return status.MgrMap.NumMgrs
	}
	if status.MgrMap.ActiveName != "" {
		return 1 + len(status.MgrMap.Standbys)
	}
	return countServiceDaemons(status.ServiceMap.Services, "mgr")
}

// summarizeCephHealth extracts human-readable messages from the Ceph health payload.
func summarizeCephHealth(status *proxmox.CephStatus) string {
	if status == nil {
		return ""
	}

	messages := make([]string, 0)

	for _, summary := range status.Health.Summary {
		switch {
		case summary.Message != "":
			messages = append(messages, summary.Message)
		case summary.Summary != "":
			messages = append(messages, summary.Summary)
		}
	}

	for checkName, check := range status.Health.Checks {
		if msg := extractCephCheckSummary(check.Summary); msg != "" {
			messages = append(messages, fmt.Sprintf("%s: %s", checkName, msg))
			continue
		}
		for _, detail := range check.Detail {
			if detail.Message != "" {
				messages = append(messages, fmt.Sprintf("%s: %s", checkName, detail.Message))
				break
			}
		}
	}

	return strings.Join(messages, "; ")
}

// extractCephCheckSummary attempts to parse the flexible summary field in Ceph health checks into a message string.
func extractCephCheckSummary(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var obj struct {
		Message string `json:"message"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if obj.Message != "" {
			return obj.Message
		}
		if obj.Summary != "" {
			return obj.Summary
		}
	}

	var list []struct {
		Message string `json:"message"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, item := range list {
			if item.Message != "" {
				return item.Message
			}
			if item.Summary != "" {
				return item.Summary
			}
		}
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}

	return ""
}

// countServiceDaemons returns the number of daemons defined for a given service type.
func countServiceDaemons(services map[string]proxmox.CephServiceDefinition, serviceType string) int {
	if services == nil {
		return 0
	}
	definition, ok := services[serviceType]
	if !ok {
		return 0
	}
	return len(definition.Daemons)
}
