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
			s.Proxies.UDP = &SnapshotProxyUDP{OK: 31, NoRelay: 3, Mismatch: 1, Unknown: 2, Unchecked: 23}
			m.apply(0, s, nil)
			got, _ := showOnSim(t, m, c.w, c.h)
			for _, want := range []string{"udp ", "31 ok", "unchecked", "83% of checked, 51% of online"} {
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

// A narrow panel keeps the ok and unchecked counts with their labels. Larger
// optional states give way first.
func TestTopUDPCoverageKeepsUncheckedInNarrowPanels(t *testing.T) {
	m := newTestModel(tui.DefaultTheme(), topProviders(1), &fakeClock{t: topBase})
	s := topFlowing(t)
	s.Proxies.UDP = &SnapshotProxyUDP{OK: 4000, NoRelay: 1000, Mismatch: 1000, Unknown: 1000, Unchecked: 1000}
	m.apply(0, s, nil)
	for _, w := range []int{80, 60, 48, 40, 30} {
		buf := tui.New(w, 4)
		m.drawUDPProxyCoverage(buf, 0)
		line := strings.SplitN(buf.String(), "\n", 2)[0]
		for _, want := range []string{"4000 ok", "1000 unchecked"} {
			if !strings.Contains(line, want) {
				t.Fatalf("width %d: %q missing from %q", w, want, line)
			}
		}
	}
	// With room for everything, all five states are drawn.
	buf := tui.New(120, 4)
	m.drawUDPProxyCoverage(buf, 0)
	line := strings.SplitN(buf.String(), "\n", 2)[0]
	for _, want := range []string{"4000 ok", "1000 no relay", "1000 mismatch", "1000 unknown", "1000 unchecked"} {
		if !strings.Contains(line, want) {
			t.Fatalf("wide panel: %q missing from %q", want, line)
		}
	}
}

// With no ok segment the first optional state is index zero. It must be
// droppable so the unchecked count (required) stays visible in a narrow panel.
func TestTopUDPCoverageDropsFirstOptionalStateWithoutOK(t *testing.T) {
	m := newTestModel(tui.DefaultTheme(), topProviders(1), &fakeClock{t: topBase})
	s := topFlowing(t)
	s.Proxies.UDP = &SnapshotProxyUDP{NoRelay: 100, Unchecked: 100}
	m.apply(0, s, nil)
	buf := tui.New(24, 4)
	m.drawUDPProxyCoverage(buf, 0)
	line := strings.SplitN(buf.String(), "\n", 2)[0]
	if !strings.Contains(line, "100 unchecked") {
		t.Fatalf("unchecked label clipped: %q", line)
	}
}
