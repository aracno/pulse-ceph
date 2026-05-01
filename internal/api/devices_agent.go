package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rcourtman/pulse-go-rewrite/internal/utils"
)

type deviceAgentPayload struct {
	DeviceID      string              `json:"deviceId"`
	Name          string              `json:"name"`
	Host          string              `json:"host"`
	Type          string              `json:"type"`
	Vendor        string              `json:"vendor"`
	Model         string              `json:"model"`
	Site          string              `json:"site"`
	Status        managedDeviceStatus `json:"status"`
	UptimeSeconds *float64            `json:"uptimeSeconds"`
	LatencyMs     *float64            `json:"latencyMs"`
	PacketLoss    *float64            `json:"packetLoss"`
	Advanced      *deviceAdvanced     `json:"advanced"`
	Raw           map[string]any      `json:"raw"`
}

func (s *devicesStore) findAgentCheckByToken(token string) (deviceCheck, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return deviceCheck{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, check := range s.state.Checks {
		if check.Type == deviceCheckAgent && check.Enabled && check.APIKey == token {
			return check, true
		}
	}
	return deviceCheck{}, false
}

func (s *devicesStore) ingestAgentMetrics(check deviceCheck, payload deviceAgentPayload) (managedDevice, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := strings.TrimSpace(payload.DeviceID)
	if id == "" {
		id = strings.TrimSpace(payload.Host)
	}
	if id == "" {
		id = strings.TrimSpace(payload.Name)
	}
	if id == "" {
		id = utils.GenerateID("device-agent")
	}
	name := firstNonEmpty(payload.Name, payload.Host, id)
	status := payload.Status
	if status == "" {
		status = deviceOnline
	}
	advanced := payload.Advanced
	if advanced != nil {
		advanced.CollectedAt = firstNonEmpty(advanced.CollectedAt, now)
	}
	device := managedDevice{
		ID:            "agent-" + sanitizeDeviceID(id),
		AccountID:     check.ID,
		AccountType:   deviceCheckAgent,
		Name:          name,
		Host:          firstNonEmpty(payload.Host, id),
		Type:          firstNonEmpty(payload.Type, "other"),
		Vendor:        payload.Vendor,
		Model:         payload.Model,
		Site:          payload.Site,
		Status:        status,
		LatencyMs:     payload.LatencyMs,
		PacketLoss:    payload.PacketLoss,
		UptimeSeconds: payload.UptimeSeconds,
		Advanced:      advanced,
		LastSeen:      now,
		LastCheckedAt: now,
		Raw:           payload.Raw,
	}
	if device.UptimeSeconds != nil {
		device.Uptime = formatDurationSeconds(*device.UptimeSeconds)
	}
	saved, err := s.upsertDevice(device)
	if err != nil {
		return saved, err
	}
	s.markCheckPolled(check.ID, nil)
	s.evaluateDeviceAlerts()
	return saved, nil
}

func sanitizeDeviceID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if r == '.' || r == '-' || r == '_' {
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return utils.GenerateID("unknown")
	}
	return out
}

func (s *devicesStore) agentScript() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Agent.Script
}

func (s *devicesStore) updateAgentScript(script string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(script) == "" {
		script = defaultDeviceAgentScript()
	}
	s.state.Agent.Script = script
	return s.saveLocked()
}

func renderDeviceAgentScript(template, baseURL, token string, interval int) string {
	replacer := strings.NewReplacer(
		"{{BASE_URL}}", strings.TrimRight(baseURL, "/"),
		"{{TOKEN}}", token,
		"{{INTERVAL_SECONDS}}", fmt.Sprintf("%d", interval),
	)
	return replacer.Replace(template)
}

