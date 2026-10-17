package main

// The supervisor of "provide --all-operators". Ported from the sn miner's
// operators_supervisor.go (upstream 4ca8316e), adapted to the v33 provider's
// frozen grammar and state model:
//
//   - It re-executes this binary as one "provide" child per listed operator
//     that has a jwt, with the operator's urls from the list.
//   - There is no URNETWORK_STATE_DIR in this binary: every state path is
//     keyed off HOME, so each child is handed the operator's directory
//     (operatorlist.DomainStateDir below the provider state directory) as
//     HOME, and the child's state directory is the .urnetwork inside it
//     (operatorJwtPath is the jwt there). An inherited HOME is dropped.
//   - Without --auto-register, --hotkey_seed_file and --port in the grammar
//     there is no hotkey sign-in, no hotkey wallet upkeep, no memory share
//     and no status server here. An operator without a jwt is awaiting auth:
//     it gets no child and its jwt is checked again every 5 minutes, so it
//     starts as soon as its jwt is written.
//
// The loop keeps the rest of the sn behavior unchanged:
//   - a child that exits restarts after a backoff, 30s doubling to 10m. A
//     child that ran for 10m starts the doubling over.
//   - the child of a delisted operator, or one whose urls changed, gets
//     SIGTERM and SIGKILL 60s later. The state directory stays.
//   - SIGINT, SIGTERM and SIGQUIT are forwarded to every child, and the
//     supervisor returns once all of them have exited.
//
// Only the Run loop touches operator state. Child exits and credential
// checks reach it over channels.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/urfoundation/sn/operatorlist"
)

const operatorRestartMinimumDelay = 30 * time.Second
const operatorRestartMaximumDelay = 10 * time.Minute
const operatorStopGrace = 60 * time.Second
const operatorAuthRecheckInterval = 5 * time.Minute

// The supervisor's command line, read by newOperatorSupervisorSettings.
type operatorSupervisorSettings struct {
	// today's provider state directory; each operator's lies below it
	baseStateDir string
	executable   string
	verbosity    int
}

// A started child: a real process, or a test's stand-in.
type operatorChildProcess interface {
	Pid() int
	Signal(os.Signal) error
	// Returns once the child has exited.
	Wait() error
}

// This binary, with a child's arguments and environment.
type operatorChildCommand struct {
	path string
	args []string
	env  []string
}

// Tests replace these, so they run no process and wait on no real timer.
type operatorSupervisorHooks struct {
	start func(operatorChildCommand) (operatorChildProcess, error)
	now   func() time.Time
	after func(time.Duration) <-chan time.Time
	// sees each next deadline (zero for none) just before the loop waits
	idle func(time.Time)
}

func defaultOperatorSupervisorHooks() operatorSupervisorHooks {
	return operatorSupervisorHooks{
		start: startOperatorChild,
		now:   time.Now,
		after: time.After,
	}
}

// The refresher, or a test's list.
type operatorListSource interface {
	Snapshot() (*operatorlist.Snapshot, chan struct{})
}

// What a child runs with. A running child whose spec changes restarts.
type operatorChildSpec struct {
	apiUrl     string
	connectUrl string
}

// The loop's record of one operator. It outlives a delisting until the
// operator's child has stopped.
type operatorState struct {
	// the latest listing, kept after delisting
	operator operatorlist.Operator
	listed   bool
	// a jwt was found; the child may start
	credentialed bool
	// while awaiting auth, when the jwt is checked again
	authCheckAt time.Time
	child       operatorChildProcess
	spec        operatorChildSpec
	startedAt   time.Time
	stopping    bool
	killAt      time.Time
	killed      bool
	restartAt   time.Time
	// consecutive runs shorter than the maximum backoff
	failures int
}

// From the goroutine that waited on a child.
type operatorChildExit struct {
	domain string
	child  operatorChildProcess
	err    error
}

type operatorSupervisor struct {
	settings     operatorSupervisorSettings
	hooks        operatorSupervisorHooks
	log          io.Writer
	operators    map[string]*operatorState
	listSha256   string
	exits        chan operatorChildExit
	shuttingDown bool
}

