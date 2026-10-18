package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexgorbatchev/fetch-track-cli/internal/cache"
	"github.com/alexgorbatchev/fetch-track-cli/internal/deps"
	"github.com/alexgorbatchev/fetch-track-cli/internal/downloader"
	"github.com/alexgorbatchev/fetch-track-cli/internal/pipeline"
)

func TestRootCommandJSONDownload(t *testing.T) {
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
			outDir := t.TempDir()
			path := filepath.Join(outDir, "Artist - Track.m4a")
			restore := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
				for _, arg := range args {
					if arg == "-o" {
						return nil, os.WriteFile(path, []byte("synthetic audio"), 0o600)
					}
				}
				return []byte(`{"id":"track","title":"Artist - Track","duration":360,"webpage_url":"https://soundcloud.com/artist/track"}`), nil
			})
			defer restore()
			output, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = output.Close() }()
			stdout := os.Stdout
			os.Stdout = output
			defer func() { os.Stdout = stdout }()
			cmd := newRootCommand()
			args := []string{"--json", "--skip-verify", "--skip-metadata", "--confirm-fingerprint=false", "-s", "soundcloud", "-o", outDir, "Artist - Track"}
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
			if result.Status != "success" || result.OutputFile != path || result.SelectedCandidate == nil {
				t.Fatalf("unexpected download result: %+v", result)
			}
			if _, err := os.Stat(result.OutputFile); err != nil {
				t.Fatalf("downloaded file missing: %v", err)
			}
		})
	}
}

func createTestAudio(t *testing.T, dir, filename string) string {
	t.Helper()
	filePath := filepath.Join(dir, filename)
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", "2", "-y", filePath)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg not available: %v", err)
	}
	return filePath
}

func setupMainTestEnv(t *testing.T) (string, func()) {
	t.Helper()
	tempCacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempCacheDir)
	t.Setenv("AGENT", "0")

	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)
	_ = c.Put("deps", "yt-dlp", "2026.08.01", time.Hour)
	_ = c.Put("deps", "ffmpeg", "ffmpeg version 8.1.2", time.Hour)
	_ = c.Put("deps", "ffprobe", "ffprobe version 8.1.2", time.Hour)
	_ = c.Put("deps", "tag-track", "1.0.0", time.Hour)

	cleanupDeps := deps.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "yt-dlp" {
			return []byte("2026.08.01"), nil
		}
		if name == "tag-track" {
			for i, arg := range args {
				if arg == "-o" && i+1 < len(args) {
					outD := args[i+1]
					filePath := filepath.Join(outD, "Boris Brejcha - Space X.m4a")
					_ = os.WriteFile(filePath, []byte("audio"), 0644)
					return []byte(fmt.Sprintf("output: %s\ntitle: Space X\nartist: Boris Brejcha\nalbum: Level One\nyear: 2024\nsource: iTunes API\nDONE: %s\n", filePath, filePath)), nil
				}
			}
			return []byte("1.0.0"), nil
		}
		return []byte(fmt.Sprintf("%s version 8.1.2", name)), nil
	})

	return tempCacheDir, func() {
		cleanupDeps()
	}
}

func TestNewRootCommand_HelpAndVersion(t *testing.T) {
	cmd := newRootCommand()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&outBuf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help command returned error: %v", err)
	}
}

