package connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestWaitPulseJitter_StaysWithinTheSpread(t *testing.T) {
	prev := pulseSpread
	pulseSpread = 40 * time.Millisecond
	defer func() { pulseSpread = prev }()
	for i := 0; i < 20; i++ {
		start := time.Now()
		if !waitPulseJitter(context.Background()) {
			t.Fatal("an uncancelled wait must complete")
		}
		if d := time.Since(start); d > 200*time.Millisecond {
			t.Fatalf("waited %s, the spread is 40ms", d)
		}
	}
}

func TestWaitPulseJitter_CancelsPromptly(t *testing.T) {
	prevS, prevJ := pulseSpread, pulseJitter
	pulseSpread = time.Hour
	pulseJitter = func(max time.Duration) time.Duration { return time.Hour }
	defer func() { pulseSpread, pulseJitter = prevS, prevJ }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- waitPulseJitter(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("a cancelled wait must report it was cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("a cancelled transport waited out the jitter")
	}
}

// The hourly pulse wakes every transport that is backing off. Waking them all in
// the same instant is a herd, so each waits a random slice of the spread first.
func TestRunH1_PulseWakesATransportAfterAJitter(t *testing.T) {
	prevS, prevJ := pulseSpread, pulseJitter
	pulseSpread = time.Second
	pulseJitter = func(max time.Duration) time.Duration { return 800 * time.Millisecond }
	defer func() { pulseSpread, pulseJitter = prevS, prevJ }()

	// a server that refuses every upgrade and records when it was asked
	var mu sync.Mutex
	var hits []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, time.Now())
		mu.Unlock()
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	lastHit := func() (time.Time, int) {
		mu.Lock()
		defer mu.Unlock()
		if len(hits) == 0 {
			return time.Time{}, 0
		}
		return hits[len(hits)-1], len(hits)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings := DefaultPlatformTransportSettings()
	settings.ReconnectTimeout = 30 * time.Second // a long backoff, so only the pulse can wake it
	transport := newFlapTestTransport(t, ctx, server.URL, settings)
	defer transport.Close()

	// wait until the first attempt has finished: the server goes quiet
	deadline := time.After(10 * time.Second)
	for {
		at, n := lastHit()
		if 0 < n && 700*time.Millisecond < time.Since(at) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the first attempt never finished")
		case <-time.After(20 * time.Millisecond):
		}
	}
	_, quiet := lastHit()

	pulsedAt := time.Now()
	TriggerPulse()
	for {
		if _, n := lastHit(); quiet < n {
			break
		}
		select {
		case <-time.After(5 * time.Second):
			t.Fatal("the pulse never woke the transport")
		case <-time.After(10 * time.Millisecond):
		}
	}
	at, _ := lastHit()
	if d := at.Sub(pulsedAt); d < 700*time.Millisecond {
		t.Fatalf("redialed %s after the pulse; the jitter was skipped", d)
	}
}
