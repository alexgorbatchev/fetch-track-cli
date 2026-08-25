package metadata

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexgorbatchev/godeps"
	"github.com/dj/fetch-track-cli/internal/cache"
)

// NormalizeCoverArtToSquare center-crops and scales any input image to a 1:1 square (1400x1400) JPEG.
func NormalizeCoverArtToSquare(ctx context.Context, inputImagePath, outDir string) (string, error) {
	if inputImagePath == "" {
		return "", fmt.Errorf("empty input image path")
	}
	if _, err := os.Stat(inputImagePath); err != nil {
		return inputImagePath, err
	}

	squarePath := filepath.Join(outDir, fmt.Sprintf("square_cover_%d.jpg", time.Now().UnixNano()))
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "ffmpeg",
		"-v", "quiet",
		"-hide_banner",
		"-y",
		"-i", inputImagePath,
		"-vf", "crop='min(iw,ih)':'min(iw,ih)',scale=1400:1400",
		"-q:v", "2",
		squarePath,
	)
	if err := cmd.Run(); err != nil {
		return inputImagePath, err
	}
	if stat, err := os.Stat(squarePath); err == nil && stat.Size() > 0 {
		return squarePath, nil
	}
	return inputImagePath, nil
}

// ApplyMetadataToLocalTrack embeds metadata and high-res cover art into the audio file
// using ffmpeg and renames the file to <outDir>/<SanitizedArtist - SanitizedTitle>.<ext>.
func ApplyMetadataToLocalTrack(ctx context.Context, filePath string, metadata TrackMetadataResult, outDir string, verboseAndCache ...interface{}) (string, error) {
	isVerbose := false
	var cacheInst *cache.Cache

	for _, v := range verboseAndCache {
		switch val := v.(type) {
		case bool:
			isVerbose = val
		case *cache.Cache:
			cacheInst = val
		}
	}

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

	tmpDir := ".tmp"
	_ = os.MkdirAll(tmpDir, 0755)

	var coverTempPath string
	if metadata.CoverArtURL != "" {
		if cacheInst != nil {
			if cachedPath, found := cacheInst.GetFile("artworks", metadata.CoverArtURL, 30*24*time.Hour); found {
				coverTempPath = cachedPath
			}
		}

		if coverTempPath == "" {
			dlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, metadata.CoverArtURL, nil)
			if err == nil {
				resp, err := http.DefaultClient.Do(req)
				if err == nil && resp.StatusCode == http.StatusOK {
					imgBytes, errRead := io.ReadAll(resp.Body)
					_ = resp.Body.Close()

					if errRead == nil && len(imgBytes) > 0 {
						if cacheInst != nil {
							if savedPath, errPut := cacheInst.PutFile("artworks", metadata.CoverArtURL, imgBytes); errPut == nil {
								coverTempPath = savedPath
							}
						}

						if coverTempPath == "" {
							cPath := filepath.Join(tmpDir, fmt.Sprintf("cover_%d.jpg", time.Now().UnixNano()))
							if errWrite := os.WriteFile(cPath, imgBytes, 0644); errWrite == nil {
								coverTempPath = cPath
							}
						}
					}
				} else if resp != nil {
					_ = resp.Body.Close()
				}
			}
			cancel()
		}
		if coverTempPath == "" {
			// Check if input audio file has an embedded video / thumbnail stream from download
			extractedPath := filepath.Join(tmpDir, fmt.Sprintf("extracted_%d.jpg", time.Now().UnixNano()))
			extCtx, extCancel := context.WithTimeout(ctx, 10*time.Second)
			extCmd := exec.CommandContext(extCtx, "ffmpeg",
				"-v", "quiet",
				"-hide_banner",
				"-y",
				"-i", filePath,
				"-an",
				"-vcodec", "copy",
				extractedPath,
			)
			if errExt := extCmd.Run(); errExt == nil {
				if stat, errStat := os.Stat(extractedPath); errStat == nil && stat.Size() > 0 {
					coverTempPath = extractedPath
				} else {
					_ = os.Remove(extractedPath)
				}
			}
			extCancel()
		}
	}

	// Normalize cover art to 1:1 square 1400x1400
	var finalCoverPath string
	if coverTempPath != "" {
		if squarePath, err := NormalizeCoverArtToSquare(ctx, coverTempPath, tmpDir); err == nil {
			finalCoverPath = squarePath
		} else {
			finalCoverPath = coverTempPath
		}
	}

	defer func() {
		if coverTempPath != "" && strings.HasPrefix(coverTempPath, tmpDir) {
			_ = os.Remove(coverTempPath)
		}
		if finalCoverPath != "" && finalCoverPath != coverTempPath {
			_ = os.Remove(finalCoverPath)
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

	dateTag := metadata.ReleaseDate
	if dateTag == "" {
		dateTag = metadata.ReleaseYear
	}

	var commentParts []string
	if metadata.AudioSourceURL != "" {
		commentParts = append(commentParts, fmt.Sprintf("Source: %s", metadata.AudioSourceURL))
	}
	if metadata.Source != "" {
		commentParts = append(commentParts, fmt.Sprintf("Metadata: %s", metadata.Source))
	}
	if !metadata.FetchedAt.IsZero() {
		commentParts = append(commentParts, fmt.Sprintf("Fetched: %s", metadata.FetchedAt.Format("2006-01-02")))
	}

	metaList := []string{
		fmt.Sprintf("title=%s", metadata.Title),
		fmt.Sprintf("artist=%s", metadata.Artist),
		fmt.Sprintf("album=%s", metadata.Album),
		fmt.Sprintf("genre=%s", metadata.Genre),
	}
	if dateTag != "" {
		metaList = append(metaList, fmt.Sprintf("date=%s", dateTag))
	}
	if len(commentParts) > 0 {
		metaList = append(metaList, fmt.Sprintf("comment=%s", strings.Join(commentParts, " | ")))
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()

	if isVerbose {
		fmt.Printf("ffmpeg: embedding tags + covr\n")
	}

	args := []string{
		"-v", "quiet",
		"-hide_banner",
		"-y",
		"-i", filePath,
	}

	if finalCoverPath != "" && ext != ".opus" && ext != ".ogg" {
		args = append(args,
			"-i", finalCoverPath,
			"-map", "0:a:0",
			"-map", "1:0",
			"-c:a", "copy",
			"-c:v", "copy",
		)
		if ext == ".m4a" || ext == ".mp4" {
			args = append(args, "-disposition:v", "attached_pic")
		} else if ext == ".mp3" {
			args = append(args,
				"-id3v2_version", "3",
				"-metadata:s:v", `title="Album cover"`,
				"-metadata:s:v", `comment="Cover (front)"`,
			)
		}
	} else {
		args = append(args, "-map", "0:a:0", "-c:a", "copy")
		if ext == ".mp3" {
			args = append(args, "-id3v2_version", "3")
		}
	}

	for _, m := range metaList {
		args = append(args, "-metadata", m)
	}

	args = append(args, tmpTaggedPath)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(cmdCtx, "ffmpeg", args...)
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		_ = os.Remove(tmpTaggedPath)
		cleanErr := godeps.SanitizeStderr(stderr.String())
		if cleanErr != "" {
			return filePath, fmt.Errorf("ffmpeg metadata tagging failed for %s: %w (%s)", filePath, err, cleanErr)
		}
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
