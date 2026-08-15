# fetch-track

`fetch-track` is a Go CLI tool for acquiring, inspecting, and enriching high-fidelity single tracks for home DJ setups (Engine DJ, Pioneer Rekordbox, Serato, Traktor).

It searches YouTube for full/extended DJ mixes, downloads native `.m4a` audio streams, performs spectral bandwidth & loudness analysis, resolves metadata with multi-tiered API fallbacks, and embeds 1400x1400 cover art compatible with Engine DJ hardware decks and macOS Finder QuickLook previews.

---

## Features

- **Single-Track DJ Acquisition:** Takes a single YouTube URL or track search query and automates candidate ranking, downloading, verification, and tagging in one step.
- **Full DJ Mix Candidate Ranking:** Evaluates search candidates on YouTube and ranks full extended/original DJ mixes (4.5 to 13 minutes) while filtering out short radio edits and continuous album mixes.
- **Engine DJ & macOS Finder Artwork Compatibility:** Saves tracks in `.m4a` container format with embedded 1400x1400 MP4 `covr` artwork, guaranteeing native artwork rendering across DJ software and hardware decks.
- **Spectral Bandwidth & Quality Inspection:** Analyzes audio frequency response up to 20 kHz via PCM Goertzel analysis to detect low-bitrate rips or transcoded YouTube uploads.
- **Gain Staging & DJ Trim Calculation:** Measures Peak dBFS and RMS loudness to recommend channel trim offsets before mixing.
- **Multi-Tiered Metadata Fallback:** Queries iTunes Search API (with 1400x1400 artwork), falling back to MusicBrainz API + Cover Art Archive, and local YouTube metadata fallback.

---

## Prerequisites

Ensure `go` (1.23+), `yt-dlp`, `ffmpeg` / `ffprobe`, and optionally `just` are installed and available in your system `PATH`:

```bash
go version
yt-dlp --version
ffmpeg -version
ffprobe -version
just --version
```

---

## Installation

Build the binary using `just` or `go`:

```bash
# Using just
just build

# Or directly with Go
go build -o fetch-track ./cmd/fetch-track
```

Optionally install it to your Go bin directory:

```bash
just install
# or: go install ./cmd/fetch-track
```

---

## Quick Start

### 1. Download and Process a Track by Search Query

Query a track by artist and title:

```bash
just run "Boris Brejcha - Space X"
# or: ./fetch-track "Boris Brejcha - Space X"
```

Sample Output:
```
=======================================================
🎧 DJ FULL MIX TRACK ACQUISITION PIPELINE
=======================================================
Target: Boris Brejcha - Space X

🔍 Step 1: Inspecting top YouTube candidates for Full Extended DJ Mix...
  ✅ Selected Full DJ Mix Candidate: https://www.youtube.com/watch?v=T4EGCbhVbnY

📥 Step 2: Downloading audio stream & artwork...
  Saved: Boris Brejcha - Space X (Unreleased Extended Fix).m4a

🔍 Step 3: Running DJ Audio Quality & Spectrum Inspection...
  Duration  : 8:23 (Original / Extended DJ Mix)
  Bandwidth : High Fidelity (>=18.5 kHz) (20 kHz)
  Peak / RMS: 0.04 dBFS / -9.44 dBFS
  DJ Trim   : -2.6 dB
  Status    : [ PASS ]

🖼️ Step 4: Enriching metadata & 1400x1400 cover art via API fallback...
  Matched  : "Boris Brejcha - Space X" (Space X - Single, 2024)
  Source   : iTunes API

=======================================================
✅ TRACK ACQUISITION COMPLETE: tracks/Boris Brejcha - Space X.m4a
=======================================================
```

### 2. Download a Direct YouTube URL

```bash
just run "https://www.youtube.com/watch?v=T4EGCbhVbnY"
```

### 3. Run Stand-Alone Audio Quality Verification

Inspect any local track or YouTube link without downloading:

```bash
just verify "tracks/Boris Brejcha - Space X.m4a"
# or: ./fetch-track verify "tracks/Boris Brejcha - Space X.m4a"
```

---

## CLI Reference

### Global Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--out-dir` | `-o` | `tracks` | Output directory for acquired tracks |
| `--skip-verify` | | `false` | Skip DJ audio quality & spectrum inspection |
| `--skip-metadata` | | `false` | Skip metadata enrichment and cover art tagging |
| `--verbose` | `-v` | `false` | Enable verbose logging |

### Commands

- `fetch-track <query_or_url>`: Full single-track acquisition pipeline.
- `fetch-track verify <file_path_or_url>`: Stand-alone DJ quality inspection.

---

## Development & Recipes

List all available `just` recipes:

```bash
just --list
```

Available recipes:
- `just build`: Compile `fetch-track` binary.
- `just install`: Install `fetch-track` binary to `$GOPATH/bin`.
- `just test`: Run all unit tests (`go test -v ./...`).
- `just vet`: Run Go static analysis (`go vet ./...`).
- `just fmt`: Format source code (`go fmt ./...`).
- `just clean`: Remove build artifacts and temp files.

---

## Project Structure

```
fetch-track-cli/
├── cmd/
│   └── fetch-track/        # Main Cobra CLI entrypoint
├── internal/
│   ├── downloader/         # YouTube candidate search, ranking, and yt-dlp downloader
│   ├── metadata/           # iTunes / MusicBrainz client, filename sanitizer, and FFmpeg tagger
│   ├── pipeline/           # Track acquisition workflow orchestrator
│   └── verifier/           # PCM spectral analysis (Goertzel), mix structure, loudness checks
├── AGENTS.md               # Workspace guidelines and rules for coding agents
├── justfile                # Task runner recipes for build, test, run, and clean
├── go.mod                  # Go module definitions
├── .gitignore              # Ignored binaries, temp files, and tracks
└── README.md               # Overview and usage documentation
```