func newOperatorSupervisor(settings operatorSupervisorSettings, hooks operatorSupervisorHooks, log io.Writer) *operatorSupervisor {
	return &operatorSupervisor{
		settings:  settings,
		hooks:     hooks,
		log:       log,
		operators: map[string]*operatorState{},
		exits:     make(chan operatorChildExit),
	}
}

// Supervises until a signal or ctx stops it and every child has exited.
func (self *operatorSupervisor) Run(ctx context.Context, list operatorListSource, signals <-chan os.Signal) {
	snapshot, update := list.Snapshot()
	self.applyList(snapshot)
	done := ctx.Done()
	for {
		now := self.hooks.now()
		next := self.reconcile(now)
		if self.shuttingDown && !self.anyChild() {
			return
		}
		var timeout <-chan time.Time
		if !next.IsZero() {
			timeout = self.hooks.after(next.Sub(now))
		}
		if self.hooks.idle != nil {
			self.hooks.idle(next)
		}
		select {
		case <-update:
			snapshot, update = list.Snapshot()
			self.applyList(snapshot)
		case exit := <-self.exits:
			self.childExited(exit)
		case received := <-signals:
			self.shutdown(received)
		case <-done:
			done = nil
			self.shutdown(syscall.SIGTERM)
		case <-timeout:
		}
	}
}

// Marks which operators are listed and logs what changed.
func (self *operatorSupervisor) applyList(snapshot *operatorlist.Snapshot) {
	if snapshot == nil {
		fmt.Fprintln(self.log, "waiting for the operator list")
		return
	}
	if snapshot.Sha256 == self.listSha256 {
		return
	}
	self.listSha256 = snapshot.Sha256
	source := "fetched"
	if snapshot.Cached {
		source = "cached"
	}
	fmt.Fprintf(self.log, "operator list %s (%s): %d operators\n", snapshot.Sha256, source, len(snapshot.List.Operators))
	listed := map[string]bool{}
	for _, operator := range snapshot.List.Operators {
		listed[operator.Domain] = true
		state, ok := self.operators[operator.Domain]
		switch {
		case !ok:
			state = &operatorState{}
			self.operators[operator.Domain] = state
		case !state.listed:
			fmt.Fprintf(self.log, "operator %s is listed again\n", operator.Domain)
		case state.operator != operator:
			fmt.Fprintf(self.log, "operator %s changed its urls\n", operator.Domain)
		}
		state.operator = operator
		state.listed = true
	}
	for _, domain := range self.domains() {
		if state := self.operators[domain]; state.listed && !listed[domain] {
			state.listed = false
			fmt.Fprintf(self.log, "operator %s was delisted; its state directory is kept\n", domain)
		}
	}
}

// Moves every operator toward what the list and its credentials ask for, and
// returns the earliest deadline still ahead, or zero.
func (self *operatorSupervisor) reconcile(now time.Time) time.Time {
	domains := self.domains()
	if !self.shuttingDown {
		// credentials first, so every operator that can start does in this
		// pass
		for _, domain := range domains {
			state := self.operators[domain]
			if state.listed && state.child == nil && !now.Before(state.restartAt) && !now.Before(state.authCheckAt) {
				self.checkCredentials(domain, state, now)
			}
		}
	}
	var next time.Time
	earliest := func(deadline time.Time) {
		if next.IsZero() || deadline.Before(next) {
			next = deadline
		}
	}
	for _, domain := range domains {
		state := self.operators[domain]
		switch {
		case state.child != nil && state.stopping:
			if !state.killed && !now.Before(state.killAt) {
				fmt.Fprintf(self.log, "operator %s: provider (pid %d) did not stop within %s; killing it\n", domain, state.child.Pid(), operatorStopGrace)
				self.signal(domain, state, os.Kill)
				state.killed = true
			}
			if !state.killed {
				earliest(state.killAt)
			}
		case state.child != nil && !state.listed:
			fmt.Fprintf(self.log, "operator %s: stopping its provider (pid %d)\n", domain, state.child.Pid())
			self.stop(domain, state, syscall.SIGTERM, now)
			earliest(state.killAt)
		case state.child != nil && self.childSpec(state) != state.spec:
			fmt.Fprintf(self.log, "operator %s: restarting its provider (pid %d) with its new urls\n", domain, state.child.Pid())
			self.stop(domain, state, syscall.SIGTERM, now)
			earliest(state.killAt)
		case state.child != nil:
		case self.shuttingDown:
		case !state.listed:
			delete(self.operators, domain)
		case now.Before(state.restartAt):
			earliest(state.restartAt)
		case !state.credentialed:
			earliest(state.authCheckAt)
		default:
			self.start(domain, state, self.childSpec(state), now)
			if state.child == nil {
				earliest(state.restartAt)
			}
		}
	}
	return next
}

