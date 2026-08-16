---
created_on: 2026-08-14 21:25
last_modified: 2026-08-14 23:25
status: current
---

# fetch-track-cli

Go CLI tool for searching, verifying, downloading, and managing high-fidelity single audio tracks in `.m4a` format for DJ collections.

## Commands
- **Build Local Binary:** `just build` (compiles to `bin/fetch-track`)
- **Run Acquisition Pipeline:** `just run "<youtube_url_or_search_query>"`
- **Inspect / Verify Audio Track:** `just verify "tracks/Artist - Title.m4a"`
- **Run Tests:** `just test` (`go test -v ./...`)
- **Run Linter / Static Analysis:** `just vet` (`go vet ./...`)
- **Format Source Code:** `just fmt` (`go fmt ./...`)

## Setup & Environment
- **Prerequisites:** Go 1.26+, `yt-dlp`, `ffmpeg`, `ffprobe`, `just`.
- **Output Directory & Format:** Master tracks are stored in `./tracks/` as **`.m4a`** files (MPEG-4 Audio with AAC stream and embedded 1400x1400 MP4 `covr` atom artwork).
- **Temporary Files:** Temporary file operations use `.tmp/fetch-track-cli-tmp` within the project root.

## Conventions
- **Original & Extended Mixes First:** Prefer full-length Extended, Original, Club, or Dub Mixes (4.5 to 13 minutes) with beatmatchable intro/outro sections. Reject short radio edits (< 3.5 minutes).
- **Format Enforcement:** Always output `.m4a`. If non-M4A audio (`.mp3`, `.opus`, etc.) is extracted by `yt-dlp`, re-encode to high-quality AAC (`-c:a aac -b:a 256k`) into an M4A container and delete the original source file.
- **Filename Sanitization:** Clean output filenames (`<outDir>/<Artist> - <Title>.m4a`). Stripped of YouTube IDs or brackets `[...]`.
- **Unicode & Accent Matching:** Title and artist matching uses `golang.org/x/text` NFD decomposition and non-spacing combining mark stripping (`unicode.Mn`) for accent-insensitive matching across Unicode alphabets (`ë`, `ё`, `ö`, `é`, `ñ`).
- **Metadata Fallback Chain:**
  1. iTunes Search API (1400x1400 artwork)
  2. MusicBrainz API + Cover Art Archive
  3. YouTube / Local Filename Raw Fallback

## Gotchas
- **Non-M4A Stream Extraction:** `yt-dlp` from non-YouTube sources (e.g. SoundCloud) may extract native MP3 or OPUS streams -> Always enforce `--audio-format m4a` during extraction and re-encode to AAC via `ffmpeg` if non-M4A audio is downloaded.
- **Temporary Folder Cleanup:** `.tmp` directories created during execution must be cleaned up on completion if `.tmp` did not exist prior to execution.

## Boundaries
- **Always:** Automatically record all new instructions in the most appropriate `AGENTS.md` file immediately upon receipt (check with user if existing instructions conflict).
- **Always:** Any time code is changed such that results from running that code are changed, a test file must be changed as well; 90% code coverage is required (the `scripts/` folder is explicitly excluded from this rule).
- **Always:** Save all downloaded audio tracks into the `./tracks/` directory as **`.m4a`** files with clean filenames (no `[...]` video IDs) and embedded 1400x1400 artwork.
- **Always:** Run `just test` and `just vet` before committing changes.
- **Ask first:** Deleting master `.m4a` audio files or modifying core spectral analysis algorithms.
- **Never:** Include YouTube video IDs or brackets (`[...]`) in track filenames.
- **Never:** Store raw `.opus` or `.mp3` files without converting or acquiring as `.m4a`.
- **Never:** Commit compiled Go binaries (e.g. `bin/fetch-track`) or master `.m4a` tracks to git.

## References
- `README.md` - Technical architecture, CLI usage, and re-encoding processing pipeline.
