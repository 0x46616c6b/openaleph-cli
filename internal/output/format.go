package output

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ProgressBar renders a bar of width cells followed by the percentage.
func ProgressBar(ratio float64, width int) string {
	ratio = min(max(ratio, 0), 1)
	filled := int(ratio*float64(width) + 0.5)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + fmt.Sprintf(" %3.0f%%", ratio*100)
}

var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// Duration renders an ISO 8601 duration such as PT5H12M42.8S as "5h12m".
// Unparsable values are returned unchanged, zero durations as "-".
func Duration(iso string) string {
	m := isoDuration.FindStringSubmatch(iso)
	if m == nil {
		return iso
	}
	num := func(s string) int {
		f, _ := strconv.ParseFloat(s, 64)
		return int(f)
	}
	days, hours, mins, secs := num(m[1]), num(m[2]), num(m[3]), num(m[4])
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if mins > 0 && days == 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	if len(parts) == 0 {
		if secs > 0 || mins > 0 {
			return fmt.Sprintf("%dm%ds", mins, secs)
		}
		return "-"
	}
	return strings.Join(parts, "")
}