// Finds the operator's jwt, or logs that the operator awaits auth and
// schedules the next check. There is no hotkey sign-in here: without
// --auto-register the jwt is written outside this command, at
// operatorJwtPath below the operator's directory.
func (self *operatorSupervisor) checkCredentials(domain string, state *operatorState, now time.Time) {
	jwtPath := operatorJwtPath(self.settings.baseStateDir, domain)
	if _, err := readOperatorJwt(jwtPath); err == nil {
		state.credentialed = true
		return
	}
	state.credentialed = false
	fmt.Fprintf(self.log, "operator %s is awaiting auth: its network jwt goes at %s\n", domain, jwtPath)
	state.authCheckAt = now.Add(operatorAuthRecheckInterval)
}

func (self *operatorSupervisor) childSpec(state *operatorState) operatorChildSpec {
	return operatorChildSpec{apiUrl: state.operator.ApiUrl, connectUrl: state.operator.ConnectUrl}
}

// A child never gets --all-operators. A new operator directory needs no
// client-registration flag on this line: the v33 provider registers its own
// client identity without one.
func (self *operatorSupervisor) childArgs(spec operatorChildSpec) []string {
	args := []string{"provide", "--api_url=" + spec.apiUrl, "--connect_url=" + spec.connectUrl, "--port=0"}
	if 0 < self.settings.verbosity {
		args = append(args, "-"+strings.Repeat("v", self.settings.verbosity))
	}
	return args
}

// The supervisor's environment with the operator's directory in place of any
// inherited home, so the child's ~/.urnetwork is the operator's own state
// directory. This binary has no URNETWORK_STATE_DIR, so HOME is the only
// switch: it is what os.UserHomeDir reads on unix, and USERPROFILE (with
// HOMEDRIVE/HOMEPATH) is what it reads on windows.
func operatorChildEnv(environ []string, directory string) []string {
	env := make([]string, 0, len(environ)+2)
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH":
			continue
		}
		env = append(env, entry)
	}
	return append(env, "HOME="+directory, "USERPROFILE="+directory)
}

// A child that cannot start counts as one that exited at once.
func (self *operatorSupervisor) start(domain string, state *operatorState, spec operatorChildSpec, now time.Time) {
	directory := operatorDirectory(self.settings.baseStateDir, domain)
	child, err := func() (operatorChildProcess, error) {
		// a child without its proxies would provide from this host directly
		if err := copyOperatorProxyFile(self.settings.baseStateDir, directory); err != nil {
			return nil, fmt.Errorf("proxy file: %w", err)
		}
		return self.hooks.start(operatorChildCommand{
			path: self.settings.executable,
			args: self.childArgs(spec),
			env:  operatorChildEnv(os.Environ(), directory),
		})
	}()
	if err != nil {
		delay := self.backoff(state, 0)
		state.restartAt = now.Add(delay)
		fmt.Fprintf(self.log, "operator %s: provider did not start: %v; trying again in %s\n", domain, err, delay)
		return
	}
	state.child = child
	state.spec = spec
	state.startedAt = now
	state.restartAt = time.Time{}
	fmt.Fprintf(self.log, "operator %s: provider started (pid %d) for %s\n", domain, child.Pid(), spec.apiUrl)
	go func() {
		err := child.Wait()
		self.exits <- operatorChildExit{domain: domain, child: child, err: err}
	}()
}

// The next restart delay. A run as long as the maximum delay starts the
// doubling over.
func (self *operatorSupervisor) backoff(state *operatorState, uptime time.Duration) time.Duration {
	if operatorRestartMaximumDelay <= uptime {
		state.failures = 0
	}
	state.failures++
	return min(operatorRestartMinimumDelay<<min(state.failures-1, 5), operatorRestartMaximumDelay)
}

