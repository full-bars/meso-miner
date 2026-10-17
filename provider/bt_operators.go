// The operators surface: `provider operators` and `provider provide
// --all-operators`. Ported from the sn miner's operators_cmd.go (upstream
// 4ca8316e; the fork branch's provider lane has no operators code), adapted
// to the v33 provider:
//
//   - The state directory is ~/.urnetwork: this binary keys every state path
//     off HOME and has no URNETWORK_STATE_DIR, so each operator's directory
//     below operators/ doubles as its child provider's home (see
//     bt_operators_supervisor.go, operatorChildEnv).
//   - The frozen grammar carries --operators-url, --operators-refresh and -v
//     only. There is no --auto-register, --hotkey_seed_file, --port,
//     --max-memory or --allow-client-registration here, so this file has no
//     hotkey sign-in, memory share or status server; an operator without a
//     jwt is reported as awaiting auth and is picked up once its jwt appears.
//   - `auth --operator` does not exist on this line, so the jwt is written
//     outside this command; the operator directory layout is the contract.
//
// Every operator keeps its own state directory, operatorlist.DomainStateDir
// below the provider state directory, and the list is cached as operators.yml
// in the provider state directory, so these commands work while ur.xyz is
// unreachable.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/operatorlist"
)

const defaultOperatorsRefresh = time.Hour

// operatorStateSubdirectory is where one operator's provider state lives
// inside the operator's directory: the directory itself is the child
// provider's home (HOME), and the v33 provider keeps its state in
// ~/.urnetwork.
const operatorStateSubdirectory = ".urnetwork"

// operatorBaseStateDir is the provider state directory ~/.urnetwork that
// every other v33 state path (jwt, .provider.key, .client_jwts.json,
// network.json, proxies) also lives in. mustStateDir is that same directory;
// it only answers "" when the home directory is unavailable.
func operatorBaseStateDir() (string, error) {
	if base := mustStateDir(); base != "" {
		return base, nil
	}
	return "", errors.New("cannot determine the provider state directory (~/.urnetwork)")
}

// operatorDirectory is one operator's directory below the provider state
// directory. It is the child provider's home, and its .urnetwork below is
// the child's state directory.
func operatorDirectory(base string, domain string) string {
	return operatorlist.DomainStateDir(base, domain)
}

// operatorJwtPath is one operator's network jwt: the jwt written for that
// operator, and the credential the supervisor and `provider operators`
// check. Also used by the wallet hotkey commands.
func operatorJwtPath(base string, domain string) string {
	return filepath.Join(operatorDirectory(base, domain), operatorStateSubdirectory, "jwt")
}

func operatorListCachePath(base string) string {
	return filepath.Join(base, "operators.yml")
}

// readOperatorJwt reads one operator's jwt with the same contract as the sn's
// clientauth.ReadToken: the file is trimmed, and an empty one is an error.
// It is repeated here because sn/clientauth imports github.com/urnetwork/sdk,
// which this module does not carry; nothing else in clientauth is used by
// this family.
func readOperatorJwt(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return token, nil
}

// One fetch of the list, or the cached copy when the fetch fails.
func loadOperatorList(ctx context.Context, opts docopt.Opts, base string) (*operatorlist.Snapshot, error) {
	operatorsUrl, _ := opts.String("--operators-url")
	return operatorlist.Load(ctx, operatorlist.RefreshSettings{Url: operatorsUrl, CachePath: operatorListCachePath(base)})
}

// The frozen grammar leaves --api_url in the --all-operators pattern; it has
// no meaning there, because every operator's urls come from the operator
// list (the sn refuses it the same way). An explicit value is refused by
// name rather than silently ignored.
func allOperatorsArgsError(opts docopt.Opts) error {
	if apiUrl, err := opts.String("--api_url"); err == nil && apiUrl != "" {
		return fmt.Errorf("provide --all-operators excludes --api_url=%s: each operator's urls come from the operator list", apiUrl)
	}
	return nil
}

