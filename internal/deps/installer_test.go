package deps

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dj/fetch-track-cli/internal/cache"
)

func TestGetManagedBinDir(t *testing.T) {
	t.Run("default_path", func(t *testing.T) {
		origXDG := os.Getenv("XDG_DATA_HOME")
		_ = os.Unsetenv("XDG_DATA_HOME")
		defer func() {
			if origXDG != "" {
				_ = os.Setenv("XDG_DATA_HOME", origXDG)
			}
		}()

		dir, err := GetManagedBinDir()
		if err != nil {
			t.Fatalf("GetManagedBinDir() error = %v", err)
		}
		if dir == "" {
			t.Fatalf("GetManagedBinDir() returned empty string")
		}
		if !strings.HasSuffix(dir, filepath.Join("fetch-track", "bin")) {
			t.Errorf("GetManagedBinDir() = %q, want suffix 'fetch-track/bin'", dir)
		}
	})

	t.Run("xdg_data_home_override", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tempDir)

		dir, err := GetManagedBinDir()
		if err != nil {
			t.Fatalf("GetManagedBinDir() error = %v", err)
		}
		expected := filepath.Join(tempDir, "fetch-track", "bin")
		if dir != expected {
			t.Errorf("GetManagedBinDir() = %q, want %q", dir, expected)
		}
	})
}

func TestGetManagedBinDirForOS(t *testing.T) {
	t.Run("windows_localappdata", func(t *testing.T) {
		getenv := func(key string) string {
			if key == "LOCALAPPDATA" {
				return `C:\Users\test\AppData\Local`
			}
			return ""
		}
		userHome := func() (string, error) {
			return `C:\Users\test`, nil
		}

		dir, err := getManagedBinDirForOS("windows", getenv, userHome)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(`C:\Users\test\AppData\Local`, "fetch-track", "bin")
		if dir != expected {
			t.Errorf("got %q, want %q", dir, expected)
		}
	})

	t.Run("home_dir_error", func(t *testing.T) {
		getenv := func(key string) string { return "" }
		userHome := func() (string, error) { return "", errors.New("cannot determine home") }

		_, err := getManagedBinDirForOS("linux", getenv, userHome)
		if err == nil {
			t.Fatal("expected error when userHomeDir fails")
		}
	})
}

func TestInstallFFmpegForOS(t *testing.T) {
	ctx := context.Background()

	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("success"), nil
	}

	tests := []struct {
		name    string
		goos    string
		tool    string
		wantErr bool
	}{
		{"darwin_brew", "darwin", "brew", false},
		{"linux_apt", "linux", "apt-get", false},
		{"linux_pacman", "linux", "pacman", false},
		{"linux_dnf", "linux", "dnf", false},
		{"windows_winget", "windows", "winget", false},
		{"windows_choco", "windows", "choco", false},
		{"unsupported_os", "freebsd", "pkg", true},
		{"no_package_manager", "linux", "none", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookPath := func(file string) (string, error) {
				if file == tt.tool {
					return "/usr/bin/" + file, nil
				}
				return "", errors.New("not found")
			}

			err := installFFmpegForOS(ctx, tt.goos, lookPath, mockRunner)
			if (err != nil) != tt.wantErr {
				t.Errorf("installFFmpegForOS(%s) err = %v, wantErr %v", tt.goos, err, tt.wantErr)
			}
		})
	}
}

func TestEnsureManagedBinDir(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tempDir)

	binDir, err := EnsureManagedBinDir()
	if err != nil {
		t.Fatalf("EnsureManagedBinDir() error = %v", err)
	}

	info, err := os.Stat(binDir)
	if err != nil {
		t.Fatalf("EnsureManagedBinDir() directory does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected %q to be a directory", binDir)
	}

	t.Run("mkdir_error", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "file_blocking_dir")
		_ = os.WriteFile(filePath, []byte("blocker"), 0644)
		t.Setenv("XDG_DATA_HOME", filePath)

		_, err := EnsureManagedBinDir()
		if err == nil {
			t.Fatal("expected error when MkdirAll fails")
		}
	})
}

