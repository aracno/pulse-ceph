package proxmox

import (
	"math"
	"strconv"
)

func intFromInt64Checked(v int64) (int, bool) {
	if strconv.IntSize == 32 {
		if v > math.MaxInt32 || v < math.MinInt32 {
			return 0, false
		}
		return int(int32(v)), true
	}
	return int(v), true
}

func intFromUint64Checked(v uint64) (int, bool) {
	if strconv.IntSize == 32 {
		if v > math.MaxInt32 {
			return 0, false
		}
		return int(int32(v)), true
	}
	if v > math.MaxInt64 {
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
