package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootInvalidProgressTarget(t *testing.T) {
	_, cleanup := setupMainTestEnv(t)
	defer cleanup()
	cmd := newRootCommand()
	cmd.SetArgs([]string{"Artist - Night", "--sources", " soundcloud, , youtube ", "--progress-target", "invalid://target"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "initializing progress reporter") {
		t.Fatalf("error = %v", err)
	}
}

func TestVerifyWithoutTargetShowsHelp(t *testing.T) {
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"verify"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "verify") {
		t.Fatalf("help = %q", out.String())
	}
}

func TestVerifyMissingFileReturnsActionableError(t *testing.T) {
	_, cleanup := setupMainTestEnv(t)
	defer cleanup()
	t.Setenv("AGENT", "1")
	cmd := newRootCommand()
	cmd.SetArgs([]string{"verify", t.TempDir() + "/missing.m4a"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "audio verification failed") {
		t.Fatalf("error = %v", err)
	}
}
