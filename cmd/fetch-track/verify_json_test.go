package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/fetch-track-cli/internal/deps"
	"github.com/alexgorbatchev/fetch-track-cli/internal/pipeline"
	"github.com/spf13/cobra"
)

func TestJSONEarlyFailuresReturnOneTypedResult(t *testing.T) {
	for _, tc := range []struct {
		name, target, message string
		args                  []string
		missingDependencies   bool
	}{
		{"verify dependencies", "track.flac", "missing", []string{"--json", "verify", "track.flac"}, true},
		{"verify analysis", "missing.flac", "audio verification failed", []string{"--json", "verify", "missing.flac"}, false},
		{"verify missing target", "", "target", []string{"--json", "verify"}, false},
		{"fetch dependencies", "Artist - Track", "missing", []string{"--json", "Artist - Track"}, true},
		{"fetch missing target", "", "target", []string{"--json"}, false},
		{"candidates missing target", "", "target", []string{"--list-candidates"}, false},
		{"fetch progress initialization", "Artist - Track", "progress reporter", []string{"--json", "--progress-target", "unsupported://fixture", "Artist - Track"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			t.Setenv("AGENT", "0")
			original := deps.RequiredDependencies
			t.Cleanup(func() { deps.RequiredDependencies = original })
			deps.RequiredDependencies = nil
			if tc.missingDependencies {
				deps.RequiredDependencies = []deps.Dependency{{Name: "json-fixture", MinVersion: "1.0.0"}}
			}
			t.Cleanup(deps.SetDefaultRunner(func(context.Context, string, ...string) ([]byte, error) {
				return nil, errors.New("missing fixture dependency")
			}))
			result, err := executeJSONCommand(t, tc.args)
			if err == nil || result.Status != "error" || result.Target != tc.target || !strings.Contains(result.Error, tc.message) {
				t.Fatalf("failure envelope %+v error %v", result, err)
			}
		})
	}
}

func TestVerifyJSONSuccessInBothModes(t *testing.T) {
	_, cleanup := setupMainTestEnv(t)
	defer cleanup()
	path := createTestAudio(t, t.TempDir(), "verification.m4a")
	for _, mode := range []string{"0", "1"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			result, err := executeJSONCommand(t, []string{"--json", "verify", path})
			if err != nil || result.Status != "success" || result.Target != path || result.Verification == nil || result.Verification.DurationSeconds != 2 {
				t.Fatalf("verification result %+v error %v", result, err)
			}
		})
	}
}

type brokenJSONWriter struct{}

func (brokenJSONWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestJSONFailurePreservesOperationAndOutputErrors(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(brokenJSONWriter{})
	operationErr := errors.New("dependency unavailable")
	err := writeJSONFailure(cmd, "fixture.flac", operationErr)
	if !errors.Is(err, operationErr) || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("lost failure: %v", err)
	}
}

func executeJSONCommand(t *testing.T, args []string) (pipeline.JSONExecutionResult, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdout.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = file
	t.Cleanup(func() { os.Stdout = original })
	command := newRootCommand()
	command.SetOut(file)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(args)
	runErr := command.Execute()
	os.Stdout = original
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var result pipeline.JSONExecutionResult
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("stdout must contain JSON only: %q (%v)", data, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		t.Fatalf("extra stdout after result: %q (%v)", data, err)
	}
	return result, runErr
}

func TestVerifyJSONStdout(t *testing.T) {
	for _, tt := range []struct {
		name  string
		mode  string
		flags []string
	}{
		{name: "human", mode: "0"},
		{name: "agent", mode: "1"},
		{name: "verbose", mode: "0", flags: []string{"--verbose"}},
		{name: "debug", mode: "0", flags: []string{"--debug"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, cleanup := setupMainTestEnv(t)
			defer cleanup()
			t.Setenv("AGENT", tt.mode)
			audio := createTestAudio(t, t.TempDir(), "Artist - Track.m4a")
			output, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = output.Close() }()
			stdout := os.Stdout
			os.Stdout = output
			defer func() { os.Stdout = stdout }()
			cmd := newRootCommand()
			args := []string{"verify", audio, "--json"}
			cmd.SetArgs(append(args, tt.flags...))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if _, err := output.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(output)
			if err != nil {
				t.Fatal(err)
			}
			var result pipeline.JSONExecutionResult
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("stdout must contain one JSON result: %v\n%s", err, data)
			}
			if result.Target != audio || result.Status != "success" || result.Verification == nil || result.Verification.DurationSeconds <= 0 {
				t.Fatalf("unexpected verification result: %+v", result)
			}
		})
	}
}
