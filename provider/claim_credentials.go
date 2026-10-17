// The queue owns its API until shutdown. Credentials control new proof reads;
// reconciliation of retained signed transactions never depends on this owner.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urfoundation/sn/ss58"
	"github.com/urnetwork/connect"
)

// claimCredentialSelection selects the one retained provider credential. An
// explicit legacy operation may use a network token; provider operations use
// the selected file or the direct provider's original .provider.jwt. Ported
// from the sn miner's sn_credentials.go selection.
type claimCredentialSelection struct {
	original      string
	jwtFile       string
	legacyNetwork bool
}

// The selected original path also names the command's consent journal.
func claimCredentialPath(selection claimCredentialSelection) (string, error) {
	path := selection.jwtFile
	if path == "" {
		name := ".provider.jwt"
		if selection.legacyNetwork {
			name = "jwt"
		}
		var err error
		path, err = providerStatePath(name)
		if err != nil {
			return "", err
		}
	}
	return path, nil
}

// Parsing only selects local wiring. The API verifies the token signature and
// authorization; this function never turns unverified claims into authority.
func readClaimDaemonCredential(selection claimCredentialSelection) (string, error) {
	path, err := claimCredentialPath(selection)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("claim credential not ready at %s: %w", path, err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("claim credential not ready: %s is empty", path)
	}
	if selection.legacyNetwork {
		claims := gojwt.MapClaims{}
		if _, _, err := gojwt.NewParser().ParseUnverified(token, claims); err != nil {
			return "", fmt.Errorf("legacy network credential: %w", err)
		}
		if value, present := claims["client_id"]; present && value != nil {
			return "", errors.New("legacy network operation requires an explicit network credential")
		}
		return token, nil
	}
	if _, err := claimCredentialClientId(token); err != nil {
		return "", fmt.Errorf("provider credential has no usable client identity: %w", errors.Join(err, errors.New("provider claim is not ready")))
	}
	if selection.original != "" {
		if err := validateRefreshedClaimCredential(selection.original, token); err != nil {
			return "", err
		}
	}
	return token, nil
}

// claimCredentialClientId extracts the client identity for local wiring only.
// Ported from clientauth.ClientIdFromJwt without the sdk dependency.
func claimCredentialClientId(token string) (connect.Id, error) {
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(token, claims); err != nil {
		return connect.Id{}, err
	}
	clientId, ok := claims["client_id"].(string)
	if !ok || clientId == "" {
		return connect.Id{}, errors.New("client JWT has no client_id")
	}
	deviceId, ok := claims["device_id"].(string)
	if !ok || deviceId == "" {
		return connect.Id{}, errors.New("client JWT has no device_id")
	}
	if !claimCredentialIdentityId(clientId) || !claimCredentialIdentityId(deviceId) {
		return connect.Id{}, errors.New("client JWT names an unusable identity")
	}
	return connect.ParseId(clientId)
}

func claimCredentialIdentityId(value string) bool {
	id, err := connect.ParseId(value)
	return err == nil && id != (connect.Id{}) && id.String() == value
}

// Stable principal fields exclude token timestamps, bearer bytes and builds.
type claimCredentialPrincipal struct {
	NetworkId string
	UserId    string
	Roles     []string
	Principal string
}

// Known client/device identity must survive every credential rotation.
type claimCredentialIdentity struct {
	claimCredentialPrincipal
	clientId string
	deviceId string
}

// claimCredentialIdentityOf parses only local ownership fields; it never turns
// unverified claims into server authority. Ported from clientauth's
// registrationIdentity.
func claimCredentialIdentityOf(token string) (claimCredentialIdentity, error) {
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(token, claims); err != nil {
		return claimCredentialIdentity{}, errors.New("claim credential is malformed")
	}
	var identity claimCredentialIdentity
	for _, field := range []struct {
		name  string
		value *string
	}{
		{name: "network_id", value: &identity.NetworkId}, {name: "user_id", value: &identity.UserId},
		{name: "principal", value: &identity.Principal}, {name: "client_id", value: &identity.clientId}, {name: "device_id", value: &identity.deviceId},
	} {
		if raw, exists := claims[field.name]; exists {
			value, ok := raw.(string)
			if !ok {
				return claimCredentialIdentity{}, errors.New("claim credential identity claim has invalid type")
			}
			*field.value = value
		}
	}
	if raw, exists := claims["roles"]; exists && raw != nil {
		roles, ok := raw.([]any)
		if !ok {
			return claimCredentialIdentity{}, errors.New("claim credential roles have invalid type")
		}
		for _, raw := range roles {
			role, ok := raw.(string)
			if !ok || role == "" {
				return claimCredentialIdentity{}, errors.New("claim credential role is invalid")
			}
			identity.Roles = append(identity.Roles, role)
		}
		slices.Sort(identity.Roles)
		identity.Roles = slices.Compact(identity.Roles)
	}
	return identity, nil
}

