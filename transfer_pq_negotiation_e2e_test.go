package connect

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"
	"time"

	"github.com/urnetwork/connect/protocol"
)

// The provider side of a real two-client loopback transfer negotiates the
// hybrid post-quantum group and seals its session. This is the post-quantum
// half of the platform capability probe's expectations (its recorder reads
// the same session facts - negotiated group, sealed - from the client side),
// run against the fork's own transport rather than the server harness.
//
// The harness is the peer-identity one (auto-granting oob + data gateway
// transports): the cipher is a binary property that completes only after the
// peer identity proof verifies, so the sealing assertion needs the
// establishing harness, not just traffic.
func TestProviderSidePqNegotiationEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}
	t.Run("opportunistic negotiates X25519MLKEM768 and seals", func(t *testing.T) {
		runProviderPqNegotiationCase(t, EncryptionModeOpportunistic, true)
	})
	t.Run("encryption off leaves no e2e session", func(t *testing.T) {
		runProviderPqNegotiationCase(t, EncryptionModeOff, false)
	})
}

func runProviderPqNegotiationCase(t *testing.T, mode EncryptionMode, expectSession bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	aClientId := NewId()
	bClientId := NewId()

	aSend := make(chan []byte)
	bSend := make(chan []byte)
	aConditioner, bReceive := newConditioner(ctx, aSend)
	bConditioner, aReceive := newConditioner(ctx, bSend)
	aConditioner.update(func() { aConditioner.randomDelay = 20 * time.Millisecond })
	bConditioner.update(func() { bConditioner.randomDelay = 20 * time.Millisecond })

	aSendTransport := newDataGatewayTransport()
	aReceiveTransport := NewReceiveGatewayTransport()
	bSendTransport := newDataGatewayTransport()
	bReceiveTransport := NewReceiveGatewayTransport()

	provideModes := map[protocol.ProvideMode]bool{protocol.ProvideMode_Network: true}

	makeSettings := func() *ClientSettings {
		settings := DefaultClientSettings()
		settings.SendBufferSettings.SequenceBufferSize = 0
		settings.SendBufferSettings.AckBufferSize = 0
		settings.SendBufferSettings.AckTimeout = 60 * time.Second
		settings.SendBufferSettings.IdleTimeout = 1 * time.Second
		settings.SendBufferSettings.MinResendInterval = 10 * time.Millisecond
		settings.ReceiveBufferSettings.SequenceBufferSize = 0
		settings.ReceiveBufferSettings.GapTimeout = 60 * time.Second
		settings.ReceiveBufferSettings.IdleTimeout = 60 * time.Second
		settings.ForwardBufferSettings.SequenceBufferSize = 0
		settings.ForwardBufferSettings.IdleTimeout = 1 * time.Second
		settings.EncryptionSettings.Mode = mode
		settings.EncryptionSettings.TlsTimeout = 30 * time.Second
		settings.EncryptionSettings.EncryptionControlUseCompanion = false
		return settings
	}

	var a, b *Client
	aOob := &grantingClientOob{
		sourceId: aClientId,
		settings: DefaultContractManagerSettings(),
		destSecretKey: func(destinationId Id) ([]byte, bool) {
			return b.ContractManager().GetProvideSecretKey(protocol.ProvideMode_Network)
		},
		destClientPublicKey: func(destinationId Id) []byte {
			return b.ClientKeyManager().PublicKey()
		},
	}
	bOob := &grantingClientOob{
		sourceId: bClientId,
		settings: DefaultContractManagerSettings(),
		destSecretKey: func(destinationId Id) ([]byte, bool) {
			return a.ContractManager().GetProvideSecretKey(protocol.ProvideMode_Network)
		},
		destClientPublicKey: func(destinationId Id) []byte {
			return a.ClientKeyManager().PublicKey()
		},
	}

	a = NewClient(ctx, aClientId, aOob, makeSettings())
	defer a.Cancel()
	a.RouteManager().UpdateTransport(aSendTransport, []Route{aSend})
	a.RouteManager().UpdateTransport(aReceiveTransport, []Route{aReceive})
	blackholeControlId(ctx, a.RouteManager())
	a.ContractManager().SetProvideModes(provideModes)

	b = NewClient(ctx, bClientId, bOob, makeSettings())
	defer b.Cancel()
	b.RouteManager().UpdateTransport(bSendTransport, []Route{bSend})
	b.RouteManager().UpdateTransport(bReceiveTransport, []Route{bReceive})
	blackholeControlId(ctx, b.RouteManager())
	b.ContractManager().SetProvideModes(provideModes)

	receives := make(chan string, 64)
	b.AddReceiveCallback(func(source TransferPath, frames []*protocol.Frame, peer Peer) {
		for _, frame := range frames {
			if message, err := FromFrame(frame); err == nil {
				if v, ok := message.(*protocol.SimpleMessage); ok {
					select {
					case receives <- v.Content:
					default:
					}
				}
			}
		}
	})

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for i := 0; ; i += 1 {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			default:
			}
			message := &protocol.SimpleMessage{Content: fmt.Sprintf("pq %d", i)}
			frame, err := ToFrame(message, DefaultProtocolVersion)
			if err != nil {
				continue
			}
			a.Send(frame, DestinationId(bClientId), func(error) {})
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()

	// traffic must flow in both modes; the receive proves the loopback path
	select {
	case <-receives:
	case <-time.After(45 * time.Second):
		t.Fatal("no message arrived over the loopback transfer")
	}

	// the provider side's session state: B answered A's ClientHello
	manager := b.EncryptionSessionManager()
	deadline := time.Now().Add(45 * time.Second)
	// the off case cannot take a session; a short window with traffic
	// flowing is the proof (a session would appear in about a second)
	negativeDeadline := time.Now().Add(5 * time.Second)
	for {
		sessions := manager.sessionsForPeer(aClientId)
		hybrid := false
		sealed := false
		for _, session := range sessions {
			if pqeCurveOf(session.currentEpoch()) == tls.X25519MLKEM768 {
				hybrid = true
				if session.Cipher() != nil {
					sealed = true
				}
			}
		}
		if expectSession {
			if hybrid && sealed {
				// the provider's own tracker agrees: an e2e session opened in
				// the post-quantum family
				if pqe := manager.PQECounts(); pqe.ActivePQE < 1 || pqe.PQELifetime < 1 {
					t.Fatalf("the e2e session opened but the provider tracker missed it, pqe=%+v", pqe)
				}
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("the provider side must negotiate the hybrid group over the loopback transfer and seal it: sessions=%d hybrid=%t sealed=%t pqe=%+v",
					len(sessions), hybrid, sealed, manager.PQECounts())
			}
		} else {
			if len(sessions) > 0 {
				t.Fatalf("encryption off must leave no e2e session, got %d", len(sessions))
			}
			if time.Now().After(negativeDeadline) {
				// the whole window passed with traffic flowing and no session
				if pqe := manager.PQECounts(); pqe.ActivePQE != 0 || pqe.ActiveClas != 0 || pqe.PQELifetime != 0 || pqe.ClasLifetime != 0 {
					t.Fatalf("encryption off must leave no e2e session, pqe=%+v", pqe)
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}
