package connect

import (
	"sync"
	"testing"
	"time"

	"github.com/go-playground/assert/v2"
)

func TestConnectHost(t *testing.T) {

	host, err := connectHost("http://connect.foo.bar")
	assert.Equal(t, err, nil)
	assert.Equal(t, host, "connect.foo.bar")

	host, err = connectHost("https://other-connect.bar.com")
	assert.Equal(t, err, nil)
	assert.Equal(t, host, "other-connect.bar.com")
}

func TestSetModeAvailableWakesWaiter(t *testing.T) {
	transport := &PlatformTransport{
		availableModeMonitor: NewMonitor(),
		availableModes:       map[TransportMode]bool{},
	}

	available, notify := transport.modesAvailable()
	assert.Equal(t, false, available[TransportModeH1])

	woke := make(chan struct{})
	go func() {
		<-notify
		close(woke)
	}()

	transport.setModeAvailable(TransportModeH1, true)

	select {
	case <-woke:
	case <-time.After(1 * time.Second):
		t.Fatal("waiter was not notified after setModeAvailable")
	}
}

func TestSetModeAvailableNoSpuriousWake(t *testing.T) {
	transport := &PlatformTransport{
		availableModeMonitor: NewMonitor(),
		availableModes:       map[TransportMode]bool{},
	}

	// Set mode to true — should notify.
	transport.setModeAvailable(TransportModeH1, true)

	// Capture watcher on a channel returned BEFORE the second set.
	_, notify := transport.modesAvailable()

	// Set the mode to the same value — should NOT notify.
	transport.setModeAvailable(TransportModeH1, true)

	select {
	case <-notify:
		t.Fatal("NotifyAll fired when mode availability did not change")
	case <-time.After(200 * time.Millisecond):
		// Expected: channel not closed.
	}
}

func TestSetActiveModeNoSpuriousWake(t *testing.T) {
	transport := &PlatformTransport{
		availableModeMonitor: NewMonitor(),
		availableModes:       map[TransportMode]bool{},
		mode:                 NewMonitorValue[TransportMode](TransportModeNone),
	}

	// Set active mode to H1 — should notify.
	transport.setActiveMode(TransportModeH1)

	// Capture watcher on a channel returned BEFORE the second set.
	_, notify := transport.activeMode()

	// Set the active mode to the same value — should NOT notify (MonitorValue
	// handles this internally).
	transport.setActiveMode(TransportModeH1)

	select {
	case <-notify:
		t.Fatal("NotifyAll fired when active mode did not change")
	case <-time.After(200 * time.Millisecond):
		// Expected: channel not closed.
	}

	// Changing the mode to a different value SHOULD still notify.
	transport.setActiveMode(TransportModeNone)

	select {
	case <-notify:
		// Expected.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("NotifyAll did not fire on real active mode change")
	}
}

func TestPlatformTransportSetAuthSnapshotRoundTrip(t *testing.T) {
	transport := &PlatformTransport{}

	auth1 := &ClientAuth{ByJwt: "jwt-1", AppVersion: "1.0.0", InstanceId: NewId()}
	transport.SetAuth(auth1)
	if got := transport.authSnapshot(); got.ByJwt != "jwt-1" || got.AppVersion != "1.0.0" || got.InstanceId != auth1.InstanceId {
		t.Fatalf("authSnapshot = %v, want the credential set by SetAuth (%v)", got, auth1)
	}

	auth2 := &ClientAuth{ByJwt: "jwt-2", AppVersion: "1.0.1", InstanceId: NewId()}
	transport.SetAuth(auth2)
	if got := transport.authSnapshot(); got.ByJwt != "jwt-2" || got.AppVersion != "1.0.1" || got.InstanceId != auth2.InstanceId {
		t.Fatalf("authSnapshot after second SetAuth = %v, want %v", got, auth2)
	}
	// the transport owns a copy: mutating the caller's value changes nothing
	auth2.ByJwt = "mutated"
	if got := transport.authSnapshot(); got.ByJwt != "jwt-2" {
		t.Fatalf("authSnapshot followed a mutation of the caller's auth: %q", got.ByJwt)
	}
}

// TestPlatformTransportSetAuthConcurrentWithSnapshot exercises SetAuth (the
// renewal watcher rotating the bearer token) racing with getAuth (the dial
// paths reading it) — this is the scenario atomic.Pointer replaced the old
// stateLock-guarded field for. Run with -race to catch any regression back to
// an unsynchronized read/write.
func TestPlatformTransportSetAuthConcurrentWithSnapshot(t *testing.T) {
	transport := &PlatformTransport{}
	transport.SetAuth(&ClientAuth{ByJwt: "initial"})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					transport.SetAuth(&ClientAuth{ByJwt: "rotated"})
				}
			}
		}(i)
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if auth := transport.authSnapshot(); auth.ByJwt == "" {
						t.Errorf("authSnapshot returned an empty credential after SetAuth had already been called")
						return
					}
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
