package urnettools

import (
	"strconv"
	"strings"
)

func compareVersionTags(v1, v2 string) int {
	v1 = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v1), "v"), "V")
	v2 = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v2), "v"), "V")
	parts1 := strings.FieldsFunc(v1, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	parts2 := strings.FieldsFunc(v2, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		p1, p2 := parts1[i], parts2[i]
		n1, e1 := strconv.Atoi(p1)
		n2, e2 := strconv.Atoi(p2)
		switch {
		case e1 == nil && e2 == nil:
			if n1 < n2 {
				return -1
			}
			if n1 > n2 {
				return 1
			}
		case e1 == nil:
			return 1
		case e2 == nil:
			return -1
		default:
			if p1 < p2 {
				return -1
			}
			if p1 > p2 {
				return 1
			}
		}
	}
	if len(parts1) < len(parts2) {
		return -1
	}
	if len(parts1) > len(parts2) {
		return 1
	}
	return 0
}
