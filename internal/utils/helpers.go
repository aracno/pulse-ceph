package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
)

// GenerateID generates a unique ID with the given prefix
func GenerateID(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, uuid.NewString())
}

// WriteJSONResponse writes a JSON response to the http.ResponseWriter
func WriteJSONResponse(w http.ResponseWriter, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	// Use Marshal instead of Encoder for better performance with large payloads
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = w.Write(jsonData)
	return err
}

// ParseBool interprets common boolean strings, returning true for typical truthy values.
func ParseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// GetenvTrim returns the environment variable value with surrounding whitespace removed.
func GetenvTrim(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// NormalizeVersion normalizes version strings for comparison by:
// 1. Stripping whitespace
// 2. Removing the "v" prefix (e.g., "v4.33.1" -> "4.33.1")
// 3. Stripping build metadata after "+" (e.g., "4.36.2+git.14.dirty" -> "4.36.2")
//
// Per semver spec, build metadata MUST be ignored when determining version precedence.
// This fixes issues where dirty builds like "4.36.2+git.14.g469307d6.dirty" would
// incorrectly be treated as newer than "4.36.2", causing infinite update loops.
func NormalizeVersion(version string) string {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	// Strip build metadata (everything after +)
	// Per semver: build metadata MUST be ignored when determining version precedence
	if idx := strings.Index(v, "+"); idx != -1 {
		v = v[:idx]
	}
	return v
}

// CompareVersions compares two semver-like version strings.
// Returns:
//
//	1 if a > b (a is newer)
//	0 if a == b
//
// -1 if a < b (b is newer)
//
// Handles versions like "4.33.1", "v4.33.1", "4.33", and semver prereleases
// like "5.1.26-rc.2" gracefully.
func CompareVersions(a, b string) int {
	coreA, prereleaseA := splitVersionForComparison(a)
	coreB, prereleaseB := splitVersionForComparison(b)

	maxLen := len(coreA)
	if len(coreB) > maxLen {
		maxLen = len(coreB)
	}

	for i := 0; i < maxLen; i++ {
		partA := versionPartAt(coreA, i)
		partB := versionPartAt(coreB, i)
		if partA > partB {
			return 1
		}
		if partA < partB {
			return -1
		}
	}

	return compareVersionPrerelease(prereleaseA, prereleaseB)
}

func splitVersionForComparison(version string) ([]int, []string) {
	normalized := NormalizeVersion(version)
	prerelease := ""
	if idx := strings.Index(normalized, "-"); idx != -1 {
		prerelease = normalized[idx+1:]
		normalized = normalized[:idx]
	}

	parts := strings.Split(normalized, ".")
	core := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			core = append(core, 0)
			continue
		}

		value := 0
		fmt.Sscanf(part, "%d", &value)
		core = append(core, value)
	}

	if prerelease == "" {
		return core, nil
	}
	return core, strings.Split(prerelease, ".")
}

func versionPartAt(parts []int, idx int) int {
	if idx >= len(parts) {
		return 0
	}
	return parts[idx]
}

func compareVersionPrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}

	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}

	for i := 0; i < limit; i++ {
		if cmp := compareVersionIdentifier(a[i], b[i]); cmp != 0 {
			return cmp
		}
	}

	switch {
	case len(a) > len(b):
		return 1
	case len(a) < len(b):
		return -1
	default:
		return 0
	}
}

func compareVersionIdentifier(a, b string) int {
	if a == b {
		return 0
	}

	aNumeric := isNumericVersionIdentifier(a)
	bNumeric := isNumericVersionIdentifier(b)
	switch {
	case aNumeric && bNumeric:
		return compareNumericIdentifier(a, b)
	case aNumeric:
		return -1
	case bNumeric:
		return 1
	default:
		return compareVersionChunks(a, b)
	}
}

func isNumericVersionIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func compareNumericIdentifier(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}

	switch {
	case len(a) > len(b):
		return 1
	case len(a) < len(b):
		return -1
	case a > b:
		return 1
	case a < b:
		return -1
	default:
		return 0
	}
}

func compareVersionChunks(a, b string) int {
	for a != "" && b != "" {
		aChunk, aNumeric, nextA := nextVersionChunk(a)
		bChunk, bNumeric, nextB := nextVersionChunk(b)

		var cmp int
		switch {
		case aNumeric && bNumeric:
			cmp = compareNumericIdentifier(aChunk, bChunk)
		case aNumeric != bNumeric:
			cmp = strings.Compare(aChunk, bChunk)
		default:
			cmp = strings.Compare(aChunk, bChunk)
		}
		if cmp != 0 {
			return normalizeCompareResult(cmp)
		}

		a = nextA
		b = nextB
	}

	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return -1
	default:
		return 1
	}
}

func nextVersionChunk(value string) (chunk string, numeric bool, rest string) {
	if value == "" {
		return "", false, ""
	}

	numeric = value[0] >= '0' && value[0] <= '9'
	end := 1
	for end < len(value) {
		isDigit := value[end] >= '0' && value[end] <= '9'
		if isDigit != numeric {
			break
		}
		end++
	}

	return value[:end], numeric, value[end:]
}

func normalizeCompareResult(cmp int) int {
	switch {
	case cmp > 0:
		return 1
	case cmp < 0:
		return -1
	default:
		return 0
	}
}
