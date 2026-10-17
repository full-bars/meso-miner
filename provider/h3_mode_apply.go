package main

import (
	"sync/atomic"
	"time"

	"github.com/urnetwork/connect"
)

// h3AppliedSetting is the setting last applied through the h3 control key, nil
// before the first application (the startup replay), which must not restart
// anything.
var h3AppliedSetting atomic.Pointer[h3Setting]

// activeProxyReloader is the running provider's reloader, for the few control
// changes that must relaunch the direct identity.
var activeProxyReloader atomic.Pointer[ProxyReloader]

// h3DefaultModeName is the mode a cleared h3 key returns to: what a restart
// would use, so the live state matches the persisted one.
func h3DefaultModeName() string {
	if h3Enabled() {
		return h3ModeOnName
	}
	return h3ModeDirectName
}

// applyH3Mode applies an h3 setting to the running provider. The runtime gate
// follows it at once. The transport mode policy is fixed when an identity builds
// its transport, so a change relaunches the direct identity, and relaunches the
// proxies whose eligibility changed (relaunchH3ProxiesNow), to pick up the new
// policy.
func applyH3Mode(setting h3Setting) (previous h3Setting, relaunch bool) {
	connect.SetH3Enabled(setting.Mode == h3ModeOn)
	prev := h3AppliedSetting.Swap(&setting)
	rerankH3(setting)
	if prev == nil {
		return setting, false
	}
	return *prev, prev.directPolicy() != setting.directPolicy()
}

// restartDirect relaunches the native [direct] identity so it rebuilds its
// platform transport under the current h3 mode. The reload pass starts it again
// once its goroutine has gone. Hot restart keeps its client credential, so this
// does not mint a new identity.
func (r *ProxyReloader) restartDirect() {
	r.cancelMapMu.Lock()
	cancel, running := r.cancelMap[directProxyKey]
	r.cancelMapMu.Unlock()
	if !running {
		return
	}
	r.mu.Lock()
	done := r.directDone
	r.mu.Unlock()
	cancel()
	if done != nil {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			tlog("[h3] warn: the direct identity did not stop within 10s, relaunching anyway\n")
		}
	}
	triggerProxyReload()
}

func restartDirectIdentity() {
	if r := activeProxyReloader.Load(); r != nil {
		r.restartDirect()
	}
}
