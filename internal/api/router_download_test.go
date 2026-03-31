package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTempPulseBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PULSE_BIN_DIR", dir)
	return dir
}

func TestHandleDownloadHostAgentServesWindowsExe(t *testing.T) {
	binDir := setupTempPulseBin(t)
	filePath := filepath.Join(binDir, "pulse-host-agent-windows-amd64.exe")
	if err := os.WriteFile(filePath, []byte("exe-binary"), 0o755); err != nil {
		t.Fatalf("failed to write test binary: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/download/pulse-host-agent?platform=windows&arch=amd64", nil)
	rr := httptest.NewRecorder()

	router := &Router{}
	router.handleDownloadHostAgent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	if got := rr.Body.String(); got != "exe-binary" {
		t.Fatalf("unexpected response body: %q", got)
	}
}

func TestHandleDownloadHostAgentServesLinuxArm64(t *testing.T) {
	binDir := setupTempPulseBin(t)
	filePath := filepath.Join(binDir, "pulse-host-agent-linux-arm64")
	payload := []byte("arm64-binary")
	if err := os.WriteFile(filePath, payload, 0o755); err != nil {
		t.Fatalf("failed to write test binary: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/download/pulse-host-agent?platform=linux&arch=arm64", nil)
	rr := httptest.NewRecorder()

	router := &Router{}
	router.handleDownloadHostAgent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	if got := rr.Body.String(); got != string(payload) {
		t.Fatalf("unexpected response body: %q", got)
	}
}

func TestHandleDownloadHostAgentServesChecksumForWindowsExe(t *testing.T) {
	const (
		arch     = "amd64"
		filename = "pulse-host-agent-windows-" + arch + ".exe"
	)
	binDir := setupTempPulseBin(t)
	filePath := filepath.Join(binDir, filename)

	payload := []byte("checksum-data")
	if err := os.WriteFile(filePath, payload, 0o755); err != nil {
		t.Fatalf("failed to write test binary: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/download/pulse-host-agent.sha256?platform=windows&arch=%s", arch), nil)
	rr := httptest.NewRecorder()

	router := &Router{}
	router.handleDownloadHostAgent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	expected := fmt.Sprintf("%x", sha256.Sum256(payload))
	if got := strings.TrimSpace(rr.Body.String()); got != expected {
		t.Fatalf("unexpected checksum body: got %q want %q", got, expected)
	}
}

func TestHandleDownloadHostAgentRejectsArchWithoutPlatform(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/download/pulse-host-agent?arch=amd64", nil)
	rr := httptest.NewRecorder()

	router := &Router{}
	router.handleDownloadHostAgent(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rr.Code)
	}
}

func TestHandleDownloadHostAgentRejectsUnsupportedTarget(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/download/pulse-host-agent?platform=windows&arch=unit-test", nil)
	rr := httptest.NewRecorder()

	router := &Router{}
	router.handleDownloadHostAgent(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rr.Code)
	}
}

func TestHandleDownloadHostAgentAllowsHEAD(t *testing.T) {
	binDir := setupTempPulseBin(t)
	filePath := filepath.Join(binDir, "pulse-host-agent-linux-amd64")
	if err := os.WriteFile(filePath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("failed to write test binary: %v", err)
	}

	req := httptest.NewRequest(http.MethodHead, "/download/pulse-host-agent?platform=linux&arch=amd64", nil)
	rr := httptest.NewRecorder()

	router := &Router{}
	router.handleDownloadHostAgent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for HEAD, got %d", rr.Code)
	}

	// HEAD response should have Content-Length but no body
	// Note: httptest.ResponseRecorder might capture body even for HEAD if handler writes it,
	// but standard http.Server suppresses it.
	// However, our handler uses http.ServeContent which respects HEAD.
	if rr.Body.Len() > 0 {
		t.Fatalf("expected empty body for HEAD, got %d bytes", rr.Body.Len())
	}
}

func TestHandleDownloadHostAgent_ProxyFromGitHub(t *testing.T) {
	binDir := setupTempPulseBin(t)
	router := &Router{
		projectRoot:         t.TempDir(),
		installScriptClient: newTestInstallScriptClient(t, "https://github.com/rcourtman/Pulse/releases/latest/download/pulse-host-agent-freebsd-amd64", http.StatusOK, "freebsd-binary", nil),
	}

	for _, path := range []string{
		filepath.Join(binDir, "pulse-host-agent-freebsd-amd64"),
		"/opt/pulse/pulse-host-agent-freebsd-amd64",
		filepath.Join("/app", "pulse-host-agent-freebsd-amd64"),
	} {
		if _, err := os.Stat(path); err == nil {
			t.Skipf("local host-agent binary exists at %s; skipping proxy fallback test", path)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/download/pulse-host-agent?platform=freebsd&arch=amd64", nil)
	rr := httptest.NewRecorder()

	router.handleDownloadHostAgent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != "freebsd-binary" {
		t.Fatalf("unexpected response body: %q", got)
	}
	if got := rr.Header().Get("X-Served-From"); got != "github-proxy" {
		t.Fatalf("unexpected X-Served-From header: %q", got)
	}
	expectedChecksum := fmt.Sprintf("%x", sha256.Sum256([]byte("freebsd-binary")))
	if got := rr.Header().Get("X-Checksum-Sha256"); got != expectedChecksum {
		t.Fatalf("unexpected checksum header: got %q want %q", got, expectedChecksum)
	}
}

func TestHandleDownloadHostAgentChecksum_ProxyFromGitHub(t *testing.T) {
	binDir := setupTempPulseBin(t)
	router := &Router{
		projectRoot:         t.TempDir(),
		installScriptClient: newTestInstallScriptClient(t, "https://github.com/rcourtman/Pulse/releases/latest/download/pulse-host-agent-freebsd-amd64", http.StatusOK, "freebsd-binary", nil),
	}

	for _, path := range []string{
		filepath.Join(binDir, "pulse-host-agent-freebsd-amd64"),
		"/opt/pulse/pulse-host-agent-freebsd-amd64",
		filepath.Join("/app", "pulse-host-agent-freebsd-amd64"),
	} {
		if _, err := os.Stat(path); err == nil {
			t.Skipf("local host-agent binary exists at %s; skipping proxy checksum fallback test", path)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/download/pulse-host-agent.sha256?platform=freebsd&arch=amd64", nil)
	rr := httptest.NewRecorder()

	router.handleDownloadHostAgent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d body=%s", rr.Code, rr.Body.String())
	}
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte("freebsd-binary")))
	if got := strings.TrimSpace(rr.Body.String()); got != expected {
		t.Fatalf("unexpected checksum body: got %q want %q", got, expected)
	}
	if got := rr.Header().Get("X-Served-From"); got != "github-proxy" {
		t.Fatalf("unexpected X-Served-From header: %q", got)
	}
}
