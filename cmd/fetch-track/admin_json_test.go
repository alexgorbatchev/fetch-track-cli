package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexgorbatchev/godeps"
	"github.com/dj/fetch-track-cli/internal/deps"
)

func TestAdminJSONDependencies(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("AGENT", "0")
	original := deps.RequiredDependencies
	t.Cleanup(func() { deps.RequiredDependencies = original })
	calls := 0
	deps.RequiredDependencies = []deps.Dependency{{Name: "admin-fixture", MinVersion: "1.0.0", Installer: godeps.InstallerFunc(func(ctx context.Context, dir string) error {
		calls++
		return os.WriteFile(filepath.Join(dir, "admin-fixture"), []byte("#!/bin/sh\nprintf '9.0.0\\n'\n"), 0755)
	})}}
	run := func(args ...string) (map[string]json.RawMessage, error) {
		t.Helper()
		command := newRootCommand()
		var out bytes.Buffer
		command.SetOut(&out)
		command.SetErr(&bytes.Buffer{})
		command.SetArgs(append([]string{"--json", "deps"}, args...))
		err := command.Execute()
		var result map[string]json.RawMessage
		if decodeErr := json.Unmarshal(out.Bytes(), &result); decodeErr != nil {
			t.Fatalf("expected only JSON, got %q: %v", out.String(), decodeErr)
		}
		var reports []deps.DependencyReport
		if e := json.Unmarshal(result["dependencies"], &reports); e != nil || len(reports) != 1 {
			t.Fatalf("missing native reports: %s (%v)", out.String(), e)
		}
		if string(result["changed"]) == "null" {
			t.Fatal("changed must be an array")
		}
		return result, err
	}
	result, err := run()
	if err == nil || string(result["status"]) != `"error"` {
		t.Fatalf("missing fixture must fail: %s %v", result, err)
	}
	for _, args := range [][]string{{"install"}, {"update"}, {"install", "admin-fixture"}, {"update", "admin-fixture"}} {
		result, err = run(args...)
		if err != nil || string(result["status"]) != `"ok"` || string(result["changed"]) != `["admin-fixture"]` {
			t.Fatalf("mutation %v: %s %v", args, result, err)
		}
	}
	if calls != 4 {
		t.Fatalf("native installer calls: %d", calls)
	}
	result, err = run("install")
	if err != nil || string(result["changed"]) != "[]" {
		t.Fatalf("satisfied install: %s %v", result, err)
	}
	for _, action := range []string{"install", "update"} {
		result, err = run(action, "unknown")
		if err == nil || string(result["status"]) != `"error"` {
			t.Fatalf("unknown dependency: %s %v", result, err)
		}
	}
	deps.RequiredDependencies[0].Installer = godeps.InstallerFunc(func(context.Context, string) error { return errors.New("fixture failure") })
	result, err = run("update")
	if err == nil || string(result["status"]) != `"error"` {
		t.Fatalf("failed update: %s %v", result, err)
	}
}

func TestAdminJSONUpgrade(t *testing.T) {
	for _, tc := range []struct {
		version string
		err     error
		status  string
	}{{"9.0.0", nil, "ok"}, {"", errors.New("already at the latest version"), "current"}, {"", errors.New("fixture failure"), "error"}} {
		t.Run(tc.status, func(t *testing.T) {
			restore := deps.SetUpgradeSelfFunc(func(context.Context, string, string, string) (string, error) { return tc.version, tc.err })
			t.Cleanup(restore)
			command := newRootCommand()
			var out bytes.Buffer
			command.SetOut(&out)
			command.SetErr(&bytes.Buffer{})
			command.SetArgs([]string{"--json", "upgrade"})
			err := command.Execute()
			var result struct {
				Status  string `json:"status"`
				Version string `json:"version"`
				Error   string `json:"error"`
			}
			if e := json.Unmarshal(out.Bytes(), &result); e != nil {
				t.Fatalf("expected only JSON, got %q: %v", out.String(), e)
			}
			if result.Status != tc.status || (err != nil) != (tc.status == "error") {
				t.Fatalf("upgrade: %+v %v", result, err)
			}
			if tc.status == "ok" && result.Version != "9.0.0" {
				t.Fatalf("version: %+v", result)
			}
			if tc.status == "error" && result.Error == "" {
				t.Fatal("missing error")
			}
		})
	}
}
