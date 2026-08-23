---
created_on: 2026-08-14 21:25
last_modified: 2026-08-18 05:30
status: current
---

# fetch-track-cli

Go CLI tool for searching, verifying, downloading, and managing high-fidelity single audio tracks in native formats for DJ collections.

## Commands
- **Build Local Binary:** `just build` (compiles to `bin/fetch-track`)
- **Run Acquisition Pipeline:** `just run "<youtube_url_or_search_query>"`
- **Inspect / Verify Audio Track:** `just verify "tracks/Artist - Title.m4a"`
- **Run Out-of-Band Socket Progress Demo:** `just demo-progress "<query>"`
- **Run Tests:** `just test` (`go test -v ./...`)
- **Run Linter / Static Analysis:** `just vet` (`go vet ./...`)
- **Format Source Code:** `just fmt` (`go fmt ./...`)

## Setup & Environment
- **Prerequisites:** Go 1.26+, `yt-dlp`, `ffmpeg`, `ffprobe`, `just`.
- **Output Directory:** Default output directory is the current working directory (`.`). Use `-o <dir>` / `--out-dir` to specify a custom destination directory.
- **Temporary Files:** Temporary file operations use `.tmp/` within the project root.

## Conventions
- **Original & Extended Mixes First:** Prefer full-length Extended, Original, Club, or Dub Mixes (4.5 to 13 minutes) with beatmatchable intro/outro sections. Reject short radio edits (< 3.5 minutes).
- **Zero-Transcode Policy:** Never lossily re-encode MP3, OPUS, or FLAC streams to AAC. Prioritize audio quality at all times. Always use `-c:a copy` during metadata tagging to preserve the original bitstream byte-for-byte.
- **Filename Sanitization:** Clean output filenames (`<outDir>/<Artist> - <Title>.<ext>`). Stripped of YouTube IDs or brackets `[...]`.
- **Hermetic Unit Tests:** All unit tests (`just test`) MUST remain 100% offline, hermetic, and deterministic. All HTTP network calls in tests use local `mockTransport` HTTP mocks, all audio operations use local synthetic files in `t.TempDir()`, and zero unit tests depend on or call the live internet.
- **Output Formatting:** All CLI output must be plain text without emojis across all modes and logs. In interactive non-AGENT mode, a bottom-line braille terminal spinner (`⠏ working... <action>`) indicates active background tasks while candidate results print above it in real time as they arrive (`PrintAbove`).
- **Progress Socket / IPC Telemetry:** When executed by another CLI, agent orchestrator, or GUI, use `--progress-target <uri>` (or `--progress-socket <path>`, or `FETCH_TRACK_PROGRESS_TARGET` env var) to stream atomic NDJSON progress events (`phase_start`, `candidate_found`, `candidate_selected`, `progress`, `complete`, `error`) over a UNIX socket (`unix:///path/to.sock`), TCP address (`tcp://127.0.0.1:9099`), file descriptor (`fd://3`), or standard stream (`stdout`, `stderr`).
- **Release Notes:** All GitHub releases MUST include comprehensive release notes detailing user-visible changes, performance improvements, and notable commits in the tag range.
- **Metadata Fallback Chain:**
  1. iTunes Search API (1400x1400 artwork)
  2. MusicBrainz API + Cover Art Archive
  3. YouTube / Local Filename Raw Fallback
- **Origin Date & Provenance Preservation:** Always preserve full release date (`YYYY-MM-DD` when available) in file date tags and embed source provenance (audio source URL, metadata provider, and acquisition date) in track comment tags (`Source: <url> | Metadata: <provider> | Fetched: <date>`) without re-encoding.
- **Dependency Management & Auto-Installation:** Use `fetch-track dependencies` (`deps`), `fetch-track deps install [dep...]`, `fetch-track deps update [dep...]`, or the `--auto-install` flag to check, install, or update required external tools (`yt-dlp`, `ffmpeg`, `ffprobe`) in `$XDG_DATA_HOME/fetch-track/bin` (or `~/.local/share/fetch-track/bin`) powered by `github.com/alexgorbatchev/godeps`.
- **Required Dependencies Messages:** Dependency verification and error messages must always show the required minimum version, and if a dependency is already installed but outdated, clearly display which old version is currently available.
- **No Stack Traces in Output:** User-facing output and error messages must never expose raw stack traces, Python tracebacks, Go panic dumps, or internal runtime frames from dependencies; errors from external tools must always be cleaned to concise user-actionable messages using `godeps.SanitizeStderr`.
- **Self-Upgrade:** Use `fetch-track upgrade` (aliases: `self-update`, `update-self`) to check for newer GitHub releases and upgrade the running `fetch-track` binary in-place without using GitHub API via `godeps.UpgradeSelf`.

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
- `PROGRESS.md` - Out-of-band socket & IPC progress telemetry protocol specification and schemas.
