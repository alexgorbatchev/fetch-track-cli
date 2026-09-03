package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dj/fetch-track-cli/internal/cache"
	"github.com/dj/fetch-track-cli/internal/deps"
	"github.com/dj/fetch-track-cli/internal/downloader"
	"github.com/dj/fetch-track-cli/internal/pipeline"
)

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
		return nil, errors.New("missing")
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

	// 2. Verify with local audio file
	audioFile := createTestAudio(t, t.TempDir(), "Boris Brejcha - Space X (Extended Mix).m4a")
	cmd := newRootCommand()
	cmd.SetArgs([]string{"verify", audioFile})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("verify command error = %v", err)
	}

	// 3. Verify in Agent Mode
	t.Setenv("AGENT", "1")
	cmdAgent := newRootCommand()
	cmdAgent.SetArgs([]string{"verify", audioFile})
	err = cmdAgent.Execute()
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
	dynamicRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		callCount++
		if callCount <= len(deps.RequiredDependencies) {
			// Report missing on first verify
			return nil, errors.New("not found")
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
		return nil, errors.New("missing")
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