// A child stopped on purpose starts again at once if still wanted; any other
// exit waits out the backoff. A child that exited after its jwt went away is
// rechecked at once, without the crash backoff.
func (self *operatorSupervisor) childExited(exit operatorChildExit) {
	state, ok := self.operators[exit.domain]
	if !ok || state.child != exit.child {
		return
	}
	state.child = nil
	if state.stopping {
		state.stopping = false
		state.killed = false
		fmt.Fprintf(self.log, "operator %s: provider (pid %d) stopped\n", exit.domain, exit.child.Pid())
		return
	}
	if _, err := os.Stat(operatorJwtPath(self.settings.baseStateDir, exit.domain)); errors.Is(err, os.ErrNotExist) {
		state.credentialed = false
		state.restartAt = time.Time{}
		state.authCheckAt = time.Time{}
		fmt.Fprintf(self.log, "operator %s: provider (pid %d) exited without a network sign-in; checking its credentials\n", exit.domain, exit.child.Pid())
		return
	}
	now := self.hooks.now()
	delay := self.backoff(state, now.Sub(state.startedAt))
	state.restartAt = now.Add(delay)
	cause := "exited"
	if exit.err != nil {
		cause = fmt.Sprintf("exited: %v", exit.err)
	}
	fmt.Fprintf(self.log, "operator %s: provider (pid %d) %s; restarting in %s\n", exit.domain, exit.child.Pid(), cause, delay)
}

// Forwards a stop signal to every child and starts nothing after it.
func (self *operatorSupervisor) shutdown(received os.Signal) {
	if !self.shuttingDown {
		self.shuttingDown = true
		fmt.Fprintf(self.log, "%s: stopping every provider\n", received)
	}
	now := self.hooks.now()
	for _, domain := range self.domains() {
		if state := self.operators[domain]; state.child != nil {
			self.stop(domain, state, received, now)
		}
	}
}

// The first stop arms the kill deadline; later signals only forward.
func (self *operatorSupervisor) stop(domain string, state *operatorState, signal os.Signal, now time.Time) {
	self.signal(domain, state, signal)
	if !state.stopping {
		state.stopping = true
		state.killAt = now.Add(operatorStopGrace)
	}
}

func (self *operatorSupervisor) signal(domain string, state *operatorState, signal os.Signal) {
	// a child that exited before its exit is received is already done
	if err := state.child.Signal(signal); err != nil && !errors.Is(err, os.ErrProcessDone) {
		fmt.Fprintf(self.log, "operator %s: provider (pid %d) %s: %v\n", domain, state.child.Pid(), signal, err)
	}
}

func (self *operatorSupervisor) domains() []string {
	return slices.Sorted(maps.Keys(self.operators))
}

func (self *operatorSupervisor) anyChild() bool {
	for _, state := range self.operators {
		if state.child != nil {
			return true
		}
	}
	return false
}

// Copies the base proxy file over the operator's through a temporary file and
// a rename, with the base file's mode. The operator's copy is in the child's
// state directory, where the child reads its proxies from. Without a base
// proxy file the operator's directory is left as it is.
func copyOperatorProxyFile(base string, directory string) (returnErr error) {
	source, err := os.Open(filepath.Join(base, "proxy"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(source)
	if err != nil {
		return err
	}
	stateDir := filepath.Join(directory, operatorStateSubdirectory)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(stateDir, ".proxy.tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
	}()
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), filepath.Join(stateDir, "proxy"))
}

type operatorExecChild struct {
	command *exec.Cmd
}

func (self *operatorExecChild) Pid() int {
	return self.command.Process.Pid
}

func (self *operatorExecChild) Signal(signal os.Signal) error {
	return self.command.Process.Signal(signal)
}

func (self *operatorExecChild) Wait() error {
	return self.command.Wait()
}

// Children write to the supervisor's stdout and stderr directly, so their
// diagnostics reach the same journal unchanged.
func startOperatorChild(command operatorChildCommand) (operatorChildProcess, error) {
	child := exec.Command(command.path, command.args...)
	child.Env = command.env
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		return nil, err
	}
	return &operatorExecChild{command: child}, nil
}
