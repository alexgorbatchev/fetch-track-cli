package deps

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
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
		{"invalid version format", "invalid", "4.4", true},
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
		if !strings.Contains(err.Error(), "ffmpeg is missing in $PATH, install version 4.4 or newer") {
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
		if !strings.Contains(err.Error(), "yt-dlp in $PATH must be version 2024.08.01 or newer") {
			t.Errorf("unexpected error message: %v", err)
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

func TestCheckDependenciesLive(t *testing.T) {
	ctx := context.Background()
	// Test CheckDependencies against host environment if dependencies are present
	err := CheckDependencies(ctx)
	if err != nil {
		t.Logf("CheckDependencies returned: %v (this is expected if dependencies are missing in the test runner)", err)
	}
}
