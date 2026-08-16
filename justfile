# fetch-track-cli justfile

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
    go run ./cmd/fetch-track {{ARGS}}

# Run stand-alone DJ audio quality verification
verify *ARGS:
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
	rm -rf bin fetch-track coverage.out .tmp