// This equality does not grant server authority; it prevents a completed
// response from silently switching the durable process to another identity.
func (self claimCredentialPrincipal) equal(other claimCredentialPrincipal) bool {
	return self.NetworkId == other.NetworkId && self.UserId == other.UserId && self.Principal == other.Principal && slices.Equal(self.Roles, other.Roles)
}

// validateRefreshedClaimCredential is the port of clientauth's
// ValidateRefreshedClientJwt: a renewed or re-read disk token must name the
// same client, device and principal it named before.
func validateRefreshedClaimCredential(original, refreshed string) error {
	before, beforeErr := claimCredentialIdentityOf(original)
	after, afterErr := claimCredentialIdentityOf(refreshed)
	if beforeErr != nil || afterErr != nil || !claimCredentialIdentityId(before.clientId) || !claimCredentialIdentityId(before.deviceId) || before.clientId != after.clientId || before.deviceId != after.deviceId || !before.claimCredentialPrincipal.equal(after.claimCredentialPrincipal) {
		return errors.New("refreshed claim credential changed its original identity")
	}
	return nil
}

// Called serially by the queue. Full identity validation precedes publication;
// disk remains provider-owned. The upstream sdk owner refreshed the client jwt
// through the platform on every read; this provider has no per-client refresh
// route (credential renewal is owned by the provider identity store), so this
// owner re-reads the retained file, refuses an identity change and never mints
// a replacement credential.
type claimCredentialApi struct {
	strategy      *connect.ClientStrategy
	apiUrl        string
	selection     claimCredentialSelection
	legacyColdkey string
	diskToken     string
	original      string
	onReady       func()
	ready         bool
}

// An unchanged disk token cannot overwrite a newer in-memory token. A changed
// disk token must still name the original provider and principal.
func (self *claimCredentialApi) reload() (string, error) {
	selection := self.selection
	selection.original = self.original
	token, err := readClaimDaemonCredential(selection)
	if err != nil {
		return "", err
	}
	if self.original == "" && !selection.legacyNetwork {
		if err := validateRefreshedClaimCredential(token, token); err != nil {
			return "", err
		}
		self.original = token
	}
	if token == "" {
		return "", errors.New("provider claim credential is not ready after authentication rejection")
	}
	self.diskToken = token
	if !self.ready {
		self.ready = true
		if self.onReady != nil {
			self.onReady()
		}
	}
	return token, nil
}

// The retained read owner passes its finite request context. The connect api
// binds the request context when it is constructed, so each read builds its
// own api over the shared strategy: that is what carries the read owner's
// deadline and cancellation into the actual HTTP request.
func (self *claimCredentialApi) SnPoolClaimSyncWithContext(ctx context.Context, args *connect.SnPoolClaimArgs) (*connect.SnPoolClaimResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	token, err := self.reload()
	if err != nil {
		return nil, err
	}
	api := connect.NewBringYourApi(ctx, self.strategy, self.apiUrl)
	api.SetByJwt(token)
	defer api.Close()
	selected := *args
	selected.LegacyColdkey = self.legacyColdkey
	result, err := api.SnPoolClaimSync(&selected)
	if err == nil && result != nil && result.Error == nil && len(result.NoId) != 0 && self.legacyColdkey != "" {
		coldkey, decodeErr := ss58.DecodeWithPrefix(self.legacyColdkey, ss58.BittensorPrefix)
		if decodeErr != nil || !bytes.Equal(coldkey[:], result.Coldkey) {
			return nil, errors.New("legacy claim response changed the selected original coldkey")
		}
	}
	return result, err
}

// Tests observe only a completed durable checkpoint, never replace a save or
// supply a claim/reconciliation verdict. The queue invokes this synchronously.
type claimDaemonCheckpointHooks struct{ afterCheckpoint func(*ClaimQueue) }
type claimDaemonCheckpointHooksKey struct{}
