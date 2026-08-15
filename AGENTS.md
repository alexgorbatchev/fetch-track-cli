---
created_on: 2026-08-14 21:25
last_modified: 2026-08-14 21:35
status: current
---

# fetch-track-cli Agent Guidelines

Dedicated workspace for `fetch-track`, a Go CLI tool for searching, verifying, downloading, and managing high-fidelity single audio tracks for DJ collections. Focused on sourcing full-length Original and Extended Mixes from YouTube and other audio sources, avoiding short radio edits, and inspecting frequency spectrum bandwidth, gain staging, and embedded track metadata with high-res cover art.

## Environment & Target Setup
- **Track Location & Format:** Master tracks are stored in `./tracks/` in **`.m4a`** format for native album artwork rendering across audio players and file browsers.
- **Go Toolchain & Binary:** Go 1.23+ with Cobra CLI framework (`github.com/spf13/cobra`).

## Primary Single-Command Track Acquisition
To add any new track or YouTube URL in 1 step (runs DJ Full Mix candidate ranker, downloads native M4A, checks quality/bandwidth, resolves 1400x1400 cover art via API fallback, and embeds MP4 `covr` artwork):
```bash
go run ./cmd/fetch-track "Boris Brejcha - Space X"
# or using just:
just run "Boris Brejcha - Space X"
```

## Common Commands

### Building & Running
- **Build Local Binary:**
  ```bash
  just build
  ```

- **Run Single Track Pipeline:**
  ```bash
  just run "<youtube_url_or_search_query>"
  ```

- **Inspect Local Audio File or URL:**
  ```bash
  just verify "tracks/Boris Brejcha - Space X.m4a"
  ```

- **Run Tests & Linter Checks:**
  ```bash
  just test
  just vet
  ```

## Audio Conventions & Rules
- **Target Audience & Disclaimer:** This tool is strictly for amateur and bedroom DJs practicing at home or playing informal sets, who are not seeking professional careers. Professional DJs must source music from legitimate commercial sources (Beatport, Bandcamp, Juno Download, iTunes, or authorized record pools).
- **Storage Directory & M4A Format:** All acquired tracks MUST be placed in `./tracks/` as **`.m4a`** files. Output filenames MUST NOT contain YouTube IDs or brackets `[...]` (e.g. `Artist - Title.m4a`).
- **Artwork Compatibility:** `.m4a` (MP4 container with `covr` atom) is required because it guarantees native cover art rendering across media players and file managers.
- **API Fallback Chain for Metadata & Artwork:**
  1. **iTunes Search API (Primary):** Clean track name, artist, album name, release year, and 1400x1400 high-res cover art.
  2. **MusicBrainz API & Cover Art Archive (Fallback 1):** Open-source music database and release cover archive.
  3. **Local Filename / YouTube Raw Metadata (Fallback 2):** Fallback when API search yields no match.
- **Original Mixes First:** Always select Original Mixes, Extended Mixes, Club Mixes, or Dub Mixes with mixable intro/outro beats. Reject short radio edits (< 3.5 minutes) that lack beatmatchable sections.
- **Audio Bandwidth Verification:** Verify frequency response with `verifier` Go module. Ensure audio bandwidth reaches >= 16 kHz to avoid low-bitrate or transcoded rips.
- **Gain Staging:** Check peak levels and RMS loudness to set appropriate channel trim offsets before playing tracks.
- **Go Conventions:** Standard library first, strict error wrapping (`fmt.Errorf("...: %w", err)`), table-driven unit tests, no compiled binaries committed to git (`.gitignore`).

## Boundaries
- **Always:** Automatically record all new user instructions in `AGENTS.md` immediately upon receipt.
- **Always:** Save all downloaded audio tracks into the `./tracks/` directory as **`.m4a`** files with clean filenames (no `[...]` video IDs) and embedded 1400x1400 artwork.
- **Always:** Run `just test` and `just vet` before committing changes.
- **Ask first:** Deleting master `.m4a` audio files or modifying core spectral analysis algorithms.
- **Never:** Include YouTube video IDs or brackets (`[...]`) in track filenames.
- **Never:** Store raw `.opus` files without converting or acquiring as `.m4a`.
- **Never:** Commit compiled Go binaries (e.g. `fetch-track`) to git.
