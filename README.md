# fetch-track

`fetch-track` is a Go CLI tool for acquiring, inspecting, and enriching high-fidelity single tracks for home DJ setups (Engine DJ, Pioneer Rekordbox, Serato, Traktor).

It searches configured sources (**YouTube, SoundCloud, Bandcamp**) in parallel for full/extended DJ mixes. When given a direct URL or search query, it extracts search terms, evaluates audio candidates concurrently across all sources for frequency response and track length, downloads native audio streams, performs spectral bandwidth & loudness analysis, resolves metadata with multi-tiered API fallbacks, and embeds 1400x1400 cover art compatible with Engine DJ hardware decks and macOS Finder QuickLook previews.

---

## Features

- **Single-Track DJ Acquisition:** Takes a search query or direct URL and automates search term extraction, parallel multi-source candidate evaluation, downloading, verification, and tagging in one step.
- **Direct URL Search Term Extraction:** If given a direct URL (e.g. a YouTube or SoundCloud link), `fetch-track` probes its metadata to extract the artist and track title, pools the direct URL with parallel search results across all configured sources, and guarantees selecting the best full Extended/Original DJ Mix track (overriding short radio edit links if a better mix version exists).
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

Sample Output:
```
=======================================================
🎧 DJ FULL MIX TRACK ACQUISITION PIPELINE
=======================================================
Target: Boris Brejcha - Space X

🔍 Searching sources (youtube, soundcloud, bandcamp) in parallel for best Extended DJ MIX track...
  ✅ Selected Best Full Extended DJ Mix Candidate [SOUNDCLOUD]: https://soundcloud.com/boris-brejcha/space-x-extended-mix
  📊 Candidate Spectrum: 20 kHz bandwidth | Rank Score: 150

📥 Step 2: Downloading audio stream & artwork...
  Saved: Boris Brejcha - Space X (Extended Mix).m4a

🔍 Step 3: Running Final DJ Audio Quality & Spectrum Inspection...
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

### 2. Passing a Direct URL (Probes & Overrides Short Edits for Best Mix)

If you pass a direct link (e.g., a 2-minute YouTube short edit link), `fetch-track` extracts its track title and artist, searches SoundCloud/Bandcamp/YouTube in parallel, and selects the full 8-minute Extended Mix:

```bash
./fetch-track "https://www.youtube.com/watch?v=short_radio_edit_id"
```

### 3. Restrict to Specific Sources

```bash
./fetch-track -s "youtube,soundcloud" "Boris Brejcha - Space X"
```

### 4. Run Stand-Alone Audio Quality Verification

Inspect any local track or remote URL without downloading:

```bash
./fetch-track verify "tracks/Boris Brejcha - Space X.m4a"
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
