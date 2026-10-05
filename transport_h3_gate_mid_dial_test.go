package connect

import (
	"context"
	"testing"
	"time"
)

// The runtime gate can be switched off while a dial is in flight. The connection
// closes itself when it notices, and the transport must not charge that to the
// backend: no connect, no drop. It must also stop dialing while the gate is off
// rather than spin, and still exit cleanly when its context is cancelled.
func TestH3GateDisabledWhileDialInFlight(t *testing.T) {
	switchH3Gate(t, true)

	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()

	// The server holds the reply to the auth frame, so the dial is parked
	// between "auth sent" and "auth answered" while the test flips the gate.
	authRead := make(chan struct{}, 1)
	releaseAuth := make(chan struct{})
	server := startH3TestServer(t, ctx, true, nil)
	server.beforeAuthReply = func() {
		authRead <- struct{}{}
		<-releaseAuth
	}

	before := TransportModeStats()
	transportCtx, transportCancel := context.WithCancel(ctx)
	transport := newH3DatagramTestTransport(transportCtx, transportCancel, server.port)
	done := make(chan struct{})
	go func() {
		defer close(done)
		transport.runH3(TransportModeH3, 0, 1)
	}()
	defer func() {
		transportCancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("runH3 did not exit after its context was cancelled while the gate was off")
		}
	}()

	select {
	case <-authRead:
	case <-time.After(10 * time.Second):
		t.Fatal("the dial never reached the auth reply")
	}

	// switched off while the dial is in flight, then the reply lands
	SetH3Enabled(false)
	close(releaseAuth)

	// The connection is expected to notice the gate and close itself. Whatever it
	// does, it must not be recorded as a connect or as a drop.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if stats := TransportModeStats(); stats.H3Connects != before.H3Connects || stats.H3Drops != before.H3Drops {
			t.Fatalf("a gate-off mid-dial was counted: connects %d -> %d, drops %d -> %d",
				before.H3Connects, stats.H3Connects, before.H3Drops, stats.H3Drops)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// And with the gate off it must wait, not dial again.
	attempts := TransportModeStats().H3Attempts
	time.Sleep(600 * time.Millisecond)
	if got := TransportModeStats().H3Attempts; got != attempts {
		t.Fatalf("the transport kept dialing with the gate off: attempts %d -> %d", attempts, got)
	}
}
