A fast, lightweight CLI tool with AI agent support for bedroom and amateur DJs to discover, inspect, download, and manage high-fidelity single audio tracks in native formats.

# What It Does

- **Direct Links & Search Queries:** Accepts direct track URLs (YouTube, SoundCloud, Bandcamp, Mixcloud) or `"Artist - Title"` search queries.
- **Parallel Extended Mix Discovery:** Searches YouTube, SoundCloud, and Bandcamp in parallel for full extended DJ mixes with mixable intro/outro sections.
- **Remix & Duration Scoring:** Matches artist, title, and remix/version intent while filtering out short radio edits, snippets, or multi-hour mix compilations.
- **Acoustic Fingerprint Confirmation:** Confirms candidate authenticity before finalizing using acoustic audio fingerprinting (Shazam / AcoustID) to eliminate false positives and fake uploads.
- **Zero-Transcode Extraction:** Downloads native high-bitrate audio streams directly without lossy re-encoding using auto-detected JavaScript runtimes.
- **Spectral & Loudness Analysis:** Analyzes spectral frequency bandwidth (Goertzel algorithm) and computes mixer volume Gain Offset.
- **Canonical Metadata & Square Artwork:** Identifies tracks via acoustic audio fingerprinting (AcoustID and Apple Shazam) with API fallbacks (iTunes and MusicBrainz) to embed canonical metadata and 1400x1400 album art.

# How It Works

- Evaluates direct streaming URLs (YouTube, SoundCloud, Bandcamp, Mixcloud) or queries configured sources in parallel.
- Discovers and prioritizes full-length Extended, Original, Club, or Dub DJ mixes over short radio edits using remix intent and duration-curve heuristics.
- Downloads the highest fidelity native audio stream without lossy re-encoding.
- Confirms candidate audio authenticity via acoustic fingerprinting against target track metadata before finalizing.
- Inspects audio frequency response and calculates mixer volume Gain Offset.
- Enriches the track with high-resolution 1400x1400 artwork and canonical tags before saving.

# How it Really Works

- Probes direct streaming links and runs concurrent `yt-dlp` JSON scrapers with multi-angle query expansion, tokenized artist matching, remix intent scoring, and channel authority bonuses.
- Auto-detects available JavaScript runtimes (`deno`, `node`, `bun`, `quickjs`) on `$PATH` for seamless `yt-dlp` signature challenge solving.
- Performs pre-tag acoustic fingerprint validation (`--confirm-fingerprint`) to automatically reject and retry mismatched search candidates.
- Extracts PCM audio samples via `ffmpeg` and executes Goertzel frequency bin analysis to detect audio cutoff thresholds and RMS loudness.
- Generates Chromaprint acoustic fingerprints and landmark constellations to query AcoustID and Shazam recognition services in pure Go.
- Normalizes and center-crops downloaded album artwork to 1:1 square format (1400x1400) and embeds ID3/MP4 metadata using stream-copy mode (`-c:a copy`).
- Streams real-time NDJSON progress events over UNIX domain sockets, TCP, or file descriptors when executed by AI agents or supervisor orchestrators. Every `fetch-track` event includes overall milestone completion as `percent` (0–100); see [the progress protocol](PROGRESS.md) for calculation and delegated `tag-track` events.

# Installation

