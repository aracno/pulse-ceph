package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rcourtman/pulse-go-rewrite/internal/config"
	"github.com/rcourtman/pulse-go-rewrite/internal/utils"
	"github.com/rs/zerolog/log"
)

func (r *Router) requireDeviceScope(handler http.HandlerFunc) http.HandlerFunc {
	return RequireAdmin(r.config, func(w http.ResponseWriter, req *http.Request) {
		scope := config.ScopeSettingsRead
		switch req.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			scope = config.ScopeSettingsWrite
		}
		if !ensureScope(w, req, scope) {
			return
		}
		handler(w, req)
	})
}

func (r *Router) handleDeviceAgentScript(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		_ = utils.WriteJSONResponse(w, r.deviceStore.snapshot().Agent)
	case http.MethodPut:
		var payload deviceAgentScriptSettings
		if !decodeJSONBody(w, req, &payload) {
			return
		}
		if err := r.deviceStore.updateAgentScript(payload.Script); err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "save_failed", "Failed to save device agent script", nil)
			return
		}
		_ = utils.WriteJSONResponse(w, r.deviceStore.snapshot().Agent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleDeviceAgentScriptDownload(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := readBearerToken(req)
	check, ok := r.deviceStore.findAgentCheckByToken(token)
	if !ok {
		writeErrorResponse(w, http.StatusUnauthorized, "invalid_agent_token", "Invalid device agent token", nil)
		return
	}
	baseURL := req.URL.Query().Get("baseUrl")
	if baseURL == "" {
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, req.Host)
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(renderDeviceAgentScript(r.deviceStore.agentScript(), baseURL, token, check.IntervalSeconds)))
}

func (r *Router) handleDeviceAgentPush(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := readBearerToken(req)
	check, ok := r.deviceStore.findAgentCheckByToken(token)
	if !ok {
		writeErrorResponse(w, http.StatusUnauthorized, "invalid_agent_token", "Invalid device agent token", nil)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, 1024*1024)
	payload, err := decodeAgentPayload(req)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid_payload", "Invalid agent metrics payload", nil)
		return
	}
	device, err := r.deviceStore.ingestAgentMetrics(check, payload)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "ingest_failed", "Failed to persist agent metrics", nil)
		return
	}
	_ = utils.WriteJSONResponse(w, device)
}

func (r *Router) handleDevicesState(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := utils.WriteJSONResponse(w, r.deviceStore.snapshot()); err != nil {
		log.Error().Err(err).Msg("Failed to write devices state")
	}
}

func (r *Router) handleDeviceChecks(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		if err := utils.WriteJSONResponse(w, r.deviceStore.snapshot().Checks); err != nil {
			log.Error().Err(err).Msg("Failed to write device checks")
		}
	case http.MethodPost:
		var check deviceCheck
		if !decodeJSONBody(w, req, &check) {
			return
		}
		saved, err := r.deviceStore.upsertCheck(check)
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "save_failed", "Failed to save device check", nil)
			return
		}
		if saved.Type == deviceCheckAgent {
			saved.InstallCommand = buildDeviceAgentInstallCommand(req, saved.APIKey)
		}
		_ = utils.WriteJSONResponse(w, redactDeviceCheck(saved))
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleDeviceCheck(w http.ResponseWriter, req *http.Request) {
	id := strings.Trim(strings.TrimPrefix(req.URL.Path, "/api/devices/checks/"), "/")
	if id == "" {
		http.NotFound(w, req)
		return
	}
	switch req.Method {
	case http.MethodPut:
		var check deviceCheck
		if !decodeJSONBody(w, req, &check) {
			return
		}
		check.ID = id
		saved, err := r.deviceStore.upsertCheck(check)
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "save_failed", "Failed to save device check", nil)
			return
		}
		if saved.Type == deviceCheckAgent {
			saved.InstallCommand = buildDeviceAgentInstallCommand(req, saved.APIKey)
		}
		_ = utils.WriteJSONResponse(w, redactDeviceCheck(saved))
	case http.MethodDelete:
		if err := r.deviceStore.deleteCheck(id); err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "delete_failed", "Failed to delete device check", nil)
			return
		}
		_ = utils.WriteJSONResponse(w, map[string]bool{"success": true})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func buildDeviceAgentInstallCommand(req *http.Request, token string) string {
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, req.Host)
	return fmt.Sprintf("curl -fsSL '%s/api/devices/agent/script.sh?token=%s' | sudo sh -s -- install", baseURL, token)
}