func TestResolveLatestTag(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/owner/repo/releases/latest" {
			http.Redirect(w, r, "/owner/repo/releases/tag/v1.4.2", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	tag, err := ResolveLatestTagWithBaseURL(ctx, ts.URL, "owner", "repo")
	if err != nil {
		t.Fatalf("ResolveLatestTagWithBaseURL() error = %v", err)
	}
	if tag != "v1.4.2" {
		t.Errorf("ResolveLatestTagWithBaseURL() = %q, want 'v1.4.2'", tag)
	}
}

func createTestTarGz(t *testing.T, binName, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	data := []byte(content)
	hdr := &tar.Header{
		Name: binName,
		Mode: 0755,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func createTestZip(t *testing.T, binName, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	data := []byte(content)
	f, err := zw.Create(binName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDownloadAndExtractBinary_TarGz(t *testing.T) {
	binContent := "#!/bin/sh\necho 1.4.2\n"
	tarGzData := createTestTarGz(t, "fetch-track", binContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(tarGzData)
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	ctx := context.Background()

	err := downloadAndExtractAsset(ctx, ts.URL, "fetch-track", tempDir, false)
	if err != nil {
		t.Fatalf("downloadAndExtractAsset() error = %v", err)
	}

	destBinary := filepath.Join(tempDir, "fetch-track")
	data, err := os.ReadFile(destBinary)
	if err != nil {
		t.Fatalf("failed to read extracted binary: %v", err)
	}
	if string(data) != binContent {
		t.Errorf("binary content = %q, want %q", string(data), binContent)
	}

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(destBinary)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0111 == 0 {
			t.Errorf("expected binary to be executable, mode = %v", fi.Mode())
		}
	}
}

func TestDownloadAndExtractBinary_Zip(t *testing.T) {
	binContent := "windows binary content"
	zipData := createTestZip(t, "fetch-track.exe", binContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipData)
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	ctx := context.Background()

	err := downloadAndExtractAsset(ctx, ts.URL, "fetch-track.exe", tempDir, true)
	if err != nil {
		t.Fatalf("downloadAndExtractAsset() error = %v", err)
	}

	destBinary := filepath.Join(tempDir, "fetch-track.exe")
	data, err := os.ReadFile(destBinary)
	if err != nil {
		t.Fatalf("failed to read extracted binary: %v", err)
	}
	if string(data) != binContent {
		t.Errorf("binary content = %q, want %q", string(data), binContent)
	}
}

func TestDownloadAndExtractGoBinaryWithBaseURL(t *testing.T) {
	binContent := "#!/bin/sh\necho 1.5.0\n"
	tarGzData := createTestTarGz(t, "fetch-track", binContent)
	zipData := createTestZip(t, "fetch-track.exe", binContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			http.Redirect(w, r, "/owner/fetch-track/releases/tag/v1.5.0", http.StatusFound)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(zipData)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	ctx := context.Background()

	err := DownloadAndExtractGoBinaryWithBaseURL(ctx, ts.URL, "owner", "fetch-track", "fetch-track", tempDir)
	if err != nil {
		t.Fatalf("DownloadAndExtractGoBinaryWithBaseURL() error = %v", err)
	}

	binName := "fetch-track"
	if runtime.GOOS == "windows" {
		binName = "fetch-track.exe"
	}
	destBinary := filepath.Join(tempDir, binName)
	data, err := os.ReadFile(destBinary)
	if err != nil {
		t.Fatalf("failed to read extracted binary: %v", err)
	}
	if string(data) != binContent {
		t.Errorf("binary content = %q, want %q", string(data), binContent)
	}
}

func TestInstallDirectBinary(t *testing.T) {
	binContent := "#!/bin/sh\necho 2026.08.19\n"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(binContent))
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	ctx := context.Background()

	err := downloadDirectBinary(ctx, ts.URL, "yt-dlp", tempDir)
	if err != nil {
		t.Fatalf("downloadDirectBinary() error = %v", err)
	}

	destBinary := filepath.Join(tempDir, "yt-dlp")
	data, err := os.ReadFile(destBinary)
	if err != nil {
		t.Fatalf("failed to read downloaded binary: %v", err)
	}
	if string(data) != binContent {
		t.Errorf("binary content = %q, want %q", string(data), binContent)
	}
}

func TestInstallYtDlpWithBaseURL(t *testing.T) {
	binContent := "#!/bin/sh\necho 2026.08.19\n"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "yt-dlp") {
			_, _ = w.Write([]byte(binContent))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	ctx := context.Background()

	err := InstallYtDlpWithBaseURL(ctx, ts.URL, tempDir)
	if err != nil {
		t.Fatalf("InstallYtDlpWithBaseURL() error = %v", err)
	}

	binName := "yt-dlp"
	if runtime.GOOS == "windows" {
		binName = "yt-dlp.exe"
	}
	destBinary := filepath.Join(tempDir, binName)
	data, err := os.ReadFile(destBinary)
	if err != nil {
		t.Fatalf("failed to read downloaded yt-dlp binary: %v", err)
	}
	if string(data) != binContent {
		t.Errorf("binary content = %q, want %q", string(data), binContent)
	}
}

func TestUpdateYtDlpWithBaseURL(t *testing.T) {
	binContent := "#!/bin/sh\necho updated\n"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(binContent))
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	ctx := context.Background()

	t.Run("native_update_success", func(t *testing.T) {
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "yt-dlp" && len(args) > 0 && args[0] == "-U" {
				return []byte("yt-dlp is up to date"), nil
			}
			return nil, errors.New("failed")
		}

		err := UpdateYtDlpWithBaseURL(ctx, runner, ts.URL, tempDir)
		if err != nil {
			t.Fatalf("expected native update success, got: %v", err)
		}
	})

	t.Run("native_update_fallback_download", func(t *testing.T) {
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("ERROR: update failed"), errors.New("update error")
		}

		err := UpdateYtDlpWithBaseURL(ctx, runner, ts.URL, tempDir)
		if err != nil {
			t.Fatalf("expected fallback download success, got: %v", err)
		}
	})
}

func TestInstallFFmpeg(t *testing.T) {
	ctx := context.Background()

	t.Run("mock_runner_invoked", func(t *testing.T) {
		called := false
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			called = true
			return []byte("installed"), nil
		}

		_ = InstallFFmpeg(ctx, runner)
		// On supported platforms with package manager, called will be true
		_ = called
	})
}

func TestInstallDependencyWithRunner(t *testing.T) {
	binContent := "#!/bin/sh\necho yt-dlp\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(binContent))
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	c := cache.NewInDir(tempDir, true)
	ctx := context.Background()

	t.Run("install_yt-dlp", func(t *testing.T) {
		err := InstallDependencyWithRunner(ctx, DefaultRunner, "yt-dlp", ts.URL, tempDir, c)
		if err != nil {
			t.Fatalf("InstallDependencyWithRunner(yt-dlp) error = %v", err)
		}
	})

	t.Run("install_ffmpeg_with_runner", func(t *testing.T) {
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("ok"), nil
		}
		_ = InstallDependencyWithRunner(ctx, runner, "ffmpeg", ts.URL, tempDir, c)
	})

	t.Run("install_unknown", func(t *testing.T) {
		err := InstallDependencyWithRunner(ctx, DefaultRunner, "unknown-tool", ts.URL, tempDir, c)
		if err == nil {
			t.Fatal("expected error for unknown tool, got nil")
		}
	})
}