// Reads the --all-operators options. Without a hotkey of any kind, the only
// work here is the refresh validation and the executable path.
func newOperatorSupervisorSettings(opts docopt.Opts) (operatorSupervisorSettings, operatorlist.RefreshSettings, error) {
	base, err := operatorBaseStateDir()
	if err != nil {
		return operatorSupervisorSettings{}, operatorlist.RefreshSettings{}, err
	}
	refresh := defaultOperatorsRefresh
	if raw, err := opts.String("--operators-refresh"); err == nil {
		refresh, err = time.ParseDuration(raw)
		if err != nil || refresh < time.Minute {
			return operatorSupervisorSettings{}, operatorlist.RefreshSettings{}, fmt.Errorf("--operators-refresh=%s must be a Go duration of at least 1m", raw)
		}
	}
	operatorsUrl, _ := opts.String("--operators-url")
	settings := operatorSupervisorSettings{baseStateDir: base}
	settings.verbosity, _ = opts["-v"].(int)
	if settings.executable, err = os.Executable(); err != nil {
		return operatorSupervisorSettings{}, operatorlist.RefreshSettings{}, err
	}
	listSettings := operatorlist.RefreshSettings{Url: operatorsUrl, Interval: refresh, CachePath: operatorListCachePath(base)}
	return settings, listSettings, nil
}

// "provider provide --all-operators".
func provideAllOperatorsCmd(opts docopt.Opts) {
	if err := allOperatorsArgsError(opts); err != nil {
		fmt.Fprintf(os.Stderr, "provide --all-operators: %v\n", err)
		os.Exit(1)
	}
	settings, listSettings, err := newOperatorSupervisorSettings(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provide --all-operators: %v\n", err)
		os.Exit(1)
	}
	supervisor := newOperatorSupervisor(settings, defaultOperatorSupervisorHooks(), os.Stderr)
	// subscribed before any child exists, so every stop signal reaches the
	// children through the supervisor
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	ctx := context.Background()
	refresher, err := operatorlist.NewRefresher(ctx, listSettings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provide --all-operators: %v\n", err)
		os.Exit(1)
	}
	supervisor.Run(ctx, refresher, signals)
	signal.Stop(signals)
	refresher.Close()
}

// "provider operators".
func operatorsCmd(opts docopt.Opts) {
	if err := printOperators(context.Background(), opts, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "operators: %v\n", err)
		os.Exit(1)
	}
}

// The list's source and digest, then each listed operator with its credential
// state and urls.
func printOperators(ctx context.Context, opts docopt.Opts, out io.Writer) error {
	base, err := operatorBaseStateDir()
	if err != nil {
		return err
	}
	snapshot, err := loadOperatorList(ctx, opts, base)
	if err != nil {
		return err
	}
	if snapshot.Cached {
		fmt.Fprintf(out, "source: cached %s, fetched %s\n", operatorListCachePath(base), snapshot.FetchedAt.UTC().Format(time.RFC3339))
	} else {
		operatorsUrl, _ := opts.String("--operators-url")
		if operatorsUrl == "" {
			operatorsUrl = operatorlist.DefaultUrl
		}
		fmt.Fprintf(out, "source: fetched %s\n", operatorsUrl)
	}
	fmt.Fprintf(out, "digest: %s\n", snapshot.Sha256)
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	awaiting := false
	for _, operator := range snapshot.List.Operators {
		state := "jwt present"
		if _, err := readOperatorJwt(operatorJwtPath(base, operator.Domain)); err != nil {
			state = "awaiting auth"
			awaiting = true
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", operator.Domain, state, operator.ApiUrl, operator.ConnectUrl)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if awaiting {
		fmt.Fprintf(out, "an operator awaiting auth needs its network jwt at %s\n", filepath.Join(base, "operators", "<domain>", operatorStateSubdirectory, "jwt"))
	}
	return nil
}
