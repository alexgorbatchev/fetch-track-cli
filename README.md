# fetch-track

`fetch-track` is a Go CLI tool for acquiring, inspecting, and enriching high-fidelity single tracks for home DJ setups (Engine DJ, Pioneer Rekordbox, Serato, Traktor).

It searches configured sources (**YouTube, SoundCloud, Bandcamp**) in parallel for full/extended DJ mixes. When given a direct URL or search query, it extracts search terms, evaluates audio candidates concurrently across all sources for frequency response and track length, downloads native audio streams, performs spectral bandwidth & loudness analysis, resolves metadata with multi-tiered API fallbacks, and embeds 1400x1400 cover art compatible with Engine DJ hardware decks and macOS Finder QuickLook previews.

---

## Features

- **Single-Track DJ Acquisition:** Takes a search query or direct URL and automates search term extraction, parallel multi-source candidate evaluation, downloading, verification, and tagging in one step.
- **LLM Agent Mode (`AGENT=1`):** When `AGENT=1` environment variable is set, outputs compact, token-conservative key-value text designed for LLM agents and fast human reading (no emojis, banner boxes, or JSON syntax overhead).
- **Direct URL Search Term Extraction:** If given a direct URL (e.g. a YouTube or SoundCloud link), `fetch-track` probes its metadata to extract artist and track title, pools the direct URL with parallel search results across all configured sources, and guarantees selecting the best full Extended/Original DJ Mix track.
- **Parallel Multi-Source Search:** Queries configured sources (**YouTube, SoundCloud, Bandcamp**) concurrently in parallel.
- **Parallel Candidate Audio Inspection:** Concurrently samples audio streams and verifies track length across top candidates from all sources before selecting the winning track.
- **Full DJ Mix Candidate Ranking:** Evaluates search candidates and ranks full extended/original DJ mixes (4.5 to 13 minutes) while filtering out short radio edits and continuous album mixes.
- **Engine DJ & macOS Finder Artwork Compatibility:** Saves tracks in `.m4a` container format with embedded 1400x1400 MP4 `covr` artwork, guaranteeing native artwork rendering across DJ software and hardware decks.
- **Spectral Bandwidth & Quality Inspection:** Analyzes audio frequency response up to 20 kHz via PCM Goertzel analysis to detect low-bitrate rips or transcoded uploads.
- **Gain Staging & DJ Trim Calculation:** Measures Peak dBFS and RMS loudness to recommend channel trim offsets before mixing.
- **Multi-Tiered Metadata Fallback:** Queries iTunes Search API (with 1400x1400 artwork), falling back to MusicBrainz API + Cover Art Archive, and local metadata fallback.

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

### 1. Parallel Multi-Source Acquisition

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
output: tracks/Boris Brejcha - Space X.m4a
```

### 3. Direct URL Search

If given a direct link (e.g., a short 2-minute radio edit link), `fetch-track` extracts the track title and artist, searches SoundCloud/Bandcamp/YouTube in parallel, and selects the full 8-minute Extended Mix:

```bash
./fetch-track "https://www.youtube.com/watch?v=short_radio_edit_id"
```

### 4. Restrict Search Sources

```bash
./fetch-track -s "youtube,soundcloud" "Boris Brejcha - Space X"
```

### 5. Stand-Alone Audio Quality Verification

Inspect any local track or remote URL without downloading:

```bash
./fetch-track verify "tracks/Boris Brejcha - Space X.m4a"
```

Agent verify mode:
```bash
AGENT=1 ./fetch-track verify "tracks/Boris Brejcha - Space X.m4a"
```

---

## CLI Reference

### Global Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--out-dir` | `-o` | `tracks` | Output directory for acquired tracks |
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
