#!/usr/bin/env bash
set -euo pipefail

# Ensure binary is built
if [[ ! -f "bin/fetch-track" ]]; then
  echo "Building bin/fetch-track..."
  just build
fi

# Ensure .tmp directory exists within project root
TMP_DIR=".tmp"
mkdir -p "$TMP_DIR"

SOCKET_PATH="${TMP_DIR}/demo_progress_$$.sock"
rm -f "$SOCKET_PATH"

cleanup() {
  rm -f "$SOCKET_PATH"
}
trap cleanup EXIT INT TERM

QUERY="${1:-Boris Brejcha - Space X}"

echo "============================================================"
echo " Starting Out-of-Band Socket Progress Demo"
echo " Query:  \"$QUERY\""
echo " Socket: $SOCKET_PATH"
echo "============================================================"
echo ""

# Start background UNIX socket listener that processes live NDJSON events
if command -v python3 >/dev/null 2>&1; then
  python3 -u -c '
import socket
import sys
import json
import os

sock_path = sys.argv[1]
server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
server.bind(sock_path)
server.listen(1)

conn, _ = server.accept()
with conn:
    f = conn.makefile("r", encoding="utf-8")
    for line in f:
        line = line.strip()
        if not line:
            continue
        try:
            ev = json.loads(line)
            ev_type = ev.get("type", "event")
            phase = ev.get("phase", "")
            step = ev.get("step", 0)
            total = ev.get("total_steps", 0)
            msg = ev.get("message", "")
            
            step_str = f"[{step}/{total}]" if step and total else ""
            print(f"[SOCKET RECV] {ev_type:<18} {step_str:<7} phase={phase:<12} {msg}")
            
            if ev_type == "candidate_found" and "candidate" in ev:
                c = ev["candidate"]
                print(f"               --> Found: {c.get('title')} [{c.get('source')}]")
            elif ev_type == "candidate_selected" and "candidate" in ev:
                c = ev["candidate"]
                print(f"               ==> Selected: {c.get('title')}")
            elif ev_type == "complete" and "result" in ev:
                r = ev["result"]
                print(f"               *** Complete: {r.get('path')} (Bandwidth: {r.get('bandwidth_rating')})")
            elif ev_type == "error":
                print(f"               !!! Error: {ev.get('error')}")
        except Exception as e:
            print(f"[SOCKET RAW] {line}")
' "$SOCKET_PATH" &
  LISTENER_PID=$!
else
  # Fallback netcat listener
  nc -lU "$SOCKET_PATH" &
  LISTENER_PID=$!
fi

# Give the background listener a moment to bind the socket
sleep 0.2

echo "Running: bin/fetch-track --progress-socket \"$SOCKET_PATH\" \"$QUERY\""
echo "------------------------------------------------------------"

# Execute fetch-track passing the progress socket
./bin/fetch-track --progress-socket "$SOCKET_PATH" "$QUERY"

# Wait for socket listener to finish reading all events
wait $LISTENER_PID || true

echo ""
echo "============================================================"
echo " Demo completed successfully."
echo "============================================================"