func TestNewRootCommand_AgentHelp(t *testing.T) {
	t.Setenv("AGENT", "1")
	const expectedAlert = "ALERT: Agents must read `AGENT=1 fetch-track skill` before using this tool.\n"

	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"root", []string{"--help"}, "command: fetch-track"},
		{"track", []string{"track", "--help"}, "command: fetch-track track"},
		{"track verify", []string{"track", "verify", "--help"}, "command: fetch-track track verify"},
		{"verify alias", []string{"verify", "--help"}, "command: fetch-track verify"},
		{"dependencies", []string{"dependencies", "--help"}, "command: fetch-track dependencies"},
		{"dependencies install", []string{"dependencies", "install", "--help"}, "command: fetch-track dependencies install"},
		{"upgrade", []string{"upgrade", "--help"}, "command: fetch-track upgrade"},
		{"skill", []string{"skill", "--help"}, "command: fetch-track skill"},
		{"completion", []string{"completion", "--help"}, "command: fetch-track completion"},
		{"help subcommand", []string{"help", "track"}, "command: fetch-track track"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCommand()
			var outBuf bytes.Buffer
			cmd.SetOut(&outBuf)
			cmd.SetErr(&outBuf)
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("agent help command (%v) returned error: %v", tt.args, err)
			}
			if !strings.HasPrefix(outBuf.String(), expectedAlert+tt.want) {
				t.Errorf("expected prefix %q, got:\n%s", expectedAlert+tt.want, outBuf.String())
			}
		})
	}
}

type failingTestWriter struct{}

func (failingTestWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestSkillCommandAndContract(t *testing.T) {
	_, cleanup := setupMainTestEnv(t)
	defer cleanup()

	for _, mode := range []string{"0", "1"} {
		t.Run("AGENT="+mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			cmd := newRootCommand()
			var outBuf, errBuf bytes.Buffer
			cmd.SetOut(&outBuf)
			cmd.SetErr(&errBuf)
			cmd.SetArgs([]string{"skill"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("skill command failed: %v", err)
			}
			if outBuf.String() != skillCommandOutput {
				t.Fatalf("skill output mismatch:\ngot:  %q\nwant: %q", outBuf.String(), skillCommandOutput)
			}
			if errBuf.String() != "" {
				t.Fatalf("unexpected stderr: %q", errBuf.String())
			}
		})
	}

	t.Run("rejects extra arguments", func(t *testing.T) {
		t.Setenv("AGENT", "0")
		cmd := newRootCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"skill", "extra"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error when passing extra arguments to skill command")
		}
	})

	t.Run("propagates write error", func(t *testing.T) {
		t.Setenv("AGENT", "0")
		cmd := newRootCommand()
		cmd.SetOut(failingTestWriter{})
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"skill"})
		if err := cmd.Execute(); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("expected io.ErrClosedPipe, got %v", err)
		}
	})

	t.Run("human help and version omit alert", func(t *testing.T) {
		t.Setenv("AGENT", "0")
		cmd := newRootCommand()
		var outBuf bytes.Buffer
		cmd.SetOut(&outBuf)
		cmd.SetArgs([]string{"--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(outBuf.String(), "ALERT:") {
			t.Fatalf("human help must not include agent alert:\n%s", outBuf.String())
		}

		t.Setenv("AGENT", "1")
		outBuf.Reset()
		cmdVer := newRootCommand()
		cmdVer.SetOut(&outBuf)
		cmdVer.SetArgs([]string{"--version"})
		if err := cmdVer.Execute(); err != nil {
			t.Fatal(err)
		}
		if outBuf.String() != version+"\n" || strings.Contains(outBuf.String(), "ALERT:") {
			t.Fatalf("unexpected --version output: %q", outBuf.String())
		}
	})

	t.Run("skill file exists and matches live CLI interface", func(t *testing.T) {
		if !strings.Contains(skillCommandOutput, "--skill fetch-track") || !strings.Contains(skillCommandOutput, "/skills/fetch-track\n") {
			t.Fatalf("skillCommandOutput missing expected skill identifier or path:\n%s", skillCommandOutput)
		}
		skillPath := filepath.Join("..", "..", "skills", "fetch-track", "SKILL.md")
		skillBytes, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("reading %s: %v", skillPath, err)
		}
		skillDoc := string(skillBytes)
		if !strings.Contains(skillDoc, "\nname: fetch-track\n") {
			t.Fatalf("SKILL.md frontmatter name must be fetch-track")
		}
		if omissions := cobrahelptree.SkillOmissions(newRootCommand(), buildTechCatalog(), skillDoc); len(omissions) > 0 {
			t.Fatalf("SKILL.md is missing CLI interface elements: %v", omissions)
		}
	})
}

