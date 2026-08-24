# fetch-track-cli justfile

set positional-arguments

# Default recipe: list available recipes
default:
    @just --list

# Build the fetch-track binary in bin/
build:
	mkdir -p bin
	go build -o bin/fetch-track ./cmd/fetch-track

# Install fetch-track binary to GOPATH bin directory
install:
    go install ./cmd/fetch-track

# Run single-track acquisition pipeline
run *ARGS:
    go run ./cmd/fetch-track "$@"

# Run in agent-facing token-conservative mode
run-ai *ARGS:
    AGENT=1 go run ./cmd/fetch-track "$@"

# Run stand-alone DJ audio quality verification
verify *ARGS:
    go run ./cmd/fetch-track verify "$@"

# Run all unit tests
test:
    go test -v ./...

# Run static code analysis
vet:
    go vet ./...

# Format Go code
fmt:
    go fmt ./...

# Clean up build binaries and temporary files
clean:
	rm -rf bin fetch-track coverage.out .tmp

# Run out-of-band socket progress demo
demo-progress *ARGS:
    ./scripts/demo-progress-socket.sh "$@"
