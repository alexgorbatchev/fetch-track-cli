package deps

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dj/fetch-track-cli/internal/cache"
)

func TestParseVersionOutput(t *testing.T) {
	tests := []struct {
		name     string
		depName  string
		input    string
		expected string
	}{
		{
			name:     "yt-dlp standard date version",
			depName:  "yt-dlp",
			input:    "2026.07.04\n",
			expected: "2026.07.04",
		},
		{
			name:     "yt-dlp date with build number",
			depName:  "yt-dlp",
			input:    "2024.08.01.1234",
			expected: "2024.08.01.1234",
		},
		{
			name:     "ffmpeg standard version line",
			depName:  "ffmpeg",
			input:    "ffmpeg version 8.1.2 Copyright (c) 2000-2026 the FFmpeg developers\nbuilt with Apple clang...",
			expected: "8.1.2",
		},
		{
			name:     "ffmpeg prefixed with n",
			depName:  "ffmpeg",
			input:    "ffmpeg version n5.1.2 Copyright (c) 2000-2023",
			expected: "5.1.2",
		},
		{
			name:     "ffprobe standard version line",
			depName:  "ffprobe",
			input:    "ffprobe version 4.4.2-0ubuntu0.22.04.1 Copyright...",
			expected: "4.4.2-0ubuntu0.22.04.1",
		},
		{
			name:     "ffmpeg nightly build",
			depName:  "ffmpeg",
			input:    "ffmpeg version N-112345-g1234567890 Copyright...",
			expected: "N-112345-g1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseVersionOutput(tt.depName, tt.input)
			if got != tt.expected {
				t.Errorf("ParseVersionOutput(%q, %q) = %q; want %q", tt.depName, tt.input, got, tt.expected)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name       string
		actual     string
		minVersion string
		wantErr    bool
	}{
		{"yt-dlp newer version", "2026.07.04", "2024.08.01", false},
		{"yt-dlp exact min version", "2024.08.01", "2024.08.01", false},
		{"yt-dlp older version", "2023.03.04", "2024.08.01", true},
		{"ffmpeg newer version", "8.1.2", "4.4", false},
		{"ffmpeg exact version", "4.4", "4.4", false},
		{"ffmpeg patch release", "4.4.1", "4.4", false},
		{"ffmpeg older major", "3.4.11", "4.4", true},
		{"ffmpeg older minor", "4.3.2", "4.4", true},
		{"ffmpeg nightly build N-", "N-112345", "4.4", false},
		{"ffmpeg git build", "git-2023-01-01", "4.4", false},
		{"dev build version", "dev", "4.4", false},
		{"DEV- prefixed version", "DEV-1234", "4.4", false},
		{"build metadata with plus", "4.4.0+build123", "4.4", false},
		{"older minor", "1.2", "4.4", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CompareVersions(tt.actual, tt.minVersion)
			if (err != nil) != tt.wantErr {
				t.Errorf("CompareVersions(%q, %q) error = %v, wantErr %v", tt.actual, tt.minVersion, err, tt.wantErr)
			}
		})
	}
}

func TestVerifyDependenciesWithRunner(t *testing.T) {
	ctx := context.Background()

	t.Run("returns reports for all dependencies", func(t *testing.T) {
		mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			switch name {
			case "yt-dlp":
				return []byte("2026.07.04\n"), nil
			case "ffmpeg":
				return []byte("ffmpeg version 8.1.2 Copyright...\n"), nil
			case "ffprobe":
				return nil, &exec.Error{Name: "ffprobe", Err: exec.ErrNotFound}
			default:
				return nil, errors.New("unknown tool")
			}
		}

		reports, err := VerifyDependenciesWithRunner(ctx, mockRunner, nil, RequiredDependencies...)
		if err == nil {
			t.Fatal("expected error due to missing ffprobe")
		}
		if len(reports) != 3 {
			t.Fatalf("expected 3 reports, got %d", len(reports))
		}

		if !reports[0].Satisfied || reports[0].Name != "yt-dlp" {
			t.Errorf("expected yt-dlp to be satisfied, got %+v", reports[0])
		}
		if !reports[1].Satisfied || reports[1].Name != "ffmpeg" {
			t.Errorf("expected ffmpeg to be satisfied, got %+v", reports[1])
		}
		if reports[2].Satisfied || reports[2].Installed || reports[2].Name != "ffprobe" {
			t.Errorf("expected ffprobe to fail/missing, got %+v", reports[2])
		}
	})
}

