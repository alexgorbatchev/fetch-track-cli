A fast, lightweight CLI tool with AI agent support for bedroom and amateur DJs to find, inspect, and download high-quality single tracks for their DJ sets.

# What It Does

- Searches YouTube, SoundCloud, and Bandcamp in parallel for full extended DJ mixes with mixable intro/outro sections.
- Matches artist and title while filtering out short radio edits, snippets, or multi-hour mix compilations.
- Downloads native high-bitrate audio streams directly without lossy re-encoding.
- Analyzes spectral frequency bandwidth (Goertzel algorithm) and computes mixer volume Gain Offset.
- Identifies tracks via acoustic audio fingerprinting (AcoustID and Apple Shazam) with API fallbacks (iTunes and MusicBrainz) to embed canonical metadata and 1400x1400 album art.

# How It Works

- Searches configured streaming platforms simultaneously for your requested artist and title.
- Evaluates candidate durations and titles to select the full-length extended DJ mix.
- Downloads the highest fidelity native audio stream without re-encoding.
- Inspects audio frequency response and calculates mixer Gain Offset.
- Enriches the track with high-resolution 1400x1400 artwork and canonical tags before saving.

# How it Really Works

- Discovers candidate streams using concurrent `yt-dlp` JSON scrapers with fuzzy title and duration scoring heuristics.
- Extracts PCM audio samples via `ffmpeg` and executes Goertzel frequency bin analysis to detect audio cutoff thresholds and RMS loudness.
- Generates Chromaprint acoustic fingerprints and landmark constellations to query AcoustID and Shazam recognition services in pure Go.
- Normalizes and center-crops downloaded album artwork to 1400x1400 square format and embeds ID3/MP4 metadata using stream-copy mode (`-c:a copy`).
- Streams real-time NDJSON progress events over UNIX domain sockets, TCP, or file descriptors when executed by AI agents or supervisor orchestrators.

# Prerequisites

- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp#installation) (version 2024.08.01 or newer) - Audio stream extraction backend.
- [`ffmpeg` & `ffprobe`](https://ffmpeg.org/download.html) (version 4.4 or newer) - Audio analysis and metadata tagging.

# Installation

Download the latest prebuilt binary from GitHub Releases:

```bash
# Using GitHub CLI
gh release download --repo alexgorbatchev/fetch-track-cli --pattern 'fetch-track_*_darwin_arm64.tar.gz'
tar -xzf fetch-track_*_darwin_arm64.tar.gz
chmod +x fetch-track
mv fetch-track ~/.local/bin/
```

Or via direct download:

```bash
curl -sSL https://github.com/alexgorbatchev/fetch-track-cli/releases/latest/download/fetch-track_darwin_arm64.tar.gz | tar -xz
chmod +x fetch-track
mv fetch-track ~/.local/bin/
```

# Quick Start

```bash
# Download a full DJ mix
fetch-track "Boris Brejcha - Space X"

# Download from direct URL
fetch-track "https://soundcloud.com/boris-brejcha/space-x-extended-mix"

# Search specific sources
fetch-track -s "youtube,soundcloud" "Boris Brejcha - Space X"

# Interactively choose candidate
fetch-track -i "Boris Brejcha - Space X"

# Inspect local file quality
fetch-track verify "Boris Brejcha - Space X.m4a"

# Manage external dependencies
fetch-track dependencies
fetch-track deps install
fetch-track deps update

# Upgrade CLI binary in-place
fetch-track upgrade
```

# Options & Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--out-dir <path>` | `-o` | `.` | Output directory for downloaded tracks |
| `--sources <list>` | `-s` | `youtube,soundcloud` | Comma-separated list of sources to search in parallel |
| `--interactive` | `-i` | `false` | Interactively approve or choose track candidate before downloading |
| `--auto-install` | | `false` | Automatically install missing dependencies without prompting |
| `--no-cache` | | `false` | Disable local caching for search queries, metadata, and artwork |
| `--skip-verify` | | `false` | Skip DJ audio quality and spectrum inspection |
| `--skip-metadata` | | `false` | Skip metadata lookup and high-res cover art tagging |
| `--progress-target <uri>` | | `""` | Target URI for streaming NDJSON progress events |
| `--progress-socket <path>` | | `""` | Shorthand alias for `--progress-target` |
| `--verbose` | `-v` | `false` | Enable verbose logging |
| `--version` | | `false` | Print version information and exit |
| `--help` | `-h` | `false` | Print command help |

# Progress & IPC Telemetry

When executing `fetch-track` from an AI agent or parent supervisor, stream NDJSON events over UNIX sockets, TCP, or file descriptors:

```bash
fetch-track --progress-target "unix:///tmp/ft.sock" "Boris Brejcha - Space X"
```

See [PROGRESS.md](PROGRESS.md) for full protocol specifications and event schemas.

# License

MIT License (c) 2026 Alex Gorbatchev
