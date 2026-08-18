# fetch-track

`fetch-track` is a CLI tool for acquiring high-fidelity music tracks for amateur and bedroom DJ collections. 

> **⚠️ Intended Audience & Legal Disclaimer:**  
> `fetch-track` is strictly intended for **amateur and bedroom DJs** practicing at home or playing non-commercial sets who are not seeking to become professional DJs. **Working professional DJs and commercial performers must source music from legitimate sources** (such as Beatport, Bandcamp purchases, Juno Download, iTunes, or authorized record pools).

## What It Does

`fetch-track` automates single-track acquisition for home DJ setups:

- **Full DJ Mix Acquisition**: Searches YouTube and SoundCloud in parallel for Extended, Original, Club, or Dub Mixes with beatmatchable intro/outro sections.
- **Zero-Loss Quality**: Downloads native high-bitrate audio streams directly without lossy-to-lossy re-encoding.
- **Spectral & Loudness Analysis**: Runs Goertzel PCM frequency analysis up to 20 kHz and calculates channel Gain Offset for seamless DJ level-matching.
- **Metadata & 1400x1400 Artwork**: Enriches files with official iTunes/MusicBrainz tags and embeds 1400x1400 cover art compatible with Engine DJ decks, Pioneer Rekordbox, Serato DJ, Traktor, and macOS Finder.
- **Clean File Organization**: Saves tracks in `./tracks/` using standardized `<Artist> - <Title>.<ext>` filenames, stripped of video IDs and brackets.

## How It Works

- **Parallel Search**: Queries **YouTube** and **SoundCloud** (configurable) concurrently for original and extended mixes.
- **Candidate Evaluation**: Fuzzy-matches titles/artists and ranks candidate tracks by duration, keywords, and relevance.
- **Stream Extraction**: Downloads native high-bitrate audio streams directly to disk without lossy re-encoding.
- **Spectrum & Loudness Analysis**: Runs Goertzel PCM spectral transform to detect low-bitrate rips and calculates Gain Offset.
- **Metadata & Artwork Enrichment**: Resolves official track metadata via iTunes and MusicBrainz APIs and embeds 1400x1400 cover art.

---

## Output Format & Compatibility

All acquired tracks are stored as `.m4a` files with clean filenames and embedded high-resolution artwork:

| Specification | Details |
| :--- | :--- |
| **Container & Format** | **`.m4a`** (MPEG-4 Part 14 Audio) |
| **Audio Codec** | Native High-Bitrate **AAC** (up to 20 kHz spectral bandwidth) |
| **Embedded Artwork** | **1400x1400** high-resolution cover art embedded as an **MP4 `covr` atom** |
| **Embedded Tags** | Title, Artist, Album, Genre, Release Year |
| **Filename Pattern** | `<outDir>/<Artist> - <Title>.m4a`<br>*(e.g., `./Boris Brejcha - Space X.m4a` — stripped of video IDs, brackets `[...]`, or illegal characters)* |
| **Hardware & App Support** | **Engine DJ OS (Denon)**, Pioneer Rekordbox, Serato DJ, Traktor, and **macOS Finder** QuickLook previews |

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

1. **iTunes Search API (Primary):**
   - Queries `https://itunes.apple.com/search?term=<query>&entity=song&limit=1`.
   - Returns exact Track Name, Artist, Album, Primary Genre, and Release Year.
   - Upgrades `artworkUrl100` (`100x100bb`) to **`1400x1400bb`** for high-res album art.
2. **MusicBrainz API + Cover Art Archive (Fallback 1):**
   - Queries `https://musicbrainz.org/ws/2/recording/?query=<query>&fmt=json`.
   - Fetches recording data and queries `https://coverartarchive.org/release/<release_id>` for cover images.
3. **YouTube / Raw Local Fallback (Fallback 2):**
   - Used if online music databases return no match, extracting title and uploader from URL or filename.

---

## Features

- **Single-Track DJ Acquisition:** Takes a search query or direct URL and automates search term extraction, parallel multi-source candidate evaluation, downloading, verification, and tagging in one step.
- **Parallel Multi-Source Search:** Queries configured sources (**YouTube, SoundCloud**) concurrently in parallel by default (configurable via `-s`).
- **Direct URL Search Term Extraction:** If given a direct URL (e.g. a YouTube or SoundCloud link), `fetch-track` probes its metadata to extract artist and track title, pools the direct URL with parallel search results across all configured sources, and guarantees selecting the best full Extended/Original DJ Mix track.
- **LLM Agent Mode (`AGENT=1`):** When `AGENT=1` environment variable is set, outputs compact, token-conservative key-value text designed for LLM agents and fast human reading.
- **Full DJ Mix Candidate Ranking:** Evaluates search candidates across sources and ranks full extended/original DJ mixes (4.5 to 13 minutes) while filtering out short radio edits and continuous album mixes.
- **Engine DJ & High-Res Artwork Compatibility:** Saves tracks in `.m4a` container format with embedded 1400x1400 MP4 `covr` artwork, guaranteeing native artwork rendering across DJ software, hardware decks, and operating systems.
- **Spectral Bandwidth & Quality Inspection:** Analyzes audio frequency response up to 20 kHz via PCM Goertzel analysis to detect low-bitrate rips or transcoded uploads.
- **Gain Staging & Gain Offset Calculation:** Measures Peak dBFS and RMS loudness to recommend channel gain offsets before mixing.
- **Multi-Tiered Metadata Fallback:** Queries iTunes Search API (with 1400x1400 artwork), falling back to MusicBrainz API + Cover Art Archive, and local metadata fallback.

---

## Prerequisites

`fetch-track` requires `yt-dlp` and `ffmpeg` / `ffprobe` available in your system `PATH`:

```bash
yt-dlp --version
ffmpeg -version
ffprobe -version
```