func TestCheckDependenciesWithRunner(t *testing.T) {
	ctx := context.Background()

	t.Run("all dependencies met", func(t *testing.T) {
		mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			switch name {
			case "yt-dlp":
				return []byte("2026.07.04\n"), nil
			case "ffmpeg":
				return []byte("ffmpeg version 8.1.2 Copyright...\n"), nil
			case "ffprobe":
				return []byte("ffprobe version 8.1.2 Copyright...\n"), nil
			default:
				return nil, errors.New("unknown tool")
			}
		}

		err := CheckDependenciesWithRunner(ctx, mockRunner, nil, RequiredDependencies...)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("agent mode messages", func(t *testing.T) {
		t.Setenv("AGENT", "1")

		mockRunnerMissing := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
		}

		err := CheckDependenciesWithRunner(ctx, mockRunnerMissing, nil, Dependency{Name: "yt-dlp", MinVersion: "2024.08.01"})
		if err == nil || !strings.Contains(err.Error(), "yt-dlp is missing in $PATH. Ask user for confirmation to install yt-dlp version 2024.08.01 or newer.") {
			t.Errorf("unexpected agent missing error: %v", err)
		}

		mockRunnerOutdated := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("2023.03.04"), nil
		}

		err = CheckDependenciesWithRunner(ctx, mockRunnerOutdated, nil, Dependency{Name: "yt-dlp", MinVersion: "2024.08.01"})
		if err == nil || !strings.Contains(err.Error(), "yt-dlp version 2023.03.04 in $PATH is outdated. Ask user for confirmation to update yt-dlp to version 2024.08.01 or newer.") {
			t.Errorf("unexpected agent outdated error: %v", err)
		}
	})

	t.Run("missing dependency", func(t *testing.T) {
		t.Setenv("AGENT", "0")
		mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "ffmpeg" {
				return nil, &exec.Error{Name: "ffmpeg", Err: exec.ErrNotFound}
			}
			return []byte("2026.07.04"), nil
		}

		err := CheckDependenciesWithRunner(ctx, mockRunner, nil, RequiredDependencies...)
		if err == nil {
			t.Fatal("expected error for missing ffmpeg, got nil")
		}
		if !strings.Contains(err.Error(), "ffmpeg is missing in $PATH") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("dependency version below minimum", func(t *testing.T) {
		t.Setenv("AGENT", "0")
		mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "yt-dlp" {
				return []byte("2023.03.04"), nil
			}
			return []byte("ffmpeg version 8.1.2"), nil
		}

		err := CheckDependenciesWithRunner(ctx, mockRunner, nil, RequiredDependencies...)
		if err == nil {
			t.Fatal("expected error for outdated yt-dlp, got nil")
		}
		expectedMsg := "yt-dlp in $PATH (version 2023.03.04) is outdated, must be version 2024.08.01 or newer"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Errorf("expected error to contain %q, got: %v", expectedMsg, err)
		}
	})

	t.Run("command execution error", func(t *testing.T) {
		mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, errors.New("permission denied")
		}

		dep := Dependency{Name: "yt-dlp", MinVersion: "2024.08.01"}
		err := CheckDependency(ctx, mockRunner, dep)
		if err == nil {
			t.Fatal("expected error on command failure")
		}
		if !strings.Contains(err.Error(), "failed to check yt-dlp version") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("unparseable version output", func(t *testing.T) {
		mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte(""), nil
		}

		dep := Dependency{Name: "yt-dlp", MinVersion: "2024.08.01"}
		err := CheckDependency(ctx, mockRunner, dep)
		if err == nil {
			t.Fatal("expected error for unparseable version")
		}
		if !strings.Contains(err.Error(), "could not parse yt-dlp version output") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestCleanErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty message",
			input:    "",
			expected: "",
		},
		{
			name:     "simple error without trace",
			input:    "permission denied",
			expected: "permission denied",
		},
		{
			name: "python traceback",
			input: `Traceback (most recent call last):
  File "/usr/local/bin/yt-dlp", line 8, in <module>
    sys.exit(main())
  File "/usr/local/lib/python3.10/site-packages/yt_dlp/__init__.py", line 1024, in main
yt_dlp.utils.DownloadError: ERROR: [youtube] 12345: Video unavailable`,
			expected: "yt_dlp.utils.DownloadError: ERROR: [youtube] 12345: Video unavailable",
		},
		{
			name: "go panic stack trace",
			input: `panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: code=0x1 addr=0x0 pc=0x102f8a4]
goroutine 1 [running]:
main.main()
	/home/user/app/main.go:42 +0x2b`,
			expected: "panic: runtime error: invalid memory address or nil pointer dereference",
		},
		{
			name: "node stack trace",
			input: `Error: Cannot find module 'ffmpeg'
    at Function.Module._resolveFilename (internal/modules/cjs/loader.js:880:15)
    at Function.Module._load (internal/modules/cjs/loader.js:725:27)
    at Function.executeUserEntryPoint [as runMain] (internal/modules/run_main.js:72:12)`,
			expected: "Error: Cannot find module 'ffmpeg'",
		},
		{
			name: "multiline ffmpeg warning with final error",
			input: `ffmpeg version 4.4.2 Copyright (c) 2000-2022
[tls @ 0x7fa890] [error] Connection refused
Error opening input: Connection refused`,
			expected: "Error opening input: Connection refused",
		},
		{
			name:     "whitespace only",
			input:    "   \n\t  ",
			expected: "",
		},
		{
			name:     "c++ stack frame",
			input:    "#0  0x00007fff89123 in crash_handler ()\n#1  0x00007fff89456 in main ()\nFatal error: segmentation fault",
			expected: "Fatal error: segmentation fault",
		},
		{
			name:     "yt-dlp extractor error prefix",
			input:    "yt_dlp.utils.ExtractorError: could not extract stream URL",
			expected: "yt_dlp.utils.ExtractorError: could not extract stream URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanErrorMessage(tt.input)
			if got != tt.expected {
				t.Errorf("CleanErrorMessage(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestVerifyDependenciesWithRunner_NoStackTraces(t *testing.T) {
	ctx := context.Background()
	pythonTracebackRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New(`Traceback (most recent call last):
  File "/usr/local/bin/yt-dlp", line 8, in <module>
    sys.exit(main())
yt_dlp.utils.DownloadError: binary crashed`)
	}

	reports, err := VerifyDependenciesWithRunner(ctx, pythonTracebackRunner, nil, Dependency{Name: "yt-dlp", MinVersion: "2024.08.01"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "Traceback") || strings.Contains(err.Error(), "File \"") {
		t.Errorf("error contains stack trace: %v", err)
	}
	if len(reports) > 0 && (strings.Contains(reports[0].Error, "Traceback") || strings.Contains(reports[0].Error, "File \"")) {
		t.Errorf("report.Error contains stack trace: %v", reports[0].Error)
	}
}

func TestCheckDependencies_Variants(t *testing.T) {
	ctx := context.Background()
	c, _ := cache.New(false)
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("1.0.0"), nil
	}
	_ = CheckDependenciesWithRunner(ctx, mockRunner, c)
	_, _ = VerifyDependenciesWithRunner(ctx, mockRunner, c)
}

func TestDefaultRunner_Stderr(t *testing.T) {
	ctx := context.Background()
	_, err := DefaultRunner(ctx, "sh", "-c", "echo error message >&2; exit 1")
	if err == nil || !strings.Contains(err.Error(), "error message") {
		t.Errorf("expected stderr in error message, got: %v", err)
	}
}

func TestDefaultRunner(t *testing.T) {
	ctx := context.Background()
	// Run a common command like 'go' version
	out, err := DefaultRunner(ctx, "go", "version")
	if err != nil {
		t.Skipf("go command not available in test environment: %v", err)
	}
	if !strings.Contains(string(out), "go version") {
		t.Errorf("expected output to contain 'go version', got %q", string(out))
	}
}

func TestCheckDependencies_Cached(t *testing.T) {
	ctx := context.Background()
	c := cache.NewInDir(t.TempDir(), true)
	_ = c.Put("deps", "yt-dlp", "2026.08.01", time.Hour)
	_ = c.Put("deps", "ffmpeg", "ffmpeg version 8.1", time.Hour)
	_ = c.Put("deps", "ffprobe", "ffprobe version 8.1", time.Hour)

	err := CheckDependencies(ctx, c)
	if err != nil {
		t.Fatalf("CheckDependencies with cache error = %v", err)
	}

	reports, err := VerifyDependencies(ctx, c)
	if err != nil {
		t.Fatalf("VerifyDependencies with cache error = %v", err)
	}
	if len(reports) != 3 {
		t.Fatalf("expected 3 reports, got %d", len(reports))
	}
}

func TestSetDefaultRunner(t *testing.T) {
	cleanup := SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("test"), nil
	})
	defer cleanup()
}

