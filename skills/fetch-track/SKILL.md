---
name: fetch-track
description: >-
  Use when searching, verifying, downloading, or tagging single DJ audio tracks
  from streaming URLs or search queries with the fetch-track CLI, inspecting
  audio frequency bandwidth and gain offsets, managing external media
  dependencies, or streaming NDJSON progress telemetry.
author: alexgorbatchev
metadata:
  created_on: 2026-10-18 09:20
  last_modified: 2026-10-18 09:20
  status: current
---

Operate the `fetch-track` CLI in `AGENT=1` mode for deterministic, token-conservative key-value output or pass `--json` for structured JSON on `stdout`.

## Command Reference

### Root Acquisition Pipeline

```bash
AGENT=1 fetch-track <url|query> [flags]
```

- Positional argument `<url|query>` (required when executing acquisition; running with no arguments prints help): Direct streaming track URL (`https://www.youtube.com/watch?v=...`, `https://youtu.be/...`, `https://soundcloud.com/...`, `https://artist.bandcamp.com/track/...`, `https://mixcloud.com/...`) or `"Artist - Title"` search query.
- Executes 5 sequential pipeline milestones:
  1. Verifies required external tools (`yt-dlp` >= `2024.08.01`, `ffmpeg` >= `4.4`, `ffprobe` >= `4.4`, `tag-track` >= `1.0.0`).
  2. Searches configured sources in parallel and ranks candidates using DJ duration curves (preferring 4.5–9 minute extended, club, original, and dub mixes while penalizing <3.5 minute radio edits, snippets, and continuous mixes).
  3. Downloads the native audio stream without lossy re-encoding (`-c:a copy`), verifying candidate identity via acoustic fingerprinting when `--confirm-fingerprint` is enabled.
  4. Decodes PCM audio via `ffmpeg` and runs Goertzel spectral bin analysis to measure high-frequency cutoff bandwidth, peak/RMS loudness, and recommended DJ mixer gain offset (`-12.0 dBFS` target RMS).
  5. Enriches the downloaded file via `tag-track` with canonical metadata, `1400x1400` 1:1 square cover art, release date, and provenance comments (`Source: <url> | Metadata: <provider> | Fetched: <date>`), saving to `<out-dir>/<Artist> - <Title>.<ext>` without brackets or video IDs.

### Track Verification

```bash
AGENT=1 fetch-track track
AGENT=1 fetch-track track verify <url|path> [flags]
```

- `fetch-track track` groups track inspection subcommands.
- `fetch-track track verify <url|path>` (shorthand alias: `fetch-track verify <url|path>`) inspects a local audio file or remote streaming URL (`<url|path>`) for duration, mix structure classification, Goertzel frequency cutoff bandwidth, peak/RMS loudness, sub/kick bass levels, and DJ mixer gain offset.
- Exits with code `2` in human mode when the target is classified as a short radio edit (`<= 4.0` minutes).

### Dependency Management

```bash
AGENT=1 fetch-track dependencies
AGENT=1 fetch-track dependencies install [dep...]
AGENT=1 fetch-track dependencies update [dep...]
```

- `fetch-track dependencies` (alias: `deps`) verifies installed versions of `yt-dlp`, `ffmpeg`, `ffprobe`, and `tag-track` against minimum version requirements across `$PATH` and `$XDG_DATA_HOME/fetch-track/bin`.
- `fetch-track dependencies install [dep...]` (aliases: `add`, `get`) downloads and installs missing external binaries (or specific `[dep...]` names from `yt-dlp`, `ffmpeg`, `ffprobe`, `tag-track`) into `$XDG_DATA_HOME/fetch-track/bin`.
- `fetch-track dependencies update [dep...]` (alias: `upgrade`) updates all managed binaries (or specified `[dep...]` names) to their latest upstream releases.

### Self-Upgrade, Skill, Help, and Shell Completion

```bash
AGENT=1 fetch-track upgrade
AGENT=1 fetch-track skill
AGENT=1 fetch-track help [command]
AGENT=1 fetch-track completion
AGENT=1 fetch-track completion bash [--no-descriptions]
AGENT=1 fetch-track completion fish [--no-descriptions]
AGENT=1 fetch-track completion powershell [--no-descriptions]
AGENT=1 fetch-track completion zsh [--no-descriptions]
```

- `fetch-track upgrade` (aliases: `self-update`, `update-self`) checks GitHub Releases for a newer `fetch-track` version and replaces the running executable in-place.
- `fetch-track skill` prints the Skills CLI installation command and repository URL to `stdout`.
- `fetch-track help` displays help for any command in the hierarchy.
- `fetch-track completion` and its shell subcommands (`fetch-track completion bash`, `fetch-track completion fish`, `fetch-track completion powershell`, `fetch-track completion zsh`) generate shell autocompletion scripts; pass `--no-descriptions` to omit completion descriptions.

## Flags & Options

