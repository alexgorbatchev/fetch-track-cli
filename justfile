# fetch-track-cli justfile

# Default recipe: list available recipes
default:
    @just --list

# Build the fetch-track binary in the root directory
build:
    go build -o fetch-track ./cmd/fetch-track

# Install fetch-track binary to GOPATH bin directory
install:
    go install ./cmd/fetch-track

# Run single-track acquisition pipeline
run +ARGS:
    go run ./cmd/fetch-track {{ARGS}}

# Run stand-alone DJ audio quality verification
verify +ARGS:
    go run ./cmd/fetch-track verify {{ARGS}}

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
    rm -f fetch-track coverage.out
    rm -rf .tmp
