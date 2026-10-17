package connect

import (
	"encoding/json"
	"strings"
	"testing"
)

// the challenge route answers refusals as HTTP 200 with {"error":{...}} and
// no challenge fields. The client must keep the message instead of decoding
// an empty challenge.

func TestAuthWalletChallengeResultKeepsServerRefusal(t *testing.T) {
	var refused AuthWalletChallengeResult
	if err := json.Unmarshal([]byte(`{"error":{"message":"Unknown wallet address."}}`), &refused); err != nil {
		t.Fatal(err)
	}
	if refused.Error == nil || refused.Error.Message != "Unknown wallet address." {
		t.Fatalf("refusal message was dropped: %+v", refused.Error)
	}
	if refused.MessageTemplate != "" || refused.Challenge != "" {
		t.Fatalf("a refusal must carry no challenge: %+v", refused)
	}
}

func TestAuthWalletChallengeResultDecodesTheChallenge(t *testing.T) {
	var result AuthWalletChallengeResult
	body := `{"challenge":"abc_DEF-123","timestamp":1760000000,"expires_in":300,` +
		`"message_template":"Sign in to URnetwork\nChallenge: abc_DEF-123\nTimestamp: 1760000000"}`
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.Challenge != "abc_DEF-123" || result.ExpiresIn != 300 || result.Timestamp != 1760000000 {
		t.Fatalf("challenge fields dropped: %+v", result)
	}
	if !strings.Contains(result.MessageTemplate, "Challenge: abc_DEF-123") {
		t.Fatalf("message template lost its challenge: %q", result.MessageTemplate)
	}
}
