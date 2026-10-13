package main

import (
	"flag"
	"os"
	"testing"
)

func TestRetentionCommandRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"extra"},
		{"-before", "invalid-date"},
		{"-before", "2026-01-01", "-policy"},
	} {
		if err := retention(args); err == nil {
			t.Fatalf("accepted invalid arguments %v", args)
		}
	}
}

func TestServerRejectsUnexpectedArguments(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	for _, arg := range []string{"migrate", "foo", "extra"} {
		os.Args = []string{"minihub", arg}
		flag.CommandLine = flag.NewFlagSet("minihub", flag.ContinueOnError)
		if err := run(); err == nil {
			t.Fatalf("expected error for unexpected argument %q, got nil", arg)
		}
	}
}
