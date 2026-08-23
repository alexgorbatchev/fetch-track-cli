package deps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// UpgradeSelf checks for a newer release of fetch-track and replaces the running binary.
func UpgradeSelf(ctx context.Context, currentVersion string) (bool, string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return false, "", fmt.Errorf("locating current executable: %w", err)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return false, "", fmt.Errorf("evaluating executable symlinks %q: %w", exePath, err)
	}

	return UpgradeSelfWithBaseURL(ctx, defaultBaseURL, currentVersion, exePath)
}

// UpgradeSelfWithBaseURL checks and upgrades the executable at targetExePath using a custom base URL.
func UpgradeSelfWithBaseURL(ctx context.Context, baseURL, currentVersion, targetExePath string) (bool, string, error) {
	owner := "alexgorbatchev"
	repo := "fetch-track-cli"
	binName := "fetch-track"

	tag, err := ResolveLatestTagWithBaseURL(ctx, baseURL, owner, repo)
	if err != nil {
		return false, "", fmt.Errorf("resolving latest release for %s/%s: %w", owner, repo, err)
	}

	latestVer := strings.TrimPrefix(tag, "v")
	cleanCurrentVer := strings.TrimPrefix(strings.TrimSpace(currentVersion), "v")

	// If running a release version and current >= latest, we are already up to date
	if cleanCurrentVer != "dev" && cleanCurrentVer != "" {
		if compErr := CompareVersions(cleanCurrentVer, latestVer); compErr == nil {
			return false, latestVer, nil
		}
	}

	targetDir := filepath.Dir(targetExePath)
	osName := runtime.GOOS
	archName := runtime.GOARCH

	isZip := (osName == "windows")
	ext := "tar.gz"
	execName := binName
	if isZip {
		ext = "zip"
		execName = binName + ".exe"
	}

	assetName := fmt.Sprintf("%s_%s_%s_%s.%s", binName, latestVer, osName, archName, ext)
	assetURL := fmt.Sprintf("%s/%s/%s/releases/download/%s/%s", strings.TrimRight(baseURL, "/"), owner, repo, tag, assetName)

	if err := downloadAndExtractAsset(ctx, assetURL, execName, targetDir, isZip); err != nil {
		return false, "", fmt.Errorf("downloading and extracting upgrade asset: %w", err)
	}

	downloadedBin := filepath.Join(targetDir, execName)

	// If targetExePath is different from downloadedBin (e.g. custom name or path in test), move it
	if downloadedBin != targetExePath {
		if err := os.Rename(downloadedBin, targetExePath); err != nil {
			_ = os.Remove(downloadedBin)
			return false, "", fmt.Errorf("replacing binary at %q: %w", targetExePath, err)
		}
	}

	return true, latestVer, nil
}
