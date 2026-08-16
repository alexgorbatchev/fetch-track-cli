package metadata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ApplyMetadataToLocalTrack embeds metadata and high-res cover art into the M4A file
// using ffmpeg and renames the file to <outDir>/<SanitizedArtist - SanitizedTitle>.m4a.
func ApplyMetadataToLocalTrack(ctx context.Context, filePath string, metadata TrackMetadataResult, outDir string, verbose ...bool) (string, error) {
	isVerbose := len(verbose) > 0 && verbose[0]

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return filePath, fmt.Errorf("creating output directory %s: %w", outDir, err)
	}

	if isVerbose {
		fmt.Printf("tagging: %s\n", filePath)
		if metadata.CoverArtURL != "" {
			fmt.Printf("coverart: downloading %s\n", metadata.CoverArtURL)
		}
	}

	_, errStat := os.Stat(".tmp")
	dotTmpExisted := !os.IsNotExist(errStat)

	tmpDir := filepath.Join(".tmp", "fetch-track-cli-tmp")
	_ = os.MkdirAll(tmpDir, 0755)

	var coverTempPath string
	if metadata.CoverArtURL != "" {
		dlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, metadata.CoverArtURL, nil)
		if err == nil {
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				cPath := filepath.Join(tmpDir, fmt.Sprintf("cover_%d.jpg", time.Now().UnixNano()))
				outFile, err := os.Create(cPath)
				if err == nil {
					_, _ = io.Copy(outFile, resp.Body)
					_ = outFile.Close()
					coverTempPath = cPath
				}
				_ = resp.Body.Close()
			}
		}
		cancel()
	}

	defer func() {
		if coverTempPath != "" {
			_ = os.Remove(coverTempPath)
		}
		_ = os.Remove(tmpDir)
		if !dotTmpExisted {
			_ = os.Remove(".tmp")
		}
	}()

	targetFilename := fmt.Sprintf("%s - %s.m4a", metadata.Artist, metadata.Title)
	if metadata.Artist == "Unknown Artist" || metadata.Artist == "" {
		baseName := filepath.Base(filePath)
		baseExt := filepath.Ext(baseName)
		cleanBaseName := strings.TrimSuffix(baseName, baseExt)
		targetFilename = cleanBaseName + ".m4a"
	}

	cleanTargetFilename := SanitizeFilename(targetFilename)
	finalPath := filepath.Join(outDir, cleanTargetFilename)

	tmpTaggedPath := filepath.Join(tmpDir, fmt.Sprintf("tagged_%d.m4a", time.Now().UnixNano()))

	inputExt := strings.ToLower(filepath.Ext(filePath))

	args := []string{"-v", "quiet", "-hide_banner", "-i", filePath}
	if coverTempPath != "" {
		args = append(args, "-i", coverTempPath, "-map", "0:a:0", "-map", "1:v:0")
		if inputExt == ".m4a" {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-b:a", "256k")
		}
		args = append(args, "-c:v", "copy", "-disposition:v", "attached_pic")
	} else {
		args = append(args, "-map", "0:a:0")
		if inputExt == ".m4a" {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-b:a", "256k")
		}
	}

	args = append(args,
		"-metadata", fmt.Sprintf("title=%s", metadata.Title),
		"-metadata", fmt.Sprintf("artist=%s", metadata.Artist),
		"-metadata", fmt.Sprintf("album=%s", metadata.Album),
		"-metadata", fmt.Sprintf("genre=%s", metadata.Genre),
		"-metadata", fmt.Sprintf("date=%s", metadata.ReleaseYear),
		"-y",
		tmpTaggedPath,
	)

	cmdCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	cmd := exec.CommandContext(cmdCtx, "ffmpeg", args...)
	if isVerbose {
		fmt.Printf("ffmpeg: embedding tags + covr\n")
	}
	err := cmd.Run()
	cancel()

	if err != nil {
		_ = os.Remove(tmpTaggedPath)
		return filePath, fmt.Errorf("ffmpeg metadata tagging failed for %s: %w", filePath, err)
	}

	if _, err := os.Stat(tmpTaggedPath); err != nil {
		return filePath, fmt.Errorf("tagged file not created at %s: %w", tmpTaggedPath, err)
	}

	// Move tagged file to final output location
	if filePath != finalPath {
		_ = os.Remove(filePath)
	}
	_ = os.Remove(finalPath)

	if err := os.Rename(tmpTaggedPath, finalPath); err != nil {
		// Fallback to copy if cross-device link error occurs
		input, errRead := os.ReadFile(tmpTaggedPath)
		if errRead != nil {
			return filePath, fmt.Errorf("reading temp tagged file: %w", errRead)
		}
		if errWrite := os.WriteFile(finalPath, input, 0644); errWrite != nil {
			return filePath, fmt.Errorf("writing final tagged file: %w", errWrite)
		}
		_ = os.Remove(tmpTaggedPath)
	}

	return finalPath, nil
}
