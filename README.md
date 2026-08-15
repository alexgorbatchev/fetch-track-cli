# fetch-track

> **⚠️ WARNING / DISCLAIMER:** This tool is strictly for amateur and bedroom DJs practicing at home or playing informal sets who are not seeking professional careers. Professional DJs must source their music from legitimate commercial sources (Beatport, Bandcamp, Juno Download, iTunes, or authorized record pools).

`fetch-track` is a Go CLI tool for acquiring, inspecting, and enriching high-fidelity single tracks for DJ collections.

It searches configured sources (**YouTube, SoundCloud, Bandcamp**) in parallel for full/extended DJ mixes. When given a direct URL or search query, it extracts search terms, evaluates audio candidates concurrently across all sources for frequency response and track length, downloads native audio streams, performs spectral bandwidth & loudness analysis, resolves metadata with multi-tiered API fallbacks, and embeds 1400x1400 cover art compatible with DJ software, hardware decks, and file manager previews.

---

## Output Format & Compatibility

By default, acquired tracks are dropped directly into your current working directory (`.`):

| Specification | Details |
| :--- | :--- |
| **Container & Format** | **`.m4a`** (MPEG-4 Part 14 Audio) |
| **Audio Codec** | Native High-Bitrate **AAC** (up to 20 kHz spectral bandwidth) |
| **Embedded Artwork** | **1400x1400** high-resolution cover art embedded as an **MP4 `covr` atom** |
| **Embedded Tags** | Title, Artist, Album, Genre, Release Year |
| **Filename Pattern** | `./<Artist> - <Title>.m4a`<br>*(e.g., `./Boris Brejcha - Space X.m4a` — stripped of video IDs, brackets `[...]`, or illegal characters)* |
| **Compatibility** | Hardware DJ decks, Rekordbox, Serato DJ, Traktor, and **macOS Finder** QuickLook previews |

---

## Metadata Sources & Fallback Chain

Metadata and high-resolution cover art are fetched without commercial API keys using a 3-tier fallback resolution chain:

```
[ Track Search Query / Clean Filename ]
                   │
                   ▼
    ┌──────────────────────────────┐
    │ 1. iTunes Search API         │ ──► (Match Found) ──► Returns Title, Artist, Album, Genre,
    └──────────────────────────────┘                        Year, and 1400x1400 High-Res Cover Art
                   │ (No match)
                   ▼
    ┌──────────────────────────────┐
    │ 2. MusicBrainz API           │ ──► (Match Found) ──► Returns Recording Data +
    │    + Cover Art Archive       │                        Release Cover Art Archive URL
    └──────────────────────────────┘
                   │ (No match)
                   ▼
    ┌──────────────────────────────┐
    │ 3. YouTube / Raw Fallback    │ ──► Uses cleaned Uploader / Title from URL / filename
    └──────────────────────────────┘
```

---

## Features

- **Single-Track DJ Acquisition:** Takes a search query or direct URL and automates search term extraction, parallel multi-source candidate evaluation, downloading, verification, and tagging in one step.
- **Default CWD Output:** Drops acquired `.m4a` tracks into the current working directory (`.`) by default. Override with `-o` / `--out-dir`.
- **LLM Agent Mode (`AGENT=1`):** When `AGENT=1` environment variable is set, outputs compact, token-conservative key-value text designed for LLM agents and fast human reading (no emojis, banner boxes, or JSON syntax overhead).
- **Direct URL Search Term Extraction:** If given a direct URL (e.g. a YouTube or SoundCloud link), `fetch-track` probes its metadata to extract artist and track title, pools the direct URL with parallel search results across all configured sources, and guarantees selecting the best full Extended/Original DJ Mix track.
- **Parallel Multi-Source Search:** Queries configured sources (**YouTube, SoundCloud, Bandcamp**) concurrently in parallel by default (configurable via `-s`).
- **Parallel Candidate Audio Inspection:** Concurrently samples audio streams and verifies track length across top candidates from all sources before selecting the winning track.
- **Full DJ Mix Candidate Ranking:** Evaluates search candidates and ranks full extended/original DJ mixes (4.5 to 13 minutes) while filtering out short radio edits and continuous album mixes.
- **Spectral Bandwidth & Quality Inspection:** Analyzes audio frequency response up to 20 kHz via PCM Goertzel analysis to detect low-bitrate rips or transcoded uploads.
- **Gain Staging & DJ Trim Calculation:** Measures Peak dBFS and RMS loudness to recommend channel trim offsets before mixing.

---

## Prerequisites

Ensure `go` (1.23+), `yt-dlp`, and `ffmpeg` / `ffprobe` are installed and available in your system `PATH`:

```bash
go version
yt-dlp --version
ffmpeg -version
ffprobe -version
```

---

## Installation

Build the binary directly with Go:

```bash
go build -o fetch-track ./cmd/fetch-track
```

Optionally install it to your Go bin directory:

```bash
go install ./cmd/fetch-track
```

---

## Quick Start

### 1. Parallel Multi-Source Acquisition (Drops into current working directory)

Search YouTube, SoundCloud, and Bandcamp in parallel:

```bash
./fetch-track "Boris Brejcha - Space X"
```

### 2. LLM Agent Mode (`AGENT=1`)

For compact, token-conservative key-value output formatted for LLM agents:

```bash
AGENT=1 ./fetch-track "Boris Brejcha - Space X"
```

Sample Agent Output:
```
target: Boris Brejcha - Space X
candidate: Boris Brejcha - Space X (Extended Mix) [soundcloud] (https://soundcloud.com/boris-brejcha/space-x-extended-mix)
duration: 8:23 (Original / Extended DJ Mix)
bandwidth: 20 kHz (High Fidelity (>=18.5 kHz))
dynamics: peak=0.04 dBFS rms=-9.44 dBFS trim=-2.6 dB
status: PASS
metadata: "Boris Brejcha - Space X" (Space X - Single, 2024) [iTunes API]
output: ./Boris Brejcha - Space X.m4a
```

### 3. Specify Custom Output Directory

Use `-o` to save tracks into a specific directory instead of cwd:

```bash
./fetch-track -o tracks "Boris Brejcha - Space X"
```

### 4. Direct URL Search

If given a direct link (e.g., a short 2-minute radio edit link), `fetch-track` extracts the track title and artist, searches SoundCloud/Bandcamp/YouTube in parallel, and selects the full 8-minute Extended Mix:

```bash
./fetch-track "https://www.youtube.com/watch?v=short_radio_edit_id"
```

### 5. Stand-Alone Audio Quality Verification

Inspect any local track or remote URL without downloading:

```bash
./fetch-track verify "Boris Brejcha - Space X.m4a"
```

---

## CLI Reference

### Global Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--out-dir` | `-o` | `.` | Output directory for downloaded tracks (default: current working directory) |
| `--sources` | `-s` | `youtube,soundcloud,bandcamp` | Comma-separated list of search sources |
| `--skip-verify` | | `false` | Skip DJ audio quality & spectrum inspection |
| `--skip-metadata` | | `false` | Skip metadata enrichment and cover art tagging |
| `--verbose` | `-v` | `false` | Enable verbose logging |

---

## Testing

Run unit tests for all packages:

```bash
go test -v ./...
```

Run static analysis checks:

```bash
go vet ./...
```
