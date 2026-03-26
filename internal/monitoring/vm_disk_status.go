package monitoring

import "strings"

func classifyGuestAgentDiskStatusError(err error) string {
	if err == nil {
		return ""
	}

	errStr := err.Error()
	errStrLower := strings.ToLower(errStr)

	switch {
	case strings.Contains(errStr, "QEMU guest agent is not running"):
		return "agent-not-running"
	case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded"):
		return "agent-timeout"
	case strings.Contains(errStr, "500") && (strings.Contains(errStr, "not running") || strings.Contains(errStr, "not available")):
		return "agent-not-running"
	case (strings.Contains(errStr, "403") || strings.Contains(errStr, "401")) &&
		(strings.Contains(errStrLower, "permission") || strings.Contains(errStrLower, "forbidden") || strings.Contains(errStrLower, "not allowed")):
		return "permission-denied"
	case strings.Contains(errStr, "500"):
		return "agent-not-running"
	default:
		return "agent-error"
	}
}

func shouldCarryForwardQEMUDisk(reason string) bool {
	switch reason {
	case "", "vm-stopped", "agent-disabled", "no-agent":
		return false
	default:
		return true
	}
}
