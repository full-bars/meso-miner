// parseArgsForTest parses argv against mainUsage without exiting on error
// (from sn 4ca8316e's main_test.go; the daemon1 family did not carry it).
package main

import (
	"testing"

	"github.com/docopt/docopt-go"
)

func parseArgsForTest(t *testing.T, argv []string) docopt.Opts {
	t.Helper()
	parser := &docopt.Parser{
		HelpHandler: docopt.NoHelpHandler,
	}
	opts, err := parser.ParseArgs(mainUsage(), argv, "test")
	if err != nil {
		t.Fatalf("parse %v: %s", argv, err)
	}
	return opts
}
