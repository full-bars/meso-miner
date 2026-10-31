package urnettools

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// captureWalletStderr runs fn and returns what it wrote to stderr, so a test
// can prove a warning was actually printed instead of merely not failing.
func captureWalletStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	return <-done
}

// The wallet group: help renders from the cobra tree (never by running the
// tool), the fleet-node refusals guard the seed-bearing forms, and the
// delegation passes the provider's own flags through untouched.

func walletTestProvider() Provider {
	return Provider{
		Unit:    "urnetwork-native.service",
		User:    "urnet",
		Binary:  "/usr/bin/provider",
		Running: true,
	}
}

// walletWorkstationProvider is the non-fleet shape: a provider the operator
// runs from their own home, not a systemd service.
func walletWorkstationProvider() Provider {
	return Provider{
		Unit:    "manual",
		User:    "operator",
		Binary:  "/usr/bin/provider",
		Running: true,
	}
}

// withWalletSeams installs the test seams and restores them on cleanup.
func withWalletSeams(t *testing.T, providers []Provider, recorder *[][]string, scriptPath, readmePath string) {
	t.Helper()
	prevProviders := discoverProvidersForWallet
	prevDocker := discoverDockerForWallet
	prevSub := walletProviderSubcommand
	discoverProvidersForWallet = func() []Provider { return providers }
	discoverDockerForWallet = func() []Provider { return nil }
	if recorder != nil {
		walletProviderSubcommand = func(p Provider, args ...string) error {
			*recorder = append(*recorder, append([]string{}, args...))
			return nil
		}
	}
	t.Cleanup(func() {
		discoverProvidersForWallet = prevProviders
		discoverDockerForWallet = prevDocker
		walletProviderSubcommand = prevSub
	})
}

func TestWalletHelpRendersFromTheTree(t *testing.T) {
	if !strings.Contains(rootHelpMenu, "wallet status") {
		t.Fatal("the root help menu must list the wallet group")
	}
	root := buildRootCmd()
	wallet, _, err := root.Find([]string{"wallet"})
	if err != nil || wallet.Name() != "wallet" {
		t.Fatalf("wallet command not found: %v", err)
	}
	if wallet.Short == "" || wallet.Long == "" || wallet.Example == "" {
		t.Fatalf("wallet group must have Short/Long/Example: short=%q long=%q example=%q", wallet.Short, wallet.Long, wallet.Example)
	}
	for _, name := range []string{"status", "challenge", "set", "sign"} {
		var sub *cobra.Command
		for _, candidate := range wallet.Commands() {
			if candidate.Name() == name {
				sub = candidate
			}
		}
		if sub == nil {
			t.Fatalf("wallet %s missing", name)
		}
		if sub.Short == "" || sub.Long == "" || sub.Example == "" {
			t.Fatalf("wallet %s must have Short/Long/Example: short=%q long=%q example=%q", name, sub.Short, sub.Long, sub.Example)
		}
		if !strings.Contains(sub.Example, "urnet-tools wallet") {
			t.Fatalf("wallet %s Example must show real invocations, got %q", name, sub.Example)
		}
	}
	// the seed-file warning is loud in the Long text
	var setCmd *cobra.Command
	for _, candidate := range wallet.Commands() {
		if candidate.Name() == "set" {
			setCmd = candidate
		}
	}
	if !strings.Contains(setCmd.Long, "OWN WORKSTATION") || !strings.Contains(setCmd.Long, "never a fleet node") {
		t.Fatal("wallet set Long must carry the workstation-only warning")
	}
}

