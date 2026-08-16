---
created_on: 2026-08-14 21:25
last_modified: 2026-08-16 08:50
status: current
---

# fetch-track-cli

Go CLI tool for searching, verifying, downloading, and managing high-fidelity single audio tracks in native formats for DJ collections.

## Commands
- **Build Local Binary:** `just build` (compiles to `bin/fetch-track`)
- **Run Acquisition Pipeline:** `just run "<youtube_url_or_search_query>"`
- **Inspect / Verify Audio Track:** `just verify "tracks/Artist - Title.m4a"`
- **Run Tests:** `just test` (`go test -v ./...`)
- **Run Linter / Static Analysis:** `just vet` (`go vet ./...`)
- **Format Source Code:** `just fmt` (`go fmt ./...`)

## Setup & Environment
- **Prerequisites:** Go 1.26+, `yt-dlp`, `ffmpeg`, `ffprobe`, `just`.
- **Output Directory:** Master tracks are stored in `./tracks/` with clean sanitized filenames. `.m4a` is preferred when native AAC streams are available; native formats (`.mp3`, `.flac`) are preserved zero-loss when downloaded from sources like SoundCloud.
- **Temporary Files:** Temporary file operations use `.tmp/` within the project root.

## Conventions
- **Original & Extended Mixes First:** Prefer full-length Extended, Original, Club, or Dub Mixes (4.5 to 13 minutes) with beatmatchable intro/outro sections. Reject short radio edits (< 3.5 minutes).
- **Zero-Transcode Policy:** Never lossily re-encode MP3, OPUS, or FLAC streams to AAC. Prioritize audio quality at all times. Always use `-c:a copy` during metadata tagging to preserve the original bitstream byte-for-byte.
- **Filename Sanitization:** Clean output filenames (`<outDir>/<Artist> - <Title>.<ext>`). Stripped of YouTube IDs or brackets `[...]`.
- **Output Formatting:** All CLI output must be plain text without emojis across all modes and logs. In interactive non-AGENT mode, a bottom-line braille terminal spinner (`⠏ working... <action>`) indicates active background tasks while candidate results print above it in real time as they arrive (`PrintAbove`).
- **Metadata Fallback Chain:**
  1. iTunes Search API (1400x1400 artwork)
  2. MusicBrainz API + Cover Art Archive
  3. YouTube / Local Filename Raw Fallback

## Gotchas
- **Zero-Reencoding Tagging:** When embedding artwork and tags via `ffmpeg`, always pass `-c:a copy` (plus `-id3v2_version 3` for MP3) to avoid triggering unintended audio transcoding.
- **Temporary Folder Cleanup:** `.tmp` directories created during execution must be cleaned up on completion if `.tmp` did not exist prior to execution.

## Boundaries
- **Always:** Automatically record all new instructions in the most appropriate `AGENTS.md` file immediately upon receipt (check with user if existing instructions conflict).
- **Always:** Any time code is changed such that results from running that code are changed, a test file must be changed as well; 90% code coverage is required (the `scripts/` folder is explicitly excluded from this rule).
- **Always:** Save all downloaded audio tracks into the `./tracks/` directory with clean filenames (no `[...]` video IDs) and embedded artwork.
- **Always:** Run `just test` and `just vet` before committing changes.
- **Ask first:** Deleting master audio files or modifying core spectral analysis algorithms.
- **Never:** Include YouTube video IDs or brackets (`[...]`) in track filenames.
- **Never:** Perform lossy-to-lossy audio re-encoding (transcoding MP3 or OPUS to AAC).
- **Never:** Commit compiled Go binaries (e.g. `bin/fetch-track`) or master tracks to git.

## References
- `README.md` - Technical architecture, CLI usage, and processing pipeline.