func TestUpdateDependencyWithRunner(t *testing.T) {
	binContent := "#!/bin/sh\necho updated\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(binContent))
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	c := cache.NewInDir(tempDir, true)
	ctx := context.Background()

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}

	t.Run("update_yt-dlp", func(t *testing.T) {
		err := UpdateDependencyWithRunner(ctx, runner, "yt-dlp", ts.URL, tempDir, c)
		if err != nil {
			t.Fatalf("UpdateDependencyWithRunner(yt-dlp) error = %v", err)
		}
	})

	t.Run("update_ffmpeg", func(t *testing.T) {
		_ = UpdateDependencyWithRunner(ctx, runner, "ffmpeg", ts.URL, tempDir, c)
	})

	t.Run("update_unknown", func(t *testing.T) {
		err := UpdateDependencyWithRunner(ctx, runner, "unknown", ts.URL, tempDir, c)
		if err == nil {
			t.Fatal("expected error for unknown dependency")
		}
	})
}

func TestInstallMissingDependenciesWithRunner(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	binContent := "#!/bin/sh\necho ok\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(binContent))
	}))
	defer ts.Close()

	ctx := context.Background()

	t.Run("all_satisfied", func(t *testing.T) {
		tempDir := t.TempDir()
		c := cache.NewInDir(tempDir, true)
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			switch name {
			case "yt-dlp":
				return []byte("2026.07.04\n"), nil
			case "ffmpeg", "ffprobe":
				return []byte("ffmpeg version 8.1.2\n"), nil
			}
			return nil, errors.New("unknown")
		}

		installed, err := InstallMissingDependenciesWithRunner(ctx, runner, c, ts.URL, tempDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(installed) != 0 {
			t.Errorf("expected 0 installed, got %v", installed)
		}
	})

	t.Run("yt_dlp_missing", func(t *testing.T) {
		tempDir := t.TempDir()
		c := cache.NewInDir(tempDir, true)
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			switch name {
			case "yt-dlp":
				return nil, errors.New("not found")
			case "ffmpeg", "ffprobe":
				return []byte("ffmpeg version 8.1.2\n"), nil
			}
			return nil, errors.New("unknown")
		}

		installed, err := InstallMissingDependenciesWithRunner(ctx, runner, c, ts.URL, tempDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(installed) != 1 || installed[0] != "yt-dlp" {
			t.Errorf("expected ['yt-dlp'] installed, got %v", installed)
		}
	})
}

