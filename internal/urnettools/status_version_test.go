package urnettools

import "testing"

func TestCompareStatusVersionTags(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"v2026.11.9-10-meso", "v2026.11.10-2-meso", -1},
		{"v2026.11.10-10-meso", "v2026.11.10-2-meso", 1},
		{" V2026.11.10 ", "v2026.11.10", 0},
		{"1.2-rc", "1.2-1", -1},
		{"1.2", "1.2.1", -1},
	} {
		if got := compareVersionTags(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersionTags(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
