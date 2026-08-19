# fetch-track

`fetch-track` is a simple command-line tool with AI agent support for bedroom and amateur DJs to find, check, and download high-quality single tracks for their DJ sets.

> **⚠️ Intended Audience & Legal Disclaimer:**  
> `fetch-track` is strictly intended for **amateur and bedroom DJs** practicing at home or playing non-commercial sets who are not seeking to become professional DJs. **Working professional DJs and commercial performers must source music from legitimate commercial sources** (such as Beatport, Bandcamp purchases, Juno Download, iTunes, or authorized record pools).

## What It Does

- **Finds Full DJ Mixes**: Searches YouTube and SoundCloud in parallel for extended and original DJ mixes with mixable intro/outro sections.
- **Picks the Right Track**: Matches artist and title while ignoring short radio edits, 30-second snippets, or multi-hour mix compilations.
- **Preserves Original Audio Quality**: Downloads high-bitrate audio streams directly without re-encoding or degrading sound quality.
- **Checks Audio & Volume**: Analyzes frequency range to catch low-quality rips and calculates the recommended volume Gain Offset for your mixer.
- **Adds Cover Art & Metadata**: Fetches official track details, release years, and high-res 1400x1400 album art so your music looks great on DJ decks and laptops.

## How It Works

1. **Search**: Searches YouTube and SoundCloud simultaneously for your requested artist and title.
2. **Select**: Ranks candidates to pick the full-length DJ mix.
3. **Download**: Downloads the best native audio stream directly.
4. **Inspect**: Checks audio frequency response and calculates mixer Gain Offset.
5. **Tag**: Adds high-res artwork and metadata tags before saving.

## Prerequisites

`fetch-track` requires the following external binary dependencies installed on your system:

- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp#installation) (version 2024.08.01 or newer) — for audio stream downloading
- [`ffmpeg`](https://ffmpeg.org/download.html) (version 4.4 or newer, including `ffprobe`) — for audio quality analysis and cover art tagging

## Installation

Go to the [Latest Release Page](https://github.com/alexgorbatchev/fetch-track-cli/releases/latest) and download the pre-compiled archive for your operating system.

## Quick Start

### 1. Download a Track

```bash
fetch-track "Boris Brejcha - Space X"
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

DONE: Boris Brejcha - Space X.m4a
```

### 2. Download from a Direct URL

If you have a link to a track:

```bash
fetch-track "https://www.youtube.com/watch?v=..."
```

### 3. Restrict Search Sources

Search YouTube only or SoundCloud only:

```bash
fetch-track -s "youtube" "Boris Brejcha - Space X"
```

### 4. Interactive Track Candidate Approval

Approve or choose a different search result candidate interactively before downloading:

```bash
fetch-track -i "Boris Brejcha - Space X"
```

### 5. Check an Existing Local Track File

Inspect any audio file without downloading:

```bash
fetch-track verify "Boris Brejcha - Space X.m4a"
```

### 6. Verify Installed Dependencies

Verify that `yt-dlp`, `ffmpeg`, and `ffprobe` are installed and meet minimum version requirements:

```bash
fetch-track dependencies
```

Sample Output:
```
yt-dlp: 2026.07.04 (min 2024.08.01) [OK]
ffmpeg: 8.1.2 (min 4.4) [OK]
ffprobe: 8.1.2 (min 4.4) [OK]

All dependencies met.
```

## Options & Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--out-dir` | `-o` | `.` | Folder where tracks are saved |
| `--sources` | `-s` | `youtube,soundcloud` | Comma-separated list of [supported search sources](#supported-search-sources) |
| `--interactive` | `-i` | `false` | Interactively approve or choose track candidate before downloading |
| `--no-cache` | | `false` | Disable local caching for search queries, metadata, and artwork |
| `--skip-verify` | | `false` | Skip audio frequency and loudness check |
| `--skip-metadata` | | `false` | Skip fetching album artwork and track details |
| `--verbose` | `-v` | `false` | Show extra detailed progress logs |

## Supported Search Sources

`fetch-track` supports searching the following platforms via the `-s` / `--sources` flag:

- `youtube` — [YouTube](https://www.youtube.com) (default)
- `soundcloud` — [SoundCloud](https://soundcloud.com) (default)
- `bandcamp` — [Bandcamp](https://bandcamp.com)

## License

This project is licensed under the [MIT License](LICENSE).
