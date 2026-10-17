package connect

import (
	"net/netip"
	"time"
)

// completeTestTransport fills the fields NewPlatformTransportWithTargetMode
// initializes, for the tests that build a PlatformTransport literal to drive one
// mode runner (runH3) directly. Fields the literal already set are kept.
func completeTestTransport(transport *PlatformTransport) *PlatformTransport {
	settings := transport.settings
	if settings == nil {
		settings = DefaultPlatformTransportSettings()
		transport.settings = settings
	}
	if transport.log == nil {
		transport.log = loggerOrDefault(settings.Log)
	}
	if transport.receiveStats == nil {
		transport.receiveStats = &PlatformTransportReceiveStats{}
	}
	if transport.framerSettings == nil {
		transport.framerSettings = settings.FramerSettings
	}
	if transport.h3DatagramSettings == nil {
		transport.h3DatagramSettings = DefaultH3DatagramSettings()
	}
	if transport.h3DatagramStats == nil {
		transport.h3DatagramStats = processH3DatagramStats()
	}
	if transport.h3DatagramReassemblyBudget == nil {
		transport.h3DatagramReassemblyBudget = NewH3DatagramReassemblyBudget(
			transport.h3DatagramSettings.ProcessReassemblyByteCount,
		)
	}
	if transport.availableModeMonitor == nil {
		transport.availableModeMonitor = NewMonitor()
	}
	if transport.availableModes == nil {
		transport.availableModes = map[TransportMode]bool{}
	}
	if transport.modePreferences == nil {
		transport.modePreferences = forkModePreferences(settings)
	}
	if transport.mode == nil {
		transport.mode = NewMonitorValue(TransportModeNone)
	}
	if transport.connectedMonitor == nil {
		transport.connectedMonitor = NewMonitor()
	}
	if transport.kickMonitor == nil {
		transport.kickMonitor = NewMonitor()
	}
	if transport.extenderIpCounts == nil {
		transport.extenderIpCounts = map[netip.Addr]int{}
	}
	if transport.extenderIpsMonitor == nil {
		transport.extenderIpsMonitor = NewMonitorValue[uint64](0)
	}
	if transport.clientLimitBackoff == nil {
		transport.clientLimitBackoff = NewClientLimitBackoff()
	}
	if transport.credentialRefusalHold == nil {
		transport.credentialRefusalHold = newCredentialRefusalHold()
	}
	if transport.held == nil {
		transport.enabled.Store(true)
		transport.held = NewMonitorValue(false)
	}
	if transport.pinnedBackoff == nil {
		transport.pinnedBackoff = newPinnedDialBackoff(settings.ReconnectTimeout, 5*time.Minute)
	}
	if transport.failBackoff == nil {
		transport.failBackoff = newDialFailureBackoff(settings.ReconnectTimeout, 60*time.Second)
	}
	if transport.h3Gate == nil {
		transport.h3Gate = newPlatformH3Gate(transport)
	}
	return transport
}
