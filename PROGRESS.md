# Progress & IPC Telemetry Protocol

`fetch-track` supports streaming real-time, structured progress and telemetry events out-of-band to parent processes (such as other CLI tools, AI agent harnesses, orchestrators, background daemons, or GUI desktop apps).

By separating telemetry from standard output streams, parent processes avoid parsing unstructured terminal text or ANSI escape sequences and can render native progress bars, inspect candidate discoveries live, or maintain alive/watchdog heartbeats.

---

## Configuration & Usage

Progress streaming is configured via CLI flags or an environment variable.

### CLI Flags

| Flag | Format / Examples | Description |
| :--- | :--- | :--- |
| `--progress-target` | `unix:///tmp/p.sock`<br>`tcp://127.0.0.1:9099`<br>`fd://3`<br>`stdout`, `stderr` | Target URI or address where NDJSON progress events are streamed. |
| `--progress-socket` | `/tmp/p.sock` or `unix:///tmp/p.sock` | Shorthand alias for `--progress-target`. |

### Environment Variable

If no flag is provided, `fetch-track` inspects:

```bash
export FETCH_TRACK_PROGRESS_TARGET="unix:///tmp/fetch-track.sock"
```

### Live Demo Recipe

You can run the included out-of-band socket demo recipe directly:

```bash
just demo-progress "Boris Brejcha - Space X"
```

---

## Supported Target Protocols

### 1. UNIX Domain Sockets (`unix://`)
Best for local parent CLIs, daemon supervisors, and agent harnesses on Linux and macOS.

```bash
# Explicit scheme
fetch-track --progress-target "unix:///tmp/fetch-track-progress.sock" "Boris Brejcha - Space X"

# Shorthand path (auto-detected as unix socket)
fetch-track --progress-socket "/tmp/fetch-track-progress.sock" "Boris Brejcha - Space X"
```

### 2. TCP Sockets (`tcp://`)
Best for cross-platform IPC (including Windows) or distributed orchestrators.

```bash
fetch-track --progress-target "tcp://127.0.0.1:9099" "Boris Brejcha - Space X"
```

### 3. Inherited File Descriptors (`fd://`)
Best for POSIX shell subshells and parent processes spawning child processes with extra pipes.

```bash
fetch-track --progress-target "fd://3" "Boris Brejcha - Space X" 3> progress.log
```

### 4. Standard Streams (`stdout`, `stderr`)
Best for piping NDJSON events directly into tools like `jq` or logging pipelines.

```bash
fetch-track --progress-target stderr "Boris Brejcha - Space X" 2> progress.ndjson
```

---

## Event Stream Specification

Events are streamed as **Newline-Delimited JSON (NDJSON)**. Every line is a single, valid JSON object ending in `\n`.

### Event Object Schema

```typescript
interface ProgressEvent {
  timestamp: string;          // ISO 8601 UTC timestamp (e.g. "2026-08-23T14:35:00Z")
  type: EventType;           // "phase_start" | "candidate_found" | "candidate_selected" | "progress" | "complete" | "error"
  phase?: string;            // Current pipeline phase ("dependencies" | "search" | "download" | "verify" | "metadata" | "complete")
  step?: number;             // Current 1-indexed step number (1 to 5)
  total_steps?: number;      // Total steps in execution (default: 5)
  message?: string;          // Human-readable status message
  percent?: number;          // Completion percentage (0.0 to 100.0)
  candidate?: CandidateInfo; // Populated for candidate events
  result?: ResultInfo;       // Populated on "complete" event
  error?: string;            // Error message if type is "error"
}

interface CandidateInfo {
  id?: string;               // Candidate identifier
  title: string;             // Track candidate title
  artist?: string;           // Detected artist name
  source: string;            // Platform ("youtube" | "soundcloud" | "bandcamp" | "direct_url")
  duration_seconds?: number; // Track duration in seconds
  score?: number;            // Ranking score based on DJ mix heuristics
  url?: string;              // Source URL
}

interface ResultInfo {
  path: string;              // Output file path
  title?: string;            // Matched track title
  artist?: string;           // Matched artist name
  album?: string;            // Matched album / EP
  release_year?: string;     // Release year
  duration_seconds?: number; // Verified audio duration in seconds
  bandwidth_hz?: number;     // Estimated high-frequency cutoff (Hz)
  bandwidth_rating?: string; // Bandwidth quality classification
  suggested_gain_db?: number;// Mixer volume Gain Offset in dB
  status?: string;           // Verification status summary
}
```