func TestNewRootCommand_NoArgs(t *testing.T) {
	cmd := newRootCommand()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("no args command returned error: %v", err)
	}
}

func TestMainFunction(t *testing.T) {
	_, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()

	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"fetch-track", "--help"}
	main()
}

func TestRunHelper(t *testing.T) {
	_, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()
	ctx := context.Background()

	err := run(ctx, []string{"--help"})
	if err != nil {
		t.Fatalf("run with --help failed: %v", err)
	}
}

func TestDepsCommand_NonAgent(t *testing.T) {
	_, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "0")

	cmd := newRootCommand()
	cmd.SetArgs([]string{"dependencies"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("dependencies command error = %v", err)
	}
}

func TestDepsCommand_NonAgentOutdated(t *testing.T) {
	tempCacheDir, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "0")

	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)
	_ = c.Delete("deps", "yt-dlp")
	outdatedRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "yt-dlp" {
			return []byte("2020.01.01"), nil
		}
		if name == "tag-track" {
			return []byte("8.1.2"), nil
		}
		return []byte(fmt.Sprintf("%s version 8.1.2", name)), nil
	}
	cleanupOutdated := deps.SetDefaultRunner(outdatedRunner)
	defer cleanupOutdated()

	cmd := newRootCommand()
	cmd.SetArgs([]string{"dependencies"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for outdated dependency in non-agent mode")
	}
	if !strings.Contains(err.Error(), "yt-dlp in $PATH (version 2020.01.01) is outdated, must be version 2024.08.01 or newer") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDepsCommand_AgentMode(t *testing.T) {
	tempCacheDir, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "1")

	cmd := newRootCommand()
	cmd.SetArgs([]string{"deps"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("deps command agent mode error = %v", err)
	}

	// Outdated tool in agent mode
	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)
	_ = c.Delete("deps", "yt-dlp")
	outdatedRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "yt-dlp" {
			return []byte("2020.01.01"), nil
		}
		if name == "tag-track" {
			return []byte("8.1.2"), nil
		}
		return []byte(fmt.Sprintf("%s version 8.1.2", name)), nil
	}
	cleanupOutdated := deps.SetDefaultRunner(outdatedRunner)
	cmdOutdated := newRootCommand()
	cmdOutdated.SetArgs([]string{"deps"})
	_ = cmdOutdated.Execute()
	cleanupOutdated()
}

func TestDepsCommand_Failure(t *testing.T) {
	tempCacheDir, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "0")

	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)
	_ = c.Delete("deps", "yt-dlp")
	_ = c.Delete("deps", "ffmpeg")
	_ = c.Delete("deps", "ffprobe")
	_ = c.Delete("deps", "tag-track")

	cleanupFail := deps.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
	})
	defer cleanupFail()

	cmd := newRootCommand()
	cmd.SetArgs([]string{"dependencies"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error from dependencies command on failure")
	}

	t.Setenv("AGENT", "1")
	cmd2 := newRootCommand()
	cmd2.SetArgs([]string{"deps"})
	err = cmd2.Execute()
	if err == nil {
		t.Fatal("expected error from deps in agent mode on failure")
	}
}