func TestCheckDependencies_DefaultRunner(t *testing.T) {
	ctx := context.Background()
	cleanup := SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "yt-dlp" {
			return []byte("2026.08.01"), nil
		}
		return []byte(fmt.Sprintf("%s version 8.1.2", name)), nil
	})
	defer cleanup()

	_ = CheckDependencies(ctx)
	_, _ = VerifyDependencies(ctx)
}

func TestManagerOperations(t *testing.T) {
	ctx := context.Background()
	c := cache.NewInDir(t.TempDir(), true)

	// Test cacheAdapter Get/Put/Delete
	adapter := &cacheAdapter{cache: c}
	_ = adapter.Put("test_key", "test_val")
	var val string
	if !adapter.Get("test_key", &val) || val != "test_val" {
		t.Errorf("cacheAdapter Get failed, got %q", val)
	}
	_ = adapter.Delete("test_key")
	if adapter.Get("test_key", &val) {
		t.Errorf("expected deleted key to return false")
	}

	// Nil cacheAdapter safety
	var nilAdapter *cacheAdapter
	if nilAdapter.Get("k", &val) {
		t.Errorf("nil adapter Get should return false")
	}
	_ = nilAdapter.Put("k", "v")
	_ = nilAdapter.Delete("k")

	// InitManagedPath
	if err := InitManagedPath(); err != nil {
		t.Fatalf("InitManagedPath failed: %v", err)
	}

	// Install/Update unknown dependency returns error
	if err := InstallDependency(ctx, "unknown-dep-xyz"); err == nil {
		t.Error("expected error installing unknown dep")
	}
	if err := UpdateDependency(ctx, "unknown-dep-xyz"); err == nil {
		t.Error("expected error updating unknown dep")
	}

	// InstallMissingDependencies & UpdateAllDependencies with mock
	cleanup := SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("2026.08.01"), nil
	})
	defer cleanup()

	_, _ = InstallMissingDependencies(ctx)
	_, _ = UpdateAllDependencies(ctx)

	// UpgradeSelf with invalid repo / dev version
	_, _ = UpgradeSelf(ctx, "dev")
	_, _ = UpgradeSelfToPath(ctx, "dev", filepath.Join(t.TempDir(), "bin"))
}
