package main

import (
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

func TestApplyProviderDialBackoff(t *testing.T) {
	t.Setenv("URNETWORK_DIAL_LONG_BACKOFF", "")
	s := connect.DefaultPlatformTransportSettings()
	if s.ReconnectLongAfter != 0 {
		t.Fatal("the library default must keep the long tier off")
	}
	applyProviderDialBackoff(s)
	if s.ReconnectLongAfter != 10 || s.ReconnectLongMaxTimeout != 10*time.Minute {
		t.Fatalf("provider long tier: %d after, %v max", s.ReconnectLongAfter, s.ReconnectLongMaxTimeout)
	}

	t.Setenv("URNETWORK_DIAL_LONG_BACKOFF", " OFF ")
	off := connect.DefaultPlatformTransportSettings()
	applyProviderDialBackoff(off)
	if off.ReconnectLongAfter != 0 || off.ReconnectLongMaxTimeout != 0 {
		t.Fatal("URNETWORK_DIAL_LONG_BACKOFF=off must restore the single cap")
	}
}