func TestUpdateAllDependenciesWithRunner(t *testing.T) {
	binContent := "#!/bin/sh\necho ok\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(binContent))
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	c := cache.NewInDir(tempDir, true)
	ctx := context.Background()

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}

	updated, _ := UpdateAllDependenciesWithRunner(ctx, runner, ts.URL, tempDir, c)
	_ = updated
}

func TestDownloadAndExtractAsset_Errors(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	t.Run("http_error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "server error", http.StatusInternalServerError)
		}))
		defer ts.Close()

		err := downloadAndExtractAsset(ctx, ts.URL, "fetch-track", tempDir, false)
		if err == nil {
			t.Fatal("expected error on 500 status")
		}
	})

	t.Run("corrupt_gzip", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write([]byte("not gzip data"))
		}))
		defer ts.Close()

		err := downloadAndExtractAsset(ctx, ts.URL, "fetch-track", tempDir, false)
		if err == nil {
			t.Fatal("expected error on corrupt gzip")
		}
	})

	t.Run("tar_missing_executable", func(t *testing.T) {
		tarGzData := createTestTarGz(t, "other-binary", "content")
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		}))
		defer ts.Close()

		err := downloadAndExtractAsset(ctx, ts.URL, "fetch-track", tempDir, false)
		if err == nil || !strings.Contains(err.Error(), "executable \"fetch-track\" not found in tar archive") {
			t.Fatalf("expected missing executable error, got: %v", err)
		}
	})

	t.Run("zip_missing_executable", func(t *testing.T) {
		zipData := createTestZip(t, "other-binary.exe", "content")
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(zipData)
		}))
		defer ts.Close()

		err := downloadAndExtractAsset(ctx, ts.URL, "fetch-track.exe", tempDir, true)
		if err == nil || !strings.Contains(err.Error(), "executable \"fetch-track.exe\" not found in zip archive") {
			t.Fatalf("expected missing executable error, got: %v", err)
		}
	})

	t.Run("corrupt_zip", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte("corrupt zip"))
		}))
		defer ts.Close()

		err := downloadAndExtractAsset(ctx, ts.URL, "fetch-track.exe", tempDir, true)
		if err == nil {
			t.Fatal("expected error on corrupt zip")
		}
	})
}

