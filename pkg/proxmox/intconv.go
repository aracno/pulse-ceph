package proxmox

import "math"

func intFromInt64Checked(v int64) (int, bool) {
	if v > int64(math.MaxInt) || v < int64(math.MinInt) {
		return 0, false
	}
	return int(v), true
}

func intFromUint64Checked(v uint64) (int, bool) {
	if v > uint64(math.MaxInt) {
		return 0, false
	}
	return int(v), true
}

func intFromFloat64RoundedChecked(v float64) (int, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	rounded := math.Round(v)
	if rounded > float64(math.MaxInt) || rounded < float64(math.MinInt) {
		return 0, false
	}
	return int(rounded), true
}

func intFromFloat64TruncChecked(v float64) (int, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	truncated := math.Trunc(v)
	if truncated > float64(math.MaxInt) || truncated < float64(math.MinInt) {
		return 0, false
	}
	return int(truncated), true
}