func defaultDeviceAgentScript() string {
	return `#!/bin/sh
set -eu

BASE_URL="{{BASE_URL}}"
TOKEN="{{TOKEN}}"
INTERVAL_SECONDS="{{INTERVAL_SECONDS}}"
MODE="${1:-run}"
AGENT_PATH="/usr/local/bin/pulse-device-agent.sh"
CRON_PATH="/etc/cron.d/pulse-device-agent"

install_agent() {
  if [ "$(id -u)" != "0" ]; then
    echo "Run as root to install the cron entry." >&2
    exit 1
  fi
  umask 077
  curl -fsSL "$BASE_URL/api/devices/agent/script.sh?token=$TOKEN" -o "$AGENT_PATH"
  chmod 700 "$AGENT_PATH"
  minutes=$((INTERVAL_SECONDS / 60))
  [ "$minutes" -lt 1 ] && minutes=1
  printf '*/%s * * * * root %s run >/dev/null 2>&1\n' "$minutes" "$AGENT_PATH" > "$CRON_PATH"
  "$AGENT_PATH" run
  echo "Pulse device advanced agent installed."
}

num() { awk "BEGIN { printf \"%.2f\", $1 }"; }
json_escape() { sed 's/\\/\\\\/g; s/"/\\"/g' | tr -d '\n'; }

iface_rate() {
  iface="$1"
  [ -r /sys/class/net/"$iface"/statistics/rx_bytes ] || { echo "\"$iface\":{\"rx\":null,\"tx\":null}"; return; }
  rx1=$(cat /sys/class/net/"$iface"/statistics/rx_bytes 2>/dev/null || echo 0)
  tx1=$(cat /sys/class/net/"$iface"/statistics/tx_bytes 2>/dev/null || echo 0)
  sleep 1
  rx2=$(cat /sys/class/net/"$iface"/statistics/rx_bytes 2>/dev/null || echo "$rx1")
  tx2=$(cat /sys/class/net/"$iface"/statistics/tx_bytes 2>/dev/null || echo "$tx1")
  echo "\"$iface\":{\"rx\":$((rx2-rx1)),\"tx\":$((tx2-tx1))}"
}

latency_average() {
  target="re.pool.ntp.org"
  total=0
  ok=0
  i=0
  while [ "$i" -lt 4 ]; do
    line=$(ping -c 1 -W 2 "$target" 2>/dev/null | awk -F'time=' '/time=/{print $2}' | awk '{print $1}' | head -n1)
    if [ -n "$line" ]; then total=$(awk "BEGIN{print $total + $line}"); ok=$((ok+1)); fi
    i=$((i+1))
  done
  if [ "$ok" -eq 0 ]; then echo "null"; else awk "BEGIN{printf \"%.1f\", $total / $ok}"; fi
}

security_checks() {
  score=100
  checks=""
  add_check() {
    id="$1"; label="$2"; passed="$3"; detail="$4"; penalty="$5"
    [ "$passed" = "true" ] || score=$((score-penalty))
    escaped=$(printf '%s' "$detail" | json_escape)
    item="{\"id\":\"$id\",\"label\":\"$label\",\"passed\":$passed,\"detail\":\"$escaped\"}"
    [ -n "$checks" ] && checks="$checks,"
    checks="$checks$item"
  }
  root_login="unknown"
  [ -r /etc/ssh/sshd_config ] && root_login=$(awk 'tolower($1)=="permitrootlogin"{print tolower($2)}' /etc/ssh/sshd_config | tail -n1)
  [ "$root_login" != "yes" ] && add_check "ssh_root" "SSH root login disabled" true "$root_login" 15 || add_check "ssh_root" "SSH root login disabled" false "$root_login" 15
  pass_auth="unknown"
  [ -r /etc/ssh/sshd_config ] && pass_auth=$(awk 'tolower($1)=="passwordauthentication"{print tolower($2)}' /etc/ssh/sshd_config | tail -n1)
  [ "$pass_auth" != "yes" ] && add_check "ssh_password" "SSH password auth disabled" true "$pass_auth" 10 || add_check "ssh_password" "SSH password auth disabled" false "$pass_auth" 10
  ports=$(ss -lntu 2>/dev/null | awk 'NR>1{print $5}' | wc -l | tr -d ' ')
  [ "${ports:-0}" -le 12 ] && add_check "open_ports" "Limited listening sockets" true "$ports listening sockets" 10 || add_check "open_ports" "Limited listening sockets" false "$ports listening sockets" 10
  if command -v ufw >/dev/null 2>&1; then ufw status | grep -qi active && fw=true || fw=false; else command -v nft >/dev/null 2>&1 || command -v iptables >/dev/null 2>&1 && fw=true || fw=false; fi
  [ "$fw" = "true" ] && add_check "firewall" "Firewall tooling present or active" true "" 10 || add_check "firewall" "Firewall tooling present or active" false "ufw/nft/iptables not found" 10
  world=$(find /tmp /var/tmp -xdev -type f -perm -0002 2>/dev/null | head -n 25 | wc -l | tr -d ' ')
  [ "${world:-0}" -le 10 ] && add_check "world_writable" "Few world-writable temp files" true "$world files" 5 || add_check "world_writable" "Few world-writable temp files" false "$world files" 5
  [ "$score" -lt 0 ] && score=0
  echo "\"securityScore\":$score,\"securityChecks\":[$checks]"
}

collect_and_push() {
  hostname=$(hostname 2>/dev/null || echo unknown)
  fqdn=$(hostname -f 2>/dev/null || echo "$hostname")
  os=$(grep PRETTY_NAME /etc/os-release 2>/dev/null | cut -d= -f2- | tr -d '"' || uname -s)
  kernel=$(uname -r)
  uptime_seconds=$(awk '{printf "%.0f", $1}' /proc/uptime 2>/dev/null || echo 0)
  cpu_idle=$(top -bn1 2>/dev/null | awk -F',' '/Cpu\(s\)|%Cpu/{for(i=1;i<=NF;i++) if($i ~ /id/) {gsub(/[^0-9.]/,"",$i); print $i; exit}}')
  [ -z "${cpu_idle:-}" ] && cpu_idle=0
  cpu=$(awk "BEGIN{printf \"%.1f\", 100 - $cpu_idle}")
  mem=$(free | awk '/Mem:/ {printf "%.1f", ($3/$2)*100}')
  disk=$(df -P / | awk 'NR==2 {gsub(/%/,"",$5); print $5}')
  latency=$(latency_average)
  default_iface=$(ip route 2>/dev/null | awk '/default/ {print $5; exit}')
  [ -z "${default_iface:-}" ] && default_iface="eth0"
  wan=$(iface_rate "$default_iface")
  eth_json=""
  for iface in eth0 eth1 eth2 eth3; do
    item=$(iface_rate "$iface")
    [ -n "$eth_json" ] && eth_json="$eth_json,"
    eth_json="$eth_json$item"
  done
  sec=$(security_checks)
  payload=$(cat <<JSON
{"deviceId":"$fqdn","name":"$hostname","host":"$fqdn","type":"other","vendor":"Linux","status":"online","uptimeSeconds":$uptime_seconds,"latencyMs":$latency,"packetLoss":0,"advanced":{"cpuPercent":$cpu,"memoryPercent":$mem,"diskPercent":$disk,"wanRxBps":$(echo "$wan" | sed 's/^.*"rx":\([^,}]*\).*$/\1/'),"wanTxBps":$(echo "$wan" | sed 's/^.*"tx":\([^,}]*\).*$/\1/'),"ethThroughputBps":{$eth_json},$sec,"os":"$(printf '%s' "$os" | json_escape)","kernel":"$(printf '%s' "$kernel" | json_escape)","hostname":"$(printf '%s' "$hostname" | json_escape)","collectedAt":"$(date -u +%Y-%m-%dT%H:%M:%SZ)"}} 
JSON
)
  curl -fsS -X POST "$BASE_URL/api/devices/agent/push" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" --data "$payload"
}

case "$MODE" in
  install) install_agent ;;
  run) collect_and_push ;;
  *) echo "Usage: $0 [install|run]" >&2; exit 2 ;;
esac
`
}

func readBearerToken(req *http.Request) string {
	header := strings.TrimSpace(req.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return strings.TrimSpace(req.URL.Query().Get("token"))
}

func decodeAgentPayload(req *http.Request) (deviceAgentPayload, error) {
	var payload deviceAgentPayload
	err := json.NewDecoder(req.Body).Decode(&payload)
	return payload, err
}
