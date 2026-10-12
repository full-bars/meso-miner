package connect

import (
	"testing"
	"time"
)

// A fresh process restarted into a backend that accepts every websocket and
// drops it within seconds: every connect succeeds, every session dies in under
// 5 s, and no proxy ever becomes stable. The verdict must still see it.
func TestGovernor_FreshProcessIntoAcceptThenDropIsGoverned(t *testing.T) {
	now := time.Unix(1_200_000, 0)
	g := newConnectGovernor(func() time.Time { return now })
	// a host that has run before restores its baseline
	g.baseline = 0.6

	const proxies = 40
	guards := make([]*flapGuard, proxies)
	for i := range guards {
		guards[i] = newFlapGuard(0, time.Second)
		guards[i].jitter = func(d time.Duration) time.Duration { return d }
	}

	degradedAt := time.Duration(-1)
	start := now
	for second := 0; second < 6*60; second++ {
		// a few proxies connect every second; each session lives 3 s
		for k := 0; k < 2; k++ {
			fg := guards[(2*second+k)%proxies]
			g.recordAttempt(connectOK, nil)
			lived := 3 * time.Second
			// what sessionEndWait does
			if fg.isYoung(lived) {
				if fg.everStable {
					g.recordProvenEarlyDeath()
				} else {
					g.recordEarlyDeath()
				}
			}
			fg.sessionEnded(lived)
		}
		// the budget's control step reads the verdict every few seconds
		if second%5 == 0 {
			s := g.snapshot()
			if s.Degraded && degradedAt < 0 {
				degradedAt = now.Sub(start)
			}
		}
		now = now.Add(time.Second)
	}

	for _, fg := range guards {
		if fg.everStable {
			t.Fatal("setup: no session may become stable")
		}
	}
	if degradedAt < 0 {
		t.Fatalf("a fresh process in a pure accept-then-drop backend was never governed: %+v", g.snapshot())
	}
	if degradedAt < govStartupGrace {
		t.Fatalf("degraded at %s, inside the startup grace", degradedAt)
	}
	if limit := govStartupGrace + 3*time.Minute; limit < degradedAt {
		t.Fatalf("degraded only at %s, want by %s", degradedAt, limit)
	}
	s := g.snapshot()
	if s.HardFailures < s.Early-s.EarlyProven {
		t.Fatalf("unproven early deaths must count as hard failures once sustained: %+v", s)
	}
}

func TestGovernor_UnprovenEarlyDeathsNeedAShareTwoMinutesAndABaseline(t *testing.T) {
	type minute struct {
		ok, silent, early int
	}
	cases := []struct {
		name     string
		baseline float64
		minutes  []minute
		want     bool
	}{
		// ratio 0 with 25 platform attempts, so only the hard-failure count decides
		{"35% share for two minutes", 0.6, []minute{{35, 65, 35}, {35, 65, 35}}, true},
		{"25% share is below the bar", 0.6, []minute{{25, 75, 25}, {25, 75, 25}, {25, 75, 25}}, false},
		{"a single minute is a spike", 0.6, []minute{{25, 75, 25}, {35, 65, 35}}, false},
		{"a quiet minute between resets", 0.6, []minute{{35, 65, 35}, {35, 0, 0}, {35, 65, 35}}, false},
		{"no baseline keeps the old behaviour", 0, []minute{{35, 65, 35}, {35, 65, 35}, {35, 65, 35}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, now := newTestGovernor()
			g.baseline = tc.baseline
			var last govSnapshot
			for _, m := range tc.minutes {
				// a fresh unix minute for each batch, then a read inside it
				*now = now.Add(time.Minute)
				record(g, connectOK, m.ok)
				record(g, connectTimeoutNoReply, m.silent)
				for i := 0; i < m.early; i++ {
					g.recordEarlyDeath()
				}
				*now = now.Add(time.Second)
				last = g.snapshot()
			}
			if last.Degraded != tc.want {
				t.Fatalf("degraded = %v, want %v: %+v", last.Degraded, tc.want, last)
			}
		})
	}
}

// The proven path is unchanged: proven early deaths count at once, with no
// streak and no baseline.
func TestGovernor_ProvenEarlyDeathsStillCountAtOnce(t *testing.T) {
	g, _ := newTestGovernor()
	record(g, connectOK, 100)
	for i := 0; i < 95; i++ {
		g.recordProvenEarlyDeath()
	}
	s := g.snapshot()
	if !s.Degraded || s.HardFailures != 95 {
		t.Fatalf("proven early deaths are hard failures immediately: %+v", s)
	}
}

// A host whose poor proxy list normally sees a quarter of its attempts end in an
// early death has learned that as normal: a 35% minute is not an incident for
// it, while the same minute is for a host that normally sees almost none.
func TestGovernor_UnprovenShareIsRelativeToWhatTheHostUsuallySees(t *testing.T) {
	run := func(usualEarly int) bool {
		g, now := newTestGovernor()
		g.baseline = 0.6
		step := func(ok, silent, early int) govSnapshot {
			*now = now.Add(time.Minute)
			record(g, connectOK, ok)
			record(g, connectTimeoutNoReply, silent)
			for i := 0; i < early; i++ {
				g.recordEarlyDeath()
			}
			*now = now.Add(time.Second)
			return g.snapshot()
		}
		for i := 0; i < govUnprovenLearnMinutes+1; i++ {
			step(25, 75, usualEarly) // ordinary minutes
		}
		step(35, 65, 35)
		return step(35, 65, 35).Degraded
	}
	if run(25) {
		t.Fatal("a host that usually sees 25% early deaths was flagged by 35%")
	}
	if !run(2) {
		t.Fatal("a host that usually sees about 2% early deaths was not flagged by 35% for two minutes")
	}
}