func TestWalletStatusDelegates(t *testing.T) {
	var calls [][]string
	withWalletSeams(t, []Provider{walletTestProvider()}, &calls, "", "")
	if err := cmdWalletStatus([]string{"--unit", "urnetwork-native.service"}); err != nil {
		t.Fatalf("wallet status: %v", err)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "wallet status" {
		t.Fatalf("delegation args = %v, want [wallet status]", calls)
	}
}

func TestWalletChallengeRequiresAColdkey(t *testing.T) {
	var calls [][]string
	withWalletSeams(t, []Provider{walletTestProvider()}, &calls, "", "")
	err := cmdWalletChallenge(nil)
	if err == nil || !strings.Contains(err.Error(), "coldkey") {
		t.Fatalf("wallet challenge with no coldkey must give a helpful error, got %v", err)
	}
	if len(calls) != 0 {
		t.Fatal("nothing may be delegated without a coldkey")
	}
	// with a coldkey the challenge delegates and passes provider flags through
	err = cmdWalletChallenge([]string{"5CvF...ss58", "--wallet-from-epoch", "51", "-f"})
	if err != nil {
		t.Fatalf("wallet challenge: %v", err)
	}
	joined := strings.Join(calls[0], " ")
	if !strings.HasPrefix(joined, "wallet challenge 5CvF...ss58") || !strings.Contains(joined, "--wallet-from-epoch 51") {
		t.Fatalf("delegation args = %q", joined)
	}
}

func TestWalletSetSeedFileWarnsOnProviderBox(t *testing.T) {
	var calls [][]string
	withWalletSeams(t, []Provider{walletTestProvider()}, &calls, "", "")
	// A provider on the box is worth saying out loud, and it is not a reason
	// to stop the operator.
	var err error
	out := captureWalletStderr(t, func() {
		err = cmdWalletSet([]string{"-f", "5CvF...ss58", "--coldkey_seed_file=/tmp/seed"})
	})
	if err != nil {
		t.Fatalf("the seed-file form must warn, not refuse, got %v", err)
	}
	if !strings.Contains(out, "not recommended here") {
		t.Fatalf("the seed-file form must warn on a provider box, got %q", out)
	}
	if len(calls) != 1 {
		t.Fatalf("the warned set must still delegate, got %v", calls)
	}
	// The message/signature form reads no seed, so it earns no warning.
	calls = nil
	out = captureWalletStderr(t, func() {
		err = cmdWalletSet([]string{"-f", "5CvF...ss58", "--message=m", "--signature=0x00"})
	})
	if err != nil {
		t.Fatalf("wallet set signed form: %v", err)
	}
	if strings.Contains(out, "not recommended here") {
		t.Fatalf("the signed form reads no seed and must not warn, got %q", out)
	}
	if len(calls) != 1 {
		t.Fatal("the signed form must delegate")
	}
}

func TestWalletFleetDetectionViaProviderEnv(t *testing.T) {
	var calls [][]string
	// a provider is present, and the runtime environment marker is what makes
	// the box look like a fleet member
	withWalletSeams(t, []Provider{walletTestProvider()}, &calls, "", "")
	t.Setenv("URNETWORK_PROFILE", "lowmem")
	var err error
	out := captureWalletStderr(t, func() {
		err = cmdWalletSet([]string{"-f", "5CvF...ss58", "--coldkey_seed_file=/tmp/seed"})
	})
	if err != nil {
		t.Fatalf("the provider environment marker must warn, not refuse, got %v", err)
	}
	if !strings.Contains(out, "not recommended here") {
		t.Fatalf("the provider environment marker must produce the warning, got %q", out)
	}
	if len(calls) != 1 {
		t.Fatalf("the warned set must still delegate, got %d calls", len(calls))
	}
}

// withWalletStdin feeds one answer to a prompt as if stdin were a terminal.
// It sets the override directly rather than calling the linux-tagged test
// helper, so this file still builds on FreeBSD and Windows.
func withWalletStdin(t *testing.T, answer string) {
	t.Helper()
	oldInteractive := stdinIsInteractiveOverride
	stdinIsInteractiveOverride = func() bool { return true }
	oldReader := stdinReader
	stdinReader = bufio.NewReader(strings.NewReader(answer))
	t.Cleanup(func() {
		stdinIsInteractiveOverride = oldInteractive
		stdinReader = oldReader
	})
}

// The removed flag is recognised by name on every subcommand and warns; it is
// neither a bypass nor a failure, and the call still reaches the provider.
func TestWalletRemovedWorkstationFlagWarns(t *testing.T) {
	var calls [][]string
	withWalletSeams(t, []Provider{walletTestProvider()}, &calls, "", "")
	for _, args := range [][]string{
		{"-f", "--i-am-on-my-own-workstation", "5CvF...ss58", "--coldkey_seed_file=/tmp/seed"},
		{"-f", "5CvF...ss58", "--message=m", "--signature=0x00", "--i-am-on-my-own-workstation"},
	} {
		var err error
		out := captureWalletStderr(t, func() { err = cmdWalletSet(args) })
		if err != nil {
			t.Fatalf("a removed flag must warn, not refuse, got %v", err)
		}
		if !strings.Contains(out, "is gone") {
			t.Fatalf("wallet set must name the removed flag, got %q", out)
		}
	}
	var err error
	out := captureWalletStderr(t, func() {
		err = cmdWalletSign([]string{"-f", "--i-am-on-my-own-workstation", "5CvF...ss58"})
	})
	if err != nil {
		t.Fatalf("wallet sign must warn, not refuse, got %v", err)
	}
	if !strings.Contains(out, "is gone") {
		t.Fatalf("wallet sign must name the removed flag, got %q", out)
	}
	if len(calls) == 0 {
		t.Fatal("a warned call must still delegate")
	}
}

// The browser form signs in the extension and never reads a seed, so it is not
// gated on a provider box. It still says so, because signing from a machine
// that also runs a provider is unusual.
func TestWalletSignWarnsButDelegatesOnProviderBox(t *testing.T) {
	var calls [][]string
	withWalletSeams(t, []Provider{walletTestProvider()}, &calls, "", "")
	t.Setenv("URNETWORK_PROFILE", "lowmem")
	var err error
	out := captureWalletStderr(t, func() {
		err = cmdWalletSign([]string{"-f", "5CvF...ss58", "--no-open"})
	})
	if err != nil {
		t.Fatalf("wallet sign must not be gated on a provider box: %v", err)
	}
	if !strings.Contains(out, "not recommended here") {
		t.Fatalf("wallet sign must warn on a provider box, got %q", out)
	}
	if len(calls) != 1 {
		t.Fatalf("wallet sign must delegate on a provider box, got %v", calls)
	}
}

func TestWalletSignDelegatesToTheProviderSigningPage(t *testing.T) {
	var calls [][]string
	withWalletSeams(t, []Provider{walletWorkstationProvider()}, &calls, "", "")
	// -f skips the confirm prompt
	if err := cmdWalletSign([]string{"-f", "5CvF...ss58", "--no-open"}); err != nil {
		t.Fatalf("wallet sign: %v", err)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "wallet sign 5CvF...ss58 --no-open" {
		t.Fatalf("wallet sign must run 'provider wallet sign' with the arguments, got %v", calls)
	}
	// the address is optional: the account is chosen on the page
	calls = nil
	if err := cmdWalletSign([]string{"-f"}); err != nil {
		t.Fatalf("wallet sign without an address: %v", err)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "wallet sign" {
		t.Fatalf("wallet sign without an address must still delegate, got %v", calls)
	}
}
