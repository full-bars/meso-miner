package urnettools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/urnetwork/connect/internal/tui"
	"github.com/urnetwork/connect/internal/tui/tcellui"
)

var (
	topStdoutIsTerminal = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }
	topOpenScreen       = tcellui.Open
	topSourceFn         = func() topSource { return socketTopSource{} }
)

func cmdTop(args []string) error {
	// --demo is hidden: it draws synthetic snapshots so the screen can be seen
	// on a box with no provider. It is peeled off before the real parsers.
	demo, args := stripDemoFlag(args)
	interval, targetArgs, err := parseTopFlags(args)
	if err != nil {
		return err
	}
	if !topStdoutIsTerminal() {
		return errors.New("top needs an interactive terminal (stdout is not one); use `urnet-tools status` for one-shot or scripted output, add --json for machine-readable output")
	}
	providers, cur := []Provider{demoProvider()}, 0
	if !demo {
		t, extra, err := parseTargetFlags(targetArgs)
		if err != nil {
			return err
		}
		if len(extra) > 0 {
			return errUnexpectedPositional("top", extra)
		}
		providers = discoverStatusFn()
		p, _, err := selectTargetOrSoleAccessible(providers, t, false)
		if err != nil {
			// Several providers and no target: status summarizes them, top starts
			// on the first and lets Tab move between them.
			if hasExplicitTarget(t) || len(providers) < 2 {
				return err
			}
			p = providers[0]
		}
		if elevated, err := maybeElevateForCrossUser("top", p, args, false, false); elevated {
			return err
		}
		for i, q := range providers {
			if matchKey(q) == matchKey(p) {
				cur = i
				break
			}
		}
	}
	if elevated, err := maybeElevateForCrossUser("top", p, args, false, false); elevated {
		return err
	}
	scr, err := topOpenScreen()
	if err != nil {
		return fmt.Errorf("cannot open the terminal for top: %w (use `urnet-tools status` instead)", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	th := tui.ThemeFromEnv(os.Getenv)
	m := newTopModel(providers, cur, interval, th, time.Now)
	if hasNoMouseFlag(args) {
		m.mouse = false
	}
	// The environment's own choice (NO_COLOR, a dumb terminal) outranks a saved
	// theme; see applySettings.
	m.themeLocked = th.ASCII
	m.settingsPath = topSettingsPath()
	m.applySettings(loadTopSettings(m.settingsPath))
	src := topSourceFn()
	if demo {
		src = newDemoTopSource(time.Now)
	}
	if err := runTop(ctx, scr, m, src, topTick); err != nil {
		return err
	}
	// The copy is printed only after the terminal is back, so the screen text
	// lands on the real stdout, not the alternate screen.
	if m.copyText != "" {
		fmt.Println(m.copyText)
	}
	if m.copyErr != nil {
		fmt.Fprintln(os.Stderr, "could not write the copy:", m.copyErr)
	}
	if m.copyPath != "" {
		fmt.Fprintf(os.Stderr, "copy written to %s\n", m.copyPath)
	}
	return nil
}

func hasNoMouseFlag(args []string) bool {
	for _, a := range args {
		if a == "--no-mouse" || a == "--no-mouse=true" || a == "--no-mouse=1" {
			return true
		}
	}
	return false
}

func parseTopFlags(args []string) (interval time.Duration, rest []string, err error) {
	interval = topDefaultInterval
	for i := 0; i < len(args); i++ {
		a := args[i]
		var val string
		switch {
		case a == "--interval":
			if i+1 >= len(args) {
				return 0, nil, errors.New("--interval needs a value, for example --interval 500ms")
			}
			i++
			val = args[i]
		case strings.HasPrefix(a, "--interval="):
			val = strings.TrimPrefix(a, "--interval=")
		case a == "--copy":
			continue
		case a == "--plain":
			continue
		default:
			rest = append(rest, a)
		}
		if perr != nil {
			return 0, nil, fmt.Errorf("--interval %q is not a duration (try 500ms or 2s)", val)
		}
		d := time.Duration(secs * float64(time.Second))
		if d < topMinInterval {
			return 0, nil, fmt.Errorf("--interval must be at least %s", topMinInterval)
		}
		interval = d
	}
	return interval, rest, nil
}