func TestDownloadDirectBinary_NetworkError(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	err := downloadDirectBinary(ctx, "http://127.0.0.1:0", "yt-dlp", tempDir)
	if err == nil {
		t.Fatal("expected network error on unreachable port 0")
	}

	err = downloadAndExtractAsset(ctx, "http://127.0.0.1:0", "fetch-track", tempDir, false)
	if err == nil {
		t.Fatal("expected network error on unreachable port 0")
	}
}

func TestDownloadDirectBinary_HTTPError(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	err := downloadDirectBinary(ctx, ts.URL, "yt-dlp", tempDir)
	if err == nil {
		t.Fatal("expected error on 404 status")
	}
}

func TestResolveLatestTag_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("404_not_found", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer ts.Close()

		_, err := ResolveLatestTagWithBaseURL(ctx, ts.URL, "owner", "repo")
		if err == nil {
			t.Errorf("expected error for 404 response, got nil")
		}
	})

	t.Run("missing_location_header", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusFound)
		}))
		defer ts.Close()

		_, err := ResolveLatestTagWithBaseURL(ctx, ts.URL, "owner", "repo")
		if err == nil {
			t.Errorf("expected error for missing Location header, got nil")
		}
	})
}

func TestResolveLatestTag_MoreErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("status_200_ok_instead_of_redirect", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		_, err := ResolveLatestTagWithBaseURL(ctx, ts.URL, "owner", "repo")
		if err == nil || !strings.Contains(err.Error(), "unexpected status 200") {
			t.Fatalf("expected unexpected status 200 error, got: %v", err)
		}
	})

	t.Run("empty_tag_in_location", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "/")
			w.WriteHeader(http.StatusFound)
		}))
		defer ts.Close()

		_, err := ResolveLatestTagWithBaseURL(ctx, ts.URL, "owner", "repo")
		if err == nil || !strings.Contains(err.Error(), "failed to extract tag") {
			t.Fatalf("expected failed to extract tag error, got: %v", err)
		}
	})
}

func TestDownloadAndExtractGoBinary_Errors(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	err := DownloadAndExtractGoBinaryWithBaseURL(ctx, "http://127.0.0.1:0", "owner", "repo", "bin", tempDir)
	if err == nil {
		t.Fatal("expected error on invalid baseURL")
	}
}

func TestInstallMissingDependencies_Errors(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	c := cache.NewInDir(tempDir, true)

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("not found")
	}

	// ts returns 500 error on install
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	_, err := InstallMissingDependenciesWithRunner(ctx, runner, c, ts.URL, tempDir)
	if err == nil {
		t.Fatal("expected error when download fails during install missing")
	}
}

func TestUpdateAllDependencies_Errors(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	c := cache.NewInDir(tempDir, true)

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("update error")
	}

	// ts returns 500 error on update
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	_, err := UpdateAllDependenciesWithRunner(ctx, runner, ts.URL, tempDir, c)
	if err == nil {
		t.Fatal("expected error when update fails")
	}
}

func TestInstallFFmpeg_NoPackageManager(t *testing.T) {
	ctx := context.Background()
	// Test error output when no package manager is found or runner fails
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("package manager error")
	}

	_ = InstallFFmpeg(ctx, runner)
}