func TestDepsInstallCommand(t *testing.T) {
	tempCacheDir, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()

	// 1. When all already satisfied
	cmd := newRootCommand()
	cmd.SetArgs([]string{"deps", "install"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("deps install error = %v", err)
	}

	// 2. When tools are missing and get installed
	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)
	_ = c.Delete("deps", "yt-dlp")
	_ = c.Delete("deps", "ffmpeg")
	_ = c.Delete("deps", "ffprobe")
	_ = c.Delete("deps", "tag-track")

	cmdMissing := newRootCommand()
	cmdMissing.SetArgs([]string{"deps", "install"})
	_ = cmdMissing.Execute()

	// Specific dep arg
	cmd2 := newRootCommand()
	cmd2.SetArgs([]string{"deps", "install", "ffmpeg"})
	err = cmd2.Execute()
	if err != nil {
		t.Fatalf("deps install specific error = %v", err)
	}

	// Unknown dep error
	cmd3 := newRootCommand()
	cmd3.SetArgs([]string{"deps", "install", "unknown-tool"})
	err = cmd3.Execute()
	if err == nil {
		t.Fatal("expected error on unknown tool install")
	}
}

func TestDepsUpdateCommand(t *testing.T) {
	tempCacheDir, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()
	_ = tempCacheDir

	// Specific dep arg
	cmd2 := newRootCommand()
	cmd2.SetArgs([]string{"deps", "update", "ffmpeg"})
	err := cmd2.Execute()
	if err != nil {
		t.Fatalf("deps update specific error = %v", err)
	}

	// Unknown dep error
	cmd3 := newRootCommand()
	cmd3.SetArgs([]string{"deps", "update", "unknown-tool"})
	err = cmd3.Execute()
	if err == nil {
		t.Fatal("expected error on unknown tool update")
	}
}

