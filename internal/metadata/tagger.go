package metadata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	ffmpeg "github.com/u2takey/ffmpeg-go"
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

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		ext = ".m4a"
	}

	targetFilename := fmt.Sprintf("%s - %s%s", metadata.Artist, metadata.Title, ext)
	if metadata.Artist == "Unknown Artist" || metadata.Artist == "" {
		baseName := filepath.Base(filePath)
		baseExt := filepath.Ext(baseName)
		cleanBaseName := strings.TrimSuffix(baseName, baseExt)
		targetFilename = cleanBaseName + ext
	}

	cleanTargetFilename := SanitizeFilename(targetFilename)
	finalPath := filepath.Join(outDir, cleanTargetFilename)

	tmpTaggedPath := filepath.Join(tmpDir, fmt.Sprintf("tagged_%d%s", time.Now().UnixNano(), ext))

	kwArgs := ffmpeg.KwArgs{
		"v":           "quiet",
		"hide_banner": "",
		"y":           "",
		"metadata": []string{
			fmt.Sprintf("title=%s", metadata.Title),
			fmt.Sprintf("artist=%s", metadata.Artist),
			fmt.Sprintf("album=%s", metadata.Album),
			fmt.Sprintf("genre=%s", metadata.Genre),
			fmt.Sprintf("date=%s", metadata.ReleaseYear),
		},
	}

	if ext == ".mp3" {
		kwArgs["id3v2_version"] = "3"
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()

	if isVerbose {
		fmt.Printf("ffmpeg: embedding tags + covr\n")
	}

	var err error
	if coverTempPath != "" {
		audioStream := ffmpeg.Input(filePath)
		coverStream := ffmpeg.Input(coverTempPath)

		kwArgs["map"] = []string{"0:a:0", "1:v:0"}
		kwArgs["c:a"] = "copy"
		kwArgs["c:v"] = "copy"
		kwArgs["disposition:v"] = "attached_pic"

		err = ffmpeg.OutputContext(cmdCtx, []*ffmpeg.Stream{audioStream, coverStream}, tmpTaggedPath, kwArgs).Run()
	} else {
		audioStream := ffmpeg.Input(filePath)

		kwArgs["map"] = "0:a:0"
		kwArgs["c:a"] = "copy"

		err = ffmpeg.OutputContext(cmdCtx, []*ffmpeg.Stream{audioStream}, tmpTaggedPath, kwArgs).Run()
	}

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