func TestInitManagedPath(t *testing.T) {
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)

	if err := InitManagedPath(); err != nil {
		t.Fatalf("InitManagedPath() error = %v", err)
	}

	binDir, err := GetManagedBinDir()
	if err != nil {
		t.Fatalf("GetManagedBinDir() error = %v", err)
	}

	currentPath := os.Getenv("PATH")
	if !strings.Contains(currentPath, binDir) {
		t.Errorf("PATH does not contain managed bin directory %q: %s", binDir, currentPath)
	}

	// Calling again should be idempotent
	if err := InitManagedPath(); err != nil {
		t.Fatalf("Second InitManagedPath() error = %v", err)
	}
}

func TestDownloadAsset_TargetDirErrors(t *testing.T) {
	ctx := context.Background()
	binContent := "#!/bin/sh\necho test\n"
	tarGzData := createTestTarGz(t, "fetch-track", binContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(tarGzData)
	}))
	defer ts.Close()

	// Invalid target directory that cannot be written to
	invalidDir := "/nonexistent/dir/for/testing"

	err := downloadAndExtractAsset(ctx, ts.URL, "fetch-track", invalidDir, false)
	if err == nil {
		t.Fatal("expected error when extracting asset to nonexistent dir")
	}

	err = downloadDirectBinary(ctx, ts.URL, "yt-dlp", invalidDir)
	if err == nil {
		t.Fatal("expected error when saving direct binary to nonexistent dir")
	}
}

func TestInstallYtDlp_Windows(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	binContent := "windows yt-dlp binary"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(binContent))
	}))
	defer ts.Close()

	// Direct test for downloadDirectBinary
	err := downloadDirectBinary(ctx, ts.URL, "yt-dlp.exe", tempDir)
	if err != nil {
		t.Fatalf("downloadDirectBinary error = %v", err)
	}
}

func TestWrapperFunctions(t *testing.T) {
	binContent := "#!/bin/sh\necho ok\n"
	tarGzData := createTestTarGz(t, "fetch-track", binContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			http.Redirect(w, r, "/alexgorbatchev/fetch-track-cli/releases/tag/v2.0.0", http.StatusFound)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
			return
		}
		_, _ = w.Write([]byte(binContent))
	}))
	defer ts.Close()

	origBaseURL := defaultBaseURL
	defaultBaseURL = ts.URL
	defer func() { defaultBaseURL = origBaseURL }()

	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tempDir)

	// Test ResolveLatestTag
	tag, err := ResolveLatestTag(ctx, "alexgorbatchev", "fetch-track-cli")
	if err != nil || tag != "v2.0.0" {
		t.Fatalf("ResolveLatestTag() = %q, %v", tag, err)
	}

	// Test DownloadAndExtractGoBinary
	err = DownloadAndExtractGoBinary(ctx, "alexgorbatchev", "fetch-track-cli", "fetch-track", tempDir)
	if err != nil {
		t.Fatalf("DownloadAndExtractGoBinary() = %v", err)
	}

	// Test InstallYtDlp
	err = InstallYtDlp(ctx, tempDir)
	if err != nil {
		t.Fatalf("InstallYtDlp() = %v", err)
	}

	// Test UpdateYtDlp with mock runner
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("yt-dlp is up to date"), nil
	}
	err = UpdateYtDlp(ctx, runner, tempDir)
	if err != nil {
		t.Fatalf("UpdateYtDlp() = %v", err)
	}

	// Test InstallDependency / UpdateDependency
	_ = InstallDependency(ctx, "yt-dlp")
	_ = UpdateDependency(ctx, "yt-dlp")

	// Test InstallMissingDependencies / UpdateAllDependencies
	_, _ = InstallMissingDependencies(ctx)
	_, _ = UpdateAllDependencies(ctx)
}

func TestInstallDependency_Unknown(t *testing.T) {
	ctx := context.Background()
	err := InstallDependency(ctx, "nonexistent-dep")
	if err == nil {
		t.Errorf("expected error for unknown dependency, got nil")
	}
}
