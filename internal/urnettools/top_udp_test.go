package urnettools

import (
	"strings"
	"testing"

	"github.com/urnetwork/connect/internal/tui"
)

func TestTopUDPCoverage(t *testing.T) {
	cases := []struct {
		name  string
		w, h  int
		theme tui.Theme
	}{
		{"top_udp_120x40", 120, 40, tui.DefaultTheme()},
		{"top_udp_100x30", 100, 30, tui.DefaultTheme()},
		{"top_udp_80x24", 80, 24, tui.DefaultTheme()},
		{"top_udp_mono_80x24", 80, 24, tui.MonoTheme()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newTestModel(c.theme, topProviders(1), &fakeClock{t: topBase})
			s := topFlowing(t)
			s.Proxies.UDP = &SnapshotProxyUDP{OK: 31, NoRelay: 3, Mismatch: 1, Unknown: 2, Unchecked: 17}
			m.apply(0, s, nil)
			got, _ := showOnSim(t, m, c.w, c.h)
			for _, want := range []string{"udp ", "31 ok", "unchecked", "83% of checked, 57% of online"} {
				if !strings.Contains(got, want) {
					t.Fatalf("missing %q:\n%s", want, got)
				}
			}
			assertGolden(t, c.name, got)
		})
	}
}

func TestTopUDPCompactAndBarsLayout(t *testing.T) {
	for _, n := range []int{1, 900} {
		m := newTestModel(tui.DefaultTheme(), topProviders(1), &fakeClock{t: topBase})
		s := topFlowing(t)
		s.Proxies = SnapshotProxies{Up: n, UDP: &SnapshotProxyUDP{OK: n}}
		m.apply(0, s, nil)
		got, _ := showOnSim(t, m, 120, 40)
		if !strings.Contains(got, "udp ") {
			t.Fatalf("pool %d: missing UDP row:\n%s", n, got)
		}
		want := topProxiesRows + 2
		if n == 1 {
			want = topProxiesCompactRows + 1
		}
		if height := m.proxiesRows(); height != want {
			t.Fatalf("pool %d: height = %d, want %d", n, height, want)
		}
	}
}

func TestTopUDPCompactAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		udp  *SnapshotProxyUDP
		want string
	}{
		{"old", nil, ""},
		{"unchecked", &SnapshotProxyUDP{Unchecked: 1}, "1 unchecked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(tui.DefaultTheme(), topProviders(1), &fakeClock{t: topBase})
			s := topFlowing(t)
			s.Proxies = SnapshotProxies{Up: 1, UDP: tc.udp}
			m.apply(0, s, nil)
			got, _ := showOnSim(t, m, 120, 40)
			if tc.udp == nil && strings.Contains(got, "udp ") {
				t.Fatalf("unexpected UDP row:\n%s", got)
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("missing %q:\n%s", tc.want, got)
			}
		})
	}
}