Download the prebuilt binary for your platform from the [latest release](https://github.com/alexgorbatchev/fetch-track-cli/releases/latest), replacing `X.X.X` with the version shown on that page:

```bash
# macOS (Apple Silicon)
curl -fsSL https://github.com/alexgorbatchev/fetch-track-cli/releases/latest/download/fetch-track_X.X.X_darwin_arm64.tar.gz | tar -xz -C ~/.local/bin
```

# Setup

- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp#installation) (version 2024.08.01 or newer) - Required for searching sources and extracting native audio streams. Run `fetch-track dependencies install yt-dlp` or pass `--auto-install` to install into `$XDG_DATA_HOME/fetch-track/bin`.
- [`ffmpeg` & `ffprobe`](https://ffmpeg.org/download.html) (version 4.4 or newer) - Required for PCM spectral verification and stream-copy metadata tagging. Run `fetch-track dependencies install ffmpeg ffprobe` or pass `--auto-install`.
- [`tag-track`](https://github.com/alexgorbatchev/tag-track-cli/releases/latest) (version 1.0.0 or newer) - Required for acoustic fingerprint verification and 1400x1400 cover art enrichment. Run `fetch-track dependencies install tag-track` or pass `--auto-install`.

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

# Inspect local file or URL quality
fetch-track track verify "Boris Brejcha - Space X.m4a"
fetch-track track verify "https://soundcloud.com/boris-brejcha/space-x-extended-mix"

# Manage external dependencies
fetch-track dependencies
fetch-track deps install ffmpeg
fetch-track deps update yt-dlp

# Pipeline composition with tag-track
fetch-track "Boris Brejcha - Space X" --no-metadata -o .tmp/ | tag-track -o tracks/

# Print AI agent skill instructions
fetch-track skill

# Upgrade CLI binary in-place
fetch-track upgrade
```

Sample Output:
```
Searching youtube...
Searching soundcloud...
Candidates:
  - "Boris Brejcha - Space X (Extended Mix)" [soundcloud 8:23]
Got 4 results from soundcloud
Got 6 results from youtube
Selected: "Boris Brejcha - Space X (Extended Mix)" [soundcloud 8:23] score=270

Downloading audio stream & artwork
  - Saved: Boris Brejcha - Space X (Extended Mix).m4a

Running audio quality & spectrum inspection
  - Duration: 8:23 (Original / Extended DJ Mix)
  - Bandwidth: High Fidelity (>=18.5 kHz) (20 kHz)
  - Peak / RMS: -0.10 dBFS / -9.40 dBFS
  - Gain Offset: -2.6 dB
  - Status: High fidelity audio suitable for mixing.

Enriching metadata & cover art via tag-track
  - Matched: "Boris Brejcha - Space X" (Level One, 2024)
  - Source: Shazam

DONE: ./Boris Brejcha - Space X.m4a
```

# Options & Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--confirm-fingerprint` | | `true` | Verify downloaded audio via acoustic fingerprinting against target track |
| `--js-runtime <engine>` | | `auto` | JavaScript runtime for `yt-dlp` (`auto`, `deno`, `node`, `bun`, `quickjs`, `none`) |
| `--json` | | `false` | Output machine-readable JSON execution result to stdout |
| `--list-candidates` | | `false` | Return ranked candidates as JSON without downloading; implies `--json` |
| `--candidate-url <url>` | | `""` | Download only the exact URL from a previous candidate result |
| `--min-bandwidth-khz <khz>` | | `16.0` | Minimum required audio frequency bandwidth in kHz (e.g. `18.5`) |
| `--out-dir <path>` | `-o` | `.` | Output directory for downloaded tracks |
| `--sources <list>` | `-s` | `youtube,soundcloud` | Comma-separated list of sources to search in parallel |
| `--interactive` | `-i` | `false` | Interactively approve or choose track candidate before downloading |
| `--auto-install` | | `false` | Automatically install missing dependencies without prompting |
| `--no-cache` | | `false` | Disable local caching for search queries, metadata, and artwork |
| `--skip-verify` | | `false` | Skip DJ audio quality and spectrum inspection |
| `--skip-metadata` | | `false` | Skip metadata lookup and high-res cover art tagging |
| `--no-metadata` | | `false` | Alias for `--skip-metadata` |
| `--progress-target <uri>` | | `""` | Target URI for streaming NDJSON progress events |
| `--progress-socket <path>` | | `""` | Shorthand alias for `--progress-target` |
| `--job-id <id>` | | generated | Job ID attached to every fetch-track progress event; sequence starts at 1 per run |
| `--verbose` | `-v` | `false` | Enable verbose logging |
| `--version` | | `false` | Print version information and exit |
| `--help` | `-h` | `false` | Print command help |

# Machine Output & Candidate Selection

Use `--json` for one JSON result on stdout, with diagnostics on stderr. Acquisition results include `target`, `status`, and available `output_file`, `selected_candidate`, `verification`, `metadata`, or `error` fields. Failed operations retain their structured result and exit unsuccessfully.

`fetch-track --json verify <path|url>` returns the same envelope with `verification` on success. Acquisition and verification report missing targets and unavailable dependencies as `status: "error"`; acquisition also reports progress-target initialization failures this way. Verification errors include the requested `target` and an actionable `error`.

Use `--list-candidates` with the intended search query to inspect ranked candidates without downloading. Its result has `status: "candidates"`, `candidates`, and `candidate_count`. Continue with the same query and `--candidate-url` set to the selected candidate's `webpage_url`; acquisition restricts its candidate pool to that exact URL and fails if the candidate is no longer available.

```bash
fetch-track --list-candidates "Boris Brejcha - Space X"
fetch-track --json --candidate-url "https://soundcloud.com/boris-brejcha/space-x-extended-mix" "Boris Brejcha - Space X"
fetch-track --json dependencies
fetch-track --json deps install ffmpeg
fetch-track --json upgrade
```

Dependency commands return `status`, `dependencies`, `changed`, and optional `error`. Self-upgrade returns `status`, `version`, and optional `error`; `status: "current"` means the installed version is already current. These commands use the same installation and upgrade behavior as their human-readable forms.

# Progress & IPC Telemetry

When executing `fetch-track` from an AI agent or parent supervisor, stream NDJSON events over UNIX sockets, TCP, or file descriptors:

```bash
fetch-track --progress-target "unix:///tmp/ft.sock" "Boris Brejcha - Space X"
```

See [PROGRESS.md](PROGRESS.md) for full protocol specifications and event schemas.

# License

[MIT License](LICENSE) (c) 2026 Alex Gorbatchev
