package urnettools

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestFreeBSDSetupIsPrintOnly(t *testing.T) {
	for _, root := range []bool{false, true} {
		for _, dry := range []bool{false, true} {
			var out bytes.Buffer
			var commands, writes, elevations int
			runner := func(string, ...string) ([]byte, error) { commands++; return nil, nil }
			cfg := &SystemPlatformConfigurer{PlatformName: "freebsd", ExecCmd: runner,
				WriteFile: func(string, []byte, os.FileMode) error { writes++; return nil }}
			err := RunExtenderSetup(ExtenderSetupOptions{
				Platform: "freebsd", Stdout: &out, Stderr: &out, PlatformCfg: cfg,
				PortChecker: &PortChecker{Platform: "freebsd", ExecCmd: runner},
				FirewallMgr: &FirewallManager{Platform: "freebsd", ExecCmd: runner, LookPath: func(s string) (string, error) { return s, nil }},
				IsRoot:      func() bool { return root }, DryRun: dry, Yes: true,
				ElevateFunc: func([]string) error { elevations++; return nil },
			})
			if err != nil || commands != 0 || writes != 0 || elevations != 0 {
				t.Fatalf("root=%v dry=%v err=%v commands=%d writes=%d elevations=%d", root, dry, err, commands, writes, elevations)
			}
			for _, want := range []string{"untested this release", "previous=$(sysctl -n net.inet.ip.portrange.reservedhigh)", "sudo sysctl net.inet.ip.portrange.reservedhigh=0", "sudo sysctl net.inet.ip.portrange.reservedhigh=\"$previous\"", "Undo", "firewall", "UDP 53"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("root=%v dry=%v missing %q in %q", root, dry, want, out.String())
				}
			}
		}
	}
}

func TestFreeBSDPlatformApplyNeverWrites(t *testing.T) {
	calls := 0
	cfg := &SystemPlatformConfigurer{PlatformName: "freebsd", ExecCmd: func(string, ...string) ([]byte, error) { calls++; return nil, nil },
		ReadFile:  func(string) ([]byte, error) { calls++; return nil, nil },
		WriteFile: func(string, []byte, os.FileMode) error { calls++; return nil }}
	applied, _, err := cfg.ApplyRequirement("", "provider")
	if err != nil || applied || calls != 0 {
		t.Fatalf("applied=%v err=%v calls=%d", applied, err, calls)
	}
}
