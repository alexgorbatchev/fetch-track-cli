set dotenv-load := false
set positional-arguments

# Default recipe: list available recipes
default:
    @just --list

# Run CLI in human mode (default)
run *args:
    go run ./cmd/fetch-track "$@"

# Run CLI in agent-facing token-conservative mode
run-ai *args:
    AGENT=1 go run ./cmd/fetch-track "$@"

# Run stand-alone DJ audio quality verification
verify *args:
    go run ./cmd/fetch-track verify "$@"

# Run all unit tests
test:
    go test -v ./...

# Lint and static check
lint:
    go vet ./...

# Static code analysis (alias for lint)
vet:
    go vet ./...

# Run static checks and test suite in sequence
check:
    go vet ./...
    go test ./...

# Format Go code
fmt:
    go fmt ./...

# Build the fetch-track binary in bin/
build:
    mkdir -p bin
    go build -o bin/fetch-track ./cmd/fetch-track

# Install fetch-track binary to GOPATH bin directory
install:
    go install ./cmd/fetch-track

# Clean up build binaries and temporary files
clean:
    rm -rf bin fetch-track coverage.out .tmp

# Run out-of-band socket progress demo
demo-progress *args:
    ./scripts/demo-progress-socket.sh "$@"
