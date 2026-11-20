package urnettools

import (
	"strings"
	"testing"
)

func TestExtenderHelpAndAboutDescribeManualRequirements(t *testing.T) {
	help := newExtenderCmd().Long
	for _, want := range []string{"UDP 53 is optional", "--udp53", "firewall access is user-managed", "FreeBSD extender setup is untested", "prints manual instructions only"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q", want)
		}
	}
	for _, want := range []string{"setup --route=sysctl --udp53", "firewall access is user-managed", "untested this release", "-p 53:53/udp", "Compose networks need IPv6 enabled", "hot swap can race", "Docker log follower"} {
		if !strings.Contains(extenderAboutText, want) {
			t.Errorf("about missing %q", want)
		}
	}
}