---

## Installation

### 1. Download Latest Release Binary (Recommended)

Download the latest pre-compiled release binary for your platform using GitHub CLI (`gh`):

```bash
# macOS (Apple Silicon / ARM64)
gh release download --repo alexgorbatchev/fetch-track-cli --pattern "*darwin_arm64*.tar.gz" --clobber
tar -xzf fetch-track*.tar.gz
sudo mv fetch-track /usr/local/bin/

# Linux (AMD64)
gh release download --repo alexgorbatchev/fetch-track-cli --pattern "*linux_amd64*.tar.gz" --clobber
tar -xzf fetch-track*.tar.gz
sudo mv fetch-track /usr/local/bin/
```

Or download directly from [GitHub Releases Latest](https://github.com/alexgorbatchev/fetch-track-cli/releases/latest).

### 2. Install via Go

```bash
go install github.com/dj/fetch-track-cli/cmd/fetch-track@latest
```

---

## Quick Start

### 1. Parallel Multi-Source Acquisition

Search YouTube and SoundCloud in parallel:

```bash
just run "Boris Brejcha - Space X"
# or: ./bin/fetch-track "Boris Brejcha - Space X"
```

Sample Output:
```
searching: youtube, soundcloud
candidates:
  - "Boris Brejcha - Space X (Radio Edit)" [youtube 3:15]
  - "Boris Brejcha - Space X (Extended Mix)" [soundcloud 8:23]
selected: "Boris Brejcha - Space X (Extended Mix)" [soundcloud 8:23] score=150

downloading audio stream & artwork (https://soundcloud.com/boris-brejcha/space-x-extended-mix)
  Saved: Boris Brejcha - Space X (Extended Mix).m4a

running audio quality & spectrum inspection
  Duration: 8:23 (Original / Extended DJ Mix)
  Bandwidth: High Fidelity (>=18.5 kHz) (20 kHz)
  Peak / RMS: 0.04 dBFS / -9.44 dBFS
  Gain Offset: -2.6 dB
  STATUS: High fidelity audio suitable for mixing.

enriching metadata & cover art via API fallback
  Matched: "Boris Brejcha - Space X" (Space X - Single, 2024)
  Source: iTunes API

DONE: tracks/Boris Brejcha - Space X.m4a
```

### 2. LLM Agent Mode (`AGENT=1`)

For compact, token-conservative key-value output formatted for LLM agents:

```bash
AGENT=1 ./bin/fetch-track "Boris Brejcha - Space X"
```

Sample Agent Output:
```
target: Boris Brejcha - Space X
candidate: Boris Brejcha - Space X (Extended Mix) [soundcloud] (https://soundcloud.com/boris-brejcha/space-x-extended-mix)
duration: 8:23 (Original / Extended DJ Mix)
bandwidth: 20 kHz (High Fidelity (>=18.5 kHz))
dynamics: peak=0.04 dBFS rms=-9.44 dBFS gain=-2.6 dB
status: PASS
metadata: "Boris Brejcha - Space X" (Space X - Single, 2024) [iTunes API]
output: tracks/Boris Brejcha - Space X.m4a
```

### 3. Direct URL Search

If given a direct link (e.g., a short 2-minute radio edit link), `fetch-track` extracts the track title and artist, searches SoundCloud/Bandcamp/YouTube in parallel, and selects the full 8-minute Extended Mix:

```bash
./bin/fetch-track "https://www.youtube.com/watch?v=short_radio_edit_id"
```

### 4. Restrict Search Sources

```bash
./bin/fetch-track -s "youtube,soundcloud" "Boris Brejcha - Space X"
```

### 5. Stand-Alone Audio Quality Verification

Inspect any local track or remote URL without downloading:

```bash
just verify "tracks/Boris Brejcha - Space X.m4a"
# or: ./bin/fetch-track verify "tracks/Boris Brejcha - Space X.m4a"
```

---

## CLI Reference

### Global Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--out-dir` | `-o` | `tracks` | Output directory for acquired tracks |
| `--sources` | `-s` | `youtube,soundcloud` | Comma-separated list of search sources |
| `--skip-verify` | | `false` | Skip DJ audio quality & spectrum inspection |
| `--skip-metadata` | | `false` | Skip metadata enrichment and cover art tagging |
| `--verbose` | `-v` | `false` | Enable verbose logging |

### Commands

- `fetch-track <query_or_url>`: Full single-track acquisition pipeline.
- `fetch-track verify <file_path_or_url>`: Stand-alone DJ quality inspection.
- `fetch-track --version`: Print version information.

---

## Technical Details

### Re-Encoding Chain & Audio Processing

`fetch-track` preserves original source audio quality by using stream copying during tagging and in-memory decoding during quality analysis:

1. **Source Stream Extraction (`yt-dlp`):**  
   Searches and requests native high-bitrate audio streams (`-f "bestaudio/best"`). Extracts the stream directly to disk without transcoding.

2. **In-Memory Spectral Analysis (`ffmpeg`):**  
   Decodes a 30-second audio snippet directly to a 32-bit float PCM memory buffer (`pipe:1`) for Goertzel frequency analysis and Peak/RMS loudness measurement. Decoded PCM data is analyzed in memory and is never written to disk.

3. **Lossless Tagging & Artwork Embedding (`ffmpeg`):**  
   Embeds metadata and 1400x1400 cover art into the M4A container using audio stream copying (`-c:a copy`). The underlying AAC bitstream remains untouched, avoiding lossy re-encoding artifacts.

4. **Atomic File Finalization:**  
   Writes the tagged track to a temporary file before atomically renaming it to `<outDir>/<Artist> - <Title>.m4a` to ensure valid file state on disk.

---

## License

This project is licensed under the [MIT License](LICENSE).
