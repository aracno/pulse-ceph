package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const defaultUniFiAPIBaseURL = "https://api.ui.com"

var (
	unifiProxyHTTPClient = &http.Client{Timeout: 15 * time.Second}
	unifiAllowedHosts    = map[string]bool{"api.ui.com": true}
	unifiAllowedPaths    = map[string]bool{
		"/v1/devices":        true,
		"/v1/hosts":          true,
		"/v1/sites":          true,
		"/ea/isp-metrics/5m": true,
		"/ea/isp-metrics/1h": true,
		"/ea/isp-metrics/1d": true,
	}
)

type unifiProxyRequest struct {
	BaseURL  string `json:"baseUrl"`
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"apiKey"`
}

func (r *Router) handleUniFiProxy(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload unifiProxyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 64*1024)).Decode(&payload); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Invalid UniFi proxy request body", nil)
		return
	}

	target, err := buildUniFiProxyURL(payload)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid_unifi_target", err.Error(), nil)
		return
	}
	apiKey := strings.TrimSpace(payload.APIKey)
	if apiKey == "" {
		writeErrorResponse(w, http.StatusBadRequest, "missing_api_key", "UniFi API key is required", nil)
		return
	}

	outbound, err := http.NewRequestWithContext(req.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid_unifi_target", "Unable to build UniFi API request", nil)
		return
	}
	outbound.Header.Set("Accept", "application/json")
	outbound.Header.Set("X-API-Key", apiKey)

	response, err := unifiProxyHTTPClient.Do(outbound)
	if err != nil {
		log.Warn().Err(err).Str("target", target).Msg("UniFi API proxy request failed")
		writeErrorResponse(w, http.StatusBadGateway, "unifi_request_failed", "Pulse backend could not reach the UniFi API", nil)
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 10*1024*1024))
	if err != nil {
		writeErrorResponse(w, http.StatusBadGateway, "unifi_response_failed", "Pulse backend could not read the UniFi API response", nil)
		return
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := extractUniFiErrorMessage(body)
		if message == "" {
			message = "UniFi API returned HTTP " + response.Status
		}
		writeErrorResponse(w, http.StatusBadGateway, "unifi_api_error", message, map[string]string{
			"upstream_status": response.Status,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		log.Debug().Err(err).Msg("Failed to write UniFi proxy response")
	}
}

func buildUniFiProxyURL(payload unifiProxyRequest) (string, error) {
	baseURL := strings.TrimSpace(payload.BaseURL)
	if baseURL == "" {
		baseURL = defaultUniFiAPIBaseURL
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "https://" + baseURL
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errInvalidUniFiTarget("Invalid UniFi API base URL")
	}
	if parsed.Scheme != "https" {
		return "", errInvalidUniFiTarget("UniFi API base URL must use HTTPS")
	}

	host := strings.ToLower(parsed.Hostname())
	if !unifiAllowedHosts[host] {
		return "", errInvalidUniFiTarget("UniFi API host is not allowed")
	}

	endpoint := strings.TrimSpace(payload.Endpoint)
	if endpoint == "" {
		endpoint = "/v1/devices"
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	if !unifiAllowedPaths[endpoint] {
		return "", errInvalidUniFiTarget("UniFi API endpoint is not allowed")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + endpoint
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

type errInvalidUniFiTarget string

func (e errInvalidUniFiTarget) Error() string {
	return string(e)
}

func extractUniFiErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		text := strings.TrimSpace(string(body))
		if len(text) > 200 {
			return text[:200]
		}
		return text
	}

	for _, key := range []string{"message", "error", "detail"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