func TestUpgradeCommand(t *testing.T) {
	tempCacheDir, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()
	_ = tempCacheDir

	// 1. Up to date (when already at latest version)
	cleanupUpToDate := deps.SetUpgradeSelfFunc(func(ctx context.Context, owner, repo, currentVersion string) (string, error) {
		return "", fmt.Errorf("already at the latest version (%s)", currentVersion)
	})
	cmd := newRootCommand()
	cmd.SetArgs([]string{"upgrade"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected nil on already up to date, got: %v", err)
	}
	cleanupUpToDate()

	// 2. Upgraded successfully
	cleanupSuccess := deps.SetUpgradeSelfFunc(func(ctx context.Context, owner, repo, currentVersion string) (string, error) {
		return "1.5.0", nil
	})
	cmd2 := newRootCommand()
	cmd2.SetArgs([]string{"upgrade"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("expected nil on successful upgrade, got: %v", err)
	}
	cleanupSuccess()

	// 3. Upgrade failure
	cleanupFail := deps.SetUpgradeSelfFunc(func(ctx context.Context, owner, repo, currentVersion string) (string, error) {
		return "", errors.New("network failure")
	})
	defer cleanupFail()

	cmd3 := newRootCommand()
	cmd3.SetArgs([]string{"upgrade"})
	err := cmd3.Execute()
	if err == nil {
		t.Fatal("expected error on upgrade failure")
	}
}

func TestVerifyCommand(t *testing.T) {
	_, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()

	// 1. Verify with no args
	cmdNoArgs := newRootCommand()
	cmdNoArgs.SetArgs([]string{"verify"})
	_ = cmdNoArgs.Execute()

	// 2. Verify with local audio file (via track verify and verify alias)
	audioFile := createTestAudio(t, t.TempDir(), "Boris Brejcha - Space X (Extended Mix).m4a")
	for _, args := range [][]string{{"track", "verify", audioFile}, {"verify", audioFile}} {
		cmd := newRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("verify command (%v) error = %v", args, err)
		}
	}

	// 3. Verify in Agent Mode
	t.Setenv("AGENT", "1")
	cmdAgent := newRootCommand()
	cmdAgent.SetArgs([]string{"track", "verify", audioFile})
	err := cmdAgent.Execute()
	if err != nil {
		t.Fatalf("verify command agent mode error = %v", err)
	}

	// 4. Verify with error
	cmdErr := newRootCommand()
	cmdErr.SetArgs([]string{"verify", "/nonexistent/path.m4a"})
	err = cmdErr.Execute()
	if err == nil {
		t.Fatal("expected error on nonexistent verify file")
	}
}

func TestRootCommand_Execution(t *testing.T) {
	tempCacheDir, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "0")

	outDir := t.TempDir()
	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)
	metaRes := pipeline.TrackMetadataResult{
		Title:       "Space X",
		Artist:      "Boris Brejcha",
		Album:       "Space X Single",
		ReleaseYear: "2024",
		Genre:       "Minimal Techno",
		Source:      "iTunes API",
	}
	_ = c.Put("metadata", "Space X", metaRes, time.Hour)
	_ = c.Put("metadata", "Boris Brejcha - Space X", metaRes, time.Hour)
	_ = c.Put("metadata", "Boris Brejcha - Space X (Extended Mix)", metaRes, time.Hour)

	jsonSearch := `{"id":"spx1","title":"Boris Brejcha - Space X (Extended Mix)","duration":2,"webpage_url":"https://soundcloud.com/boris-brejcha/space-x"}` + "\n"
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "-o" {
				createTestAudio(t, outDir, "Boris Brejcha - Space X (Extended Mix).m4a")
				return []byte("downloaded"), nil
			}
		}
		return []byte(jsonSearch), nil
	}

	cleanupDl := downloader.SetDefaultRunner(mockRunner)
	defer cleanupDl()

	// 1. With progress-target
	cmd := newRootCommand()
	cmd.SetArgs([]string{"-o", outDir, "-s", "soundcloud", "--skip-metadata", "--progress-target", "stdout", "Boris Brejcha - Space X"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("root command execution error = %v", err)
	}

	// 2. With progress-socket
	outDir2 := t.TempDir()
	cmd2 := newRootCommand()
	cmd2.SetArgs([]string{"-o", outDir2, "-s", "soundcloud", "--skip-metadata", "--progress-socket", "stderr", "Boris Brejcha - Space X"})
	mockRunner2 := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "-o" {
				createTestAudio(t, outDir2, "Boris Brejcha - Space X (Extended Mix).m4a")
				return []byte("downloaded"), nil
			}
		}
		return []byte(jsonSearch), nil
	}
	cleanupDl2 := downloader.SetDefaultRunner(mockRunner2)
	defer cleanupDl2()

	err = cmd2.Execute()
	if err != nil {
		t.Fatalf("root command with progress-socket error = %v", err)
	}

	// 3. With FETCH_TRACK_PROGRESS_TARGET env var
	t.Setenv("FETCH_TRACK_PROGRESS_TARGET", "stdout")
	cmdEnv := newRootCommand()
	cmdEnv.SetArgs([]string{"-o", outDir, "-s", "soundcloud", "--skip-metadata", "Boris Brejcha - Space X"})
	_ = cmdEnv.Execute()
}

