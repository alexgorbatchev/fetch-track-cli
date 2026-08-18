# fetch-track

`fetch-track` is a simple command-line tool for bedroom and amateur DJs to find, check, and download high-quality single tracks for their DJ sets.

> **⚠️ Intended Audience & Legal Disclaimer:**  
> `fetch-track` is strictly intended for **amateur and bedroom DJs** practicing at home or playing non-commercial sets who are not seeking to become professional DJs. **Working professional DJs and commercial performers must source music from legitimate commercial sources** (such as Beatport, Bandcamp purchases, Juno Download, iTunes, or authorized record pools).

---

## What It Does

- **Finds Full DJ Mixes**: Searches YouTube and SoundCloud in parallel for extended and original DJ mixes with mixable intro/outro sections.
- **Picks the Right Track**: Matches artist and title while ignoring short radio edits, 30-second snippets, or multi-hour mix compilations.
- **Preserves Original Audio Quality**: Downloads high-bitrate audio streams directly without re-encoding or degrading sound quality.
- **Checks Audio & Volume**: Analyzes frequency range to catch low-quality rips and calculates the recommended volume Gain Offset for your mixer.
- **Adds Cover Art & Metadata**: Fetches official track details, release years, and high-res 1400x1400 album art so your music looks great on DJ decks and laptops.

---

## How It Works

1. **Search**: Searches YouTube and SoundCloud simultaneously for your requested artist and title.
2. **Select**: Ranks candidates to pick the full-length DJ mix.
3. **Download**: Downloads the best native audio stream directly.
4. **Inspect**: Checks audio frequency response and calculates mixer Gain Offset.
5. **Tag**: Adds high-res artwork and metadata tags before saving.

---

## Compatibility

Acquired tracks are saved in standard `.m4a` format with embedded 1400x1400 artwork and tags. They work natively across:

- **Hardware Decks**: Denon DJ / Engine DJ OS standalone players
- **DJ Software**: Pioneer Rekordbox, Serato DJ, Traktor, VirtualDJ
- **File Browsers**: macOS Finder QuickLook previews, Windows File Explorer

---

## Prerequisites

`fetch-track` requires `yt-dlp` and `ffmpeg` installed on your computer:

```bash
yt-dlp --version
ffmpeg -version
```

---

## Installation

Download the latest pre-compiled binary for your computer using GitHub CLI or direct download:

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

---

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

### 4. Check an Existing Local Track File

Inspect any audio file without downloading:

```bash
fetch-track verify "Boris Brejcha - Space X.m4a"
```

---

## Options & Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--out-dir` | `-o` | `.` | Folder where tracks are saved |
| `--sources` | `-s` | `youtube,soundcloud` | Sources to search (`youtube`, `soundcloud`) |
| `--skip-verify` | | `false` | Skip audio frequency and loudness check |
| `--skip-metadata` | | `false` | Skip fetching album artwork and track details |
| `--verbose` | `-v` | `false` | Show extra detailed progress logs |

---

## License

This project is licensed under the [MIT License](LICENSE).