func (r *Router) handleManagedDevices(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		if err := utils.WriteJSONResponse(w, r.deviceStore.snapshot().Devices); err != nil {
			log.Error().Err(err).Msg("Failed to write managed devices")
		}
	case http.MethodPost:
		var device managedDevice
		if !decodeJSONBody(w, req, &device) {
			return
		}
		saved, err := r.deviceStore.upsertDevice(device)
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "save_failed", "Failed to save device", nil)
			return
		}
		_ = utils.WriteJSONResponse(w, saved)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleManagedDevice(w http.ResponseWriter, req *http.Request) {
	id := strings.Trim(strings.TrimPrefix(req.URL.Path, "/api/devices/inventory/"), "/")
	if id == "" {
		http.NotFound(w, req)
		return
	}
	switch req.Method {
	case http.MethodPut:
		var device managedDevice
		if !decodeJSONBody(w, req, &device) {
			return
		}
		device.ID = id
		saved, err := r.deviceStore.upsertDevice(device)
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "save_failed", "Failed to save device", nil)
			return
		}
		_ = utils.WriteJSONResponse(w, saved)
	case http.MethodDelete:
		if err := r.deviceStore.deleteDevice(id); err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "delete_failed", "Failed to delete device", nil)
			return
		}
		_ = utils.WriteJSONResponse(w, map[string]bool{"success": true})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleDevicesAlerts(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		_ = utils.WriteJSONResponse(w, r.deviceStore.snapshot().Alerts)
	case http.MethodPut:
		var alerts deviceAlertSettings
		if !decodeJSONBody(w, req, &alerts) {
			return
		}
		if err := r.deviceStore.updateAlerts(alerts); err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "save_failed", "Failed to save device alert settings", nil)
			return
		}
		_ = utils.WriteJSONResponse(w, r.deviceStore.snapshot().Alerts)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleDevicesPoll(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = utils.WriteJSONResponse(w, r.deviceStore.pollDue(true))
}

func (r *Router) handleUniFiDiscover(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		unifiProxyRequest
		CheckID string `json:"checkId"`
	}
	if !decodeJSONBody(w, req, &payload) {
		return
	}
	check := deviceCheck{}
	if payload.CheckID != "" {
		var ok bool
		check, ok = r.deviceStore.findCheck(payload.CheckID)
		if !ok || check.Type != deviceCheckUniFi {
			writeErrorResponse(w, http.StatusNotFound, "unifi_check_not_found", "UniFi check not found", nil)
			return
		}
	} else {
		check = deviceCheck{
			ID:     "unifi-discovery",
			Type:   deviceCheckUniFi,
			Host:   payload.BaseURL,
			APIKey: payload.APIKey,
		}
	}
	devices, err := r.deviceStore.discoverUniFi(check)
	if err != nil {
		writeErrorResponse(w, http.StatusBadGateway, "unifi_discovery_failed", err.Error(), nil)
		return
	}
	_ = utils.WriteJSONResponse(w, map[string]any{"devices": devices})
}

func decodeJSONBody(w http.ResponseWriter, req *http.Request, target any) bool {
	req.Body = http.MaxBytesReader(w, req.Body, 1024*1024)
	if err := json.NewDecoder(req.Body).Decode(target); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Invalid request body", nil)
		return false
	}
	return true
}
