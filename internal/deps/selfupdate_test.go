package deps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeSelfToPath(t *testing.T) {
	tempDir := t.TempDir()
	targetExe := filepath.Join(tempDir, "custom-fetch-track")

	// Write old mock binary
	oldContent := "#!/bin/sh\necho old-version\n"
	if err := os.WriteFile(targetExe, []byte(oldContent), 0755); err != nil {
		t.Fatalf("failed to write initial binary: %v", err)
	}

	newContent := "#!/bin/sh\necho 2.0.0\n"
	tarGzData := createTestTarGz(t, "fetch-track", newContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alexgorbatchev/fetch-track-cli/releases/latest":
			http.Redirect(w, r, "/alexgorbatchev/fetch-track-cli/releases/tag/v2.0.0", http.StatusFound)
		case "/alexgorbatchev/fetch-track-cli/releases/download/v2.0.0/fetch-track_2.0.0_darwin_arm64.tar.gz",
			"/alexgorbatchev/fetch-track-cli/releases/download/v2.0.0/fetch-track_2.0.0_darwin_amd64.tar.gz",
			"/alexgorbatchev/fetch-track-cli/releases/download/v2.0.0/fetch-track_2.0.0_linux_amd64.tar.gz",
			"/alexgorbatchev/fetch-track-cli/releases/download/v2.0.0/fetch-track_2.0.0_linux_arm64.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ctx := context.Background()

	// 1. When current version is older (1.0.0 < 2.0.0), it should upgrade
	updated, newVer, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "1.0.0", targetExe)
	if err != nil {
		t.Fatalf("UpgradeSelfWithBaseURL() error = %v", err)
	}
	if !updated {
		t.Errorf("expected updated = true, got false")
	}
	if newVer != "2.0.0" {
		t.Errorf("expected newVer = '2.0.0', got %q", newVer)
	}

	// Verify target executable was replaced with new content
	data, err := os.ReadFile(targetExe)
	if err != nil {
		t.Fatalf("failed to read upgraded binary: %v", err)
	}
	if string(data) != newContent {
		t.Errorf("binary content = %q, want %q", string(data), newContent)
	}

	// 2. When current version is already 2.0.0, it should report up to date
	updated, sameVer, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "2.0.0", targetExe)
	if err != nil {
		t.Fatalf("UpgradeSelfWithBaseURL() error = %v", err)
	}
	if updated {
		t.Errorf("expected updated = false when already on latest version, got true")
	}
	if sameVer != "2.0.0" {
		t.Errorf("expected sameVer = '2.0.0', got %q", sameVer)
	}
}

func TestUpgradeSelf(t *testing.T) {
	newContent := "#!/bin/sh\necho 2.0.0\n"
	tarGzData := createTestTarGz(t, "fetch-track", newContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alexgorbatchev/fetch-track-cli/releases/latest":
			http.Redirect(w, r, "/alexgorbatchev/fetch-track-cli/releases/tag/v2.0.0", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		}
	}))
	defer ts.Close()

	origBaseURL := defaultBaseURL
	defaultBaseURL = ts.URL
	defer func() { defaultBaseURL = origBaseURL }()

	ctx := context.Background()
	// Test when current version is already 2.0.0 (up to date)
	updated, ver, err := UpgradeSelf(ctx, "2.0.0")
	if err != nil {
		t.Fatalf("UpgradeSelf() error = %v", err)
	}
	if updated {
		t.Errorf("expected updated = false, got true")
	}
	if ver != "2.0.0" {
		t.Errorf("expected ver = '2.0.0', got %q", ver)
	}

	// Test when current version is older
	updated, ver, err = UpgradeSelf(ctx, "0.1.0")
	if err != nil {
		t.Fatalf("UpgradeSelf(0.1.0) error = %v", err)
	}
	if !updated {
		t.Errorf("expected updated = true, got false")
	}
	if ver != "2.0.0" {
		t.Errorf("expected ver = '2.0.0', got %q", ver)
	}
}

func TestUpgradeSelf_DevVersion(t *testing.T) {
	tempDir := t.TempDir()
	targetExe := filepath.Join(tempDir, "fetch-track")

	newContent := "#!/bin/sh\necho 2.0.0\n"
	tarGzData := createTestTarGz(t, "fetch-track", newContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alexgorbatchev/fetch-track-cli/releases/latest":
			http.Redirect(w, r, "/alexgorbatchev/fetch-track-cli/releases/tag/v2.0.0", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	updated, ver, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "dev", targetExe)
	if err != nil {
		t.Fatalf("UpgradeSelfWithBaseURL(dev) error = %v", err)
	}
	if !updated {
		t.Errorf("expected updated = true for 'dev' version, got false")
	}
	if ver != "2.0.0" {
		t.Errorf("expected ver = '2.0.0', got %q", ver)
	}
}

func TestUpgradeSelf_Errors(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	targetExe := filepath.Join(tempDir, "fetch-track")

	t.Run("resolve_tag_error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer ts.Close()

		_, _, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "1.0.0", targetExe)
		if err == nil {
			t.Fatal("expected error when resolving tag fails")
		}
	})

	t.Run("download_asset_error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/releases/latest") {
				http.Redirect(w, r, "/alexgorbatchev/fetch-track-cli/releases/tag/v2.0.0", http.StatusFound)
				return
			}
			http.Error(w, "download error", http.StatusInternalServerError)
		}))
		defer ts.Close()

		_, _, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "1.0.0", targetExe)
		if err == nil {
			t.Fatal("expected error when downloading asset fails")
		}
	})
}