func TestEnsureDependencies_Branches(t *testing.T) {
	tempCacheDir, cleanupEnv := setupMainTestEnv(t)
	defer cleanupEnv()
	ctx := context.Background()

	// 1. All satisfied
	err := ensureDependencies(ctx)
	if err != nil {
		t.Fatalf("expected nil when all satisfied, got %v", err)
	}

	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)

	// 2. Auto-install branch when install succeeds with installed tools
	_ = c.Delete("deps", "yt-dlp")
	_ = c.Delete("deps", "ffmpeg")
	_ = c.Delete("deps", "ffprobe")
	_ = c.Delete("deps", "tag-track")

	var callCount int
	var mu sync.Mutex
	dynamicRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		mu.Lock()
		callCount++
		firstVerify := callCount <= len(deps.RequiredDependencies)
		mu.Unlock()
		if firstVerify {
			// Report missing on first verify
			return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
		}
		if name == "yt-dlp" {
			return []byte("2026.08.01"), nil
		}
		if name == "tag-track" {
			return []byte("1.0.0"), nil
		}
		return []byte(fmt.Sprintf("%s version 8.1.2", name)), nil
	}
	cleanupDynamic := deps.SetDefaultRunner(dynamicRunner)

	autoInstall = true
	_ = ensureDependencies(ctx)
	autoInstall = false
	cleanupDynamic()

	// Set runner to report missing tools
	failRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	cleanupFailDeps := deps.SetDefaultRunner(failRunner)

	// 3. Auto-install failure
	_ = c.Delete("deps", "yt-dlp")
	_ = c.Delete("deps", "ffmpeg")
	_ = c.Delete("deps", "ffprobe")
	_ = c.Delete("deps", "tag-track")
	autoInstall = true
	_ = ensureDependencies(ctx)
	autoInstall = false

	// 4. User prompt branch - user answers yes with failure
	_ = c.Delete("deps", "yt-dlp")
	_ = c.Delete("deps", "ffmpeg")
	_ = c.Delete("deps", "ffprobe")
	_ = c.Delete("deps", "tag-track")
	stdinReader = strings.NewReader("y\n")
	_ = ensureDependencies(ctx)

	// 5. User prompt branch - user answers no
	_ = c.Delete("deps", "yt-dlp")
	_ = c.Delete("deps", "ffmpeg")
	_ = c.Delete("deps", "ffprobe")
	_ = c.Delete("deps", "tag-track")
	stdinReader = strings.NewReader("n\n")
	_ = ensureDependencies(ctx)

	cleanupFailDeps()

	// 6. User prompt branch - user answers yes with success
	_ = c.Delete("deps", "yt-dlp")
	_ = c.Delete("deps", "ffmpeg")
	_ = c.Delete("deps", "ffprobe")
	_ = c.Delete("deps", "tag-track")
	stdinReader = strings.NewReader("yes\n")
	_ = ensureDependencies(ctx)

	// 7. Outdated dependency prompt formatting
	_ = c.Delete("deps", "yt-dlp")
	outdatedRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "yt-dlp" {
			return []byte("2020.01.01"), nil
		}
		if name == "tag-track" {
			return []byte("8.1.2"), nil
		}
		return []byte(fmt.Sprintf("%s version 8.1.2", name)), nil
	}
	cleanupOutdated := deps.SetDefaultRunner(outdatedRunner)
	defer cleanupOutdated()
	stdinReader = strings.NewReader("n\n")
	errOutdated := ensureDependencies(ctx)
	if errOutdated == nil {
		t.Fatal("expected error when user declines install on outdated dependency")
	}
	if !strings.Contains(errOutdated.Error(), "yt-dlp in $PATH (version 2020.01.01) is outdated, must be version 2024.08.01 or newer") {
		t.Errorf("unexpected error from ensureDependencies: %v", errOutdated)
	}
}

func TestHomeDirectoryFoldingInCLIOutput(t *testing.T) {
	_, cleanup := setupMainTestEnv(t)
	defer cleanup()

	homeDir := filepath.Join(t.TempDir(), "userhome")
	tracksDir := filepath.Join(homeDir, "tracks")
	if err := os.MkdirAll(tracksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("AGENT", "1")

	missingPath := filepath.Join(tracksDir, "Missing.m4a")
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()

	oldStdout := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = oldStdout }()

	cmd := newRootCommand()
	cmd.SetArgs([]string{"track", "verify", missingPath})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "~/tracks/Missing.m4a") || strings.Contains(err.Error(), homeDir) {
		t.Fatalf("expected error to fold home directory to ~, got: %v", err)
	}

	if _, err := output.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(output)
	if err != nil {
		t.Fatal(err)
	}
	stdoutStr := string(data)
	if !strings.Contains(stdoutStr, "target: ~/tracks/Missing.m4a") || strings.Contains(stdoutStr, homeDir) {
		t.Fatalf("expected stdout to fold home directory to ~, got:\n%s", stdoutStr)
	}
}