---

## Event Types & Lifecycles

### 1. `phase_start`
Emitted at the beginning of each major acquisition step.

```json
{"timestamp":"2026-08-23T14:35:00Z","type":"phase_start","phase":"search","step":2,"total_steps":5,"message":"searching sources: youtube, soundcloud"}
```

### 2. `candidate_found`
Emitted for each potential track match discovered across search sources.

```json
{"timestamp":"2026-08-23T14:35:01Z","type":"candidate_found","phase":"search","candidate":{"id":"sc_12345","title":"Boris Brejcha - Space X (Extended Mix)","source":"soundcloud","duration_seconds":503,"score":150,"url":"https://soundcloud.com/boris-brejcha/space-x-extended-mix"}}
```

### 3. `candidate_selected`
Emitted when candidate ranking and evaluation selects the best track to download.

```json
{"timestamp":"2026-08-23T14:35:02Z","type":"candidate_selected","phase":"search","candidate":{"id":"sc_12345","title":"Boris Brejcha - Space X (Extended Mix)","source":"soundcloud","duration_seconds":503,"score":150,"url":"https://soundcloud.com/boris-brejcha/space-x-extended-mix"}}
```

### 4. `progress`
Emitted during long-running tasks with optional percent completion.

```json
{"timestamp":"2026-08-23T14:35:05Z","type":"progress","phase":"download","step":3,"total_steps":5,"percent":45.0,"message":"downloading audio stream"}
```

### 5. `complete`
Emitted when track downloading, spectral verification, and metadata tagging succeed.

```json
{
  "timestamp": "2026-08-23T14:35:14Z",
  "type": "complete",
  "phase": "complete",
  "step": 5,
  "total_steps": 5,
  "message": "track acquisition complete",
  "result": {
    "path": "tracks/Boris Brejcha - Space X.m4a",
    "title": "Space X",
    "artist": "Boris Brejcha",
    "album": "Space X - Single",
    "release_year": "2024",
    "duration_seconds": 503,
    "bandwidth_hz": 20000,
    "bandwidth_rating": "High Fidelity (>=18.5 kHz)",
    "suggested_gain_db": -2.6,
    "status": "STATUS: High fidelity audio suitable for mixing."
  }
}
```

### 6. `error`
Emitted if an unrecoverable failure occurs.

```json
{"timestamp":"2026-08-23T14:35:04Z","type":"error","phase":"download","error":"downloading audio stream for https://...: exit status 1"}
```

---

## Integration Examples

### Go Parent Process (UNIX Domain Socket)

```go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
)

type ProgressEvent struct {
	Type    string `json:"type"`
	Phase   string `json:"phase"`
	Message string `json:"message"`
	Percent float64 `json:"percent"`
}

func main() {
	sockPath := filepath.Join(os.TempDir(), "ft_progress.sock")
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	// Spawn fetch-track child process
	cmd := exec.Command("fetch-track", "--progress-socket", sockPath, "Boris Brejcha - Space X")
	if err := cmd.Start(); err != nil {
		panic(err)
	}

	conn, err := listener.Accept()
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var ev ProgressEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err == nil {
			fmt.Printf("[%s] %s: %s\n", ev.Phase, ev.Type, ev.Message)
		}
	}

	_ = cmd.Wait()
}
```

### Node.js / TypeScript Parent Process

```typescript
import * as net from 'node:net';
import * as readline from 'node:readline';
import { spawn } from 'node:child_process';
import * as path from 'node:path';
import * as os from 'node:os';

const sockPath = path.join(os.tmpdir(), `ft_${Date.now()}.sock`);

const server = net.createServer((socket) => {
  const rl = readline.createInterface({ input: socket });
  rl.on('line', (line) => {
    try {
      const event = JSON.parse(line);
      console.log(`[${event.phase || 'info'}] ${event.type}: ${event.message || ''}`);
    } catch {
      // Ignore unparseable lines
    }
  });
});

server.listen(sockPath, () => {
  const child = spawn('fetch-track', ['--progress-socket', sockPath, 'Boris Brejcha - Space X']);
  child.on('exit', () => {
    server.close();
  });
});
```