| Flag | Short | Type | Default | Scope | Description |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `--auto-install` | | `bool` | `false` | Root, `track verify` | Automatically install missing external dependencies without interactive prompting |
| `--confirm-fingerprint` | | `bool` | `true` | Root | Verify downloaded audio via acoustic fingerprinting (`tag-track` / Shazam) and retry next candidate on mismatch (inverse hidden alias: `--skip-fingerprint`) |
| `--debug` | | `bool` | `false` | Global | Prefix log lines with elapsed millisecond timestamps (`[123ms]`) and write diagnostic dependency checks to `stderr` |
| `--help` | `-h` | `bool` | `false` | All | Print command help screen to `stdout` |
| `--interactive` | `-i` | `bool` | `false` | Root | Prompt interactively in a TTY to approve, skip, or select from ranked candidates before downloading |
| `--job-id` | | `string` | `""` | Root | Job identifier attached to every `fetch-track` progress event; auto-generates a random ID when empty |
| `--js-runtime` | | `string` | `"auto"` | Root | JavaScript runtime passed to `yt-dlp` (`auto`, `deno`, `node`, `bun`, `quickjs`, `none`, `off`) |
| `--json` | | `bool` | `false` | Global | Emit a single machine-readable JSON result object to `stdout` while routing human logs to `stderr` |
| `--min-bandwidth-khz` | | `float64` | `16` | Root, `track verify` | Minimum acceptable audio frequency cutoff in kHz (e.g. `18.5`) before triggering a low-bandwidth warning |
| `--no-cache` | | `bool` | `false` | Root | Bypass local disk cache for search queries, probed URL metadata, and cover art |
| `--no-descriptions` | | `bool` | `false` | `completion` | Disable completion descriptions in generated shell scripts |
| `--out-dir` | `-o` | `string` | `"."` | Root | Destination directory for downloaded and tagged audio files |
| `--progress-socket` | | `string` | `""` | Root | Shorthand alias for `--progress-target` |
| `--progress-target` | | `string` | `""` | Root | Destination URI for NDJSON telemetry events (`unix:///path/to.sock`, `/path/to.sock`, `tcp://127.0.0.1:9099`, `fd://3`, `stdout`, `stderr`) |
| `--skip-metadata` | | `bool` | `false` | Root | Skip `tag-track` metadata identification and cover art embedding (hidden alias: `--no-metadata`) |
| `--skip-verify` | | `bool` | `false` | Root | Skip PCM decoding and Goertzel spectral quality verification |
| `--sources` | `-s` | `string` | `"youtube,soundcloud"` | Root | Comma-separated sources to query in parallel (`youtube`, `soundcloud`, `bandcamp`) |
| `--verbose` | `-v` | `bool` | `false` | Global | Print detailed subprocess and search diagnostics (suppressed when `--json` is set) |
| `--version` | | `bool` | `false` | Root | Print raw semantic version string followed by a newline (`1.2.3\n`) and exit |

## Environment Variables

- `AGENT`: Set to `1`, `true`, or `yes` to enable token-conservative key-value output and agent help screens.
- `FETCH_TRACK_PROGRESS_TARGET`: Fallback URI for streaming NDJSON progress events when `--progress-target` and `--progress-socket` are unset.
- `XDG_CACHE_HOME`: Base directory for cached search results and metadata (`$XDG_CACHE_HOME/fetch-track`, fallback `~/.cache/fetch-track`).
- `XDG_DATA_HOME`: Base directory for managed external binaries (`$XDG_DATA_HOME/fetch-track/bin`, fallback `~/.local/share/fetch-track/bin`).

## Output Contracts & Error Handling

- **Agent Mode (`AGENT=1`)**: Prints flat `key: value` lines (`target:`, `candidate:`, `duration:`, `bandwidth:`, `dynamics:`, `status:`, `metadata:`, `output:`). On failure, prints `target: <input>`, `status: error`, and `error: <sanitized message>` before returning a non-zero exit code.
- **JSON Mode (`--json`)**: Writes a single `JSONExecutionResult` object (`target`, `status`, `candidate_count`, `selected_candidate`, `output_file`, `verification`, `metadata`, `error`) to `stdout` and sends progress/diagnostic logs to `stderr`.
- **Progress Telemetry (`--progress-target`)**: Streams newline-delimited JSON (`phase_start`, `candidate_found`, `candidate_selected`, `progress`, `complete`, `error`) with monotonic `sequence` (starting at `1`), `job_id`, UTC `timestamp`, and milestone `percent` (`0` to `100`).
- **Home Directory Folding**: All file paths printed in human and agent text outputs, logs, and errors fold the user's home directory prefix to `~` (e.g. `~/tracks/Artist - Title.m4a`).
- **Temporary Files**: Creates working files inside `.tmp/` in the current working directory and removes `.tmp/` on exit if it did not exist prior to invocation.

## Workflows

```bash
# Acquire an extended DJ mix in agent mode with structured JSON output
AGENT=1 fetch-track --json -o tracks "Boris Brejcha - Space X"

# Verify audio bandwidth and gain offset of a local track
AGENT=1 fetch-track track verify --min-bandwidth-khz 18.5 "tracks/Boris Brejcha - Space X.m4a"

# Stream acquisition progress over a UNIX socket with an explicit job ID
AGENT=1 fetch-track --progress-target "unix://.tmp/progress.sock" --job-id "job-42" "Artist - Track"

# Compose with tag-track by skipping built-in metadata enrichment
AGENT=1 fetch-track --skip-metadata -o .tmp "Artist - Track"
```
