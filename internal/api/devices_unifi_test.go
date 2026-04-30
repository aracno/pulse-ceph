package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBuildUniFiProxyURL(t *testing.T) {
	target, err := buildUniFiProxyURL(unifiProxyRequest{
		BaseURL:  "api.ui.com",
		Endpoint: "v1/devices",
	})
	if err != nil {
		t.Fatalf("buildUniFiProxyURL returned error: %v", err)
	}
	if target != "https://api.ui.com/v1/devices" {
		t.Fatalf("unexpected target: %s", target)
	}
}

func TestBuildUniFiProxyURLRejectsUnsafeTargets(t *testing.T) {
	tests := []unifiProxyRequest{
		{BaseURL: "http://api.ui.com", Endpoint: "/v1/devices"},
		{BaseURL: "https://example.com", Endpoint: "/v1/devices"},
		{BaseURL: "https://api.ui.com", Endpoint: "/v1/unknown"},
	}

	for _, tc := range tests {
		if target, err := buildUniFiProxyURL(tc); err == nil {
			t.Fatalf("expected error for %+v, got target %s", tc, target)
		}
	}
}

func TestExtractUniFiErrorMessage(t *testing.T) {
	if got := extractUniFiErrorMessage([]byte(`{"message":"bad api key"}`)); got != "bad api key" {
		t.Fatalf("unexpected JSON error message: %q", got)
	}
	if got := extractUniFiErrorMessage([]byte("plain failure")); got != "plain failure" {
		t.Fatalf("unexpected text error message: %q", got)
	}
}

func TestHandleUniFiProxyRelaysAllowedRequest(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/devices" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Fatalf("unexpected api key header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"dev-1","name":"Switch"}]}`))
	}))
	defer upstream.Close()

	originalClient := unifiProxyHTTPClient
	originalAllowed := unifiAllowedHosts
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	unifiProxyHTTPClient = upstream.Client()
	unifiAllowedHosts = map[string]bool{
		upstreamURL.Hostname(): true,
	}
	defer func() {
		unifiProxyHTTPClient = originalClient
		unifiAllowedHosts = originalAllowed
	}()

	body := `{"baseUrl":"` + upstream.URL + `","endpoint":"/v1/devices","apiKey":"test-key"}`
	req := httptest.NewRequest(http.MethodPost, "/api/devices/unifi/proxy", strings.NewReader(body))
	rec := httptest.NewRecorder()

	(&Router{}).handleUniFiProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"dev-1"`) {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}
