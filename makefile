# Notes:
#   - All recipe lines must be indented with a literal TAB.
#   - To fix accidental spaces in indentation: sed -i -E 's/^( {4})+/\t/' Makefile

OUTPUT_NAME = gitlab-autoscaler
DOCKER_IMAGE = golang:1.5
GOARM =
GOOS =
GOARCH =

# Get version and commit hash from Git
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "unknown")
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Set GOOS, GOARCH, GOARM based on OS and ARCH parameters
ifeq ($(OS),darwin)
	ifeq ($(ARCH),amd64)
	GOOS = darwin
	GOARCH = amd64
	else ifeq ($(ARCH),arm64)
	GOOS = darwin
	GOARCH = arm64
	else ifneq ($(ARCH),)
	$(error Unsupported architecture for macOS: $(ARCH). Supported: amd64, arm64)
	endif
else ifeq ($(OS),linux)
	ifeq ($(ARCH),amd64)
	GOOS = linux
	GOARCH = amd64
	else ifeq ($(ARCH),arm)
	GOOS = linux
	GOARCH = arm
	GOARM = 7
	else ifeq ($(ARCH),arm64)
	GOOS = linux
	GOARCH = arm64
	else ifneq ($(ARCH),)
	$(error Unsupported architecture for Linux: $(ARCH). Supported: amd64, arm, arm64)
	endif
else ifneq ($(OS),)
	$(error Unsupported operating system: $(OS). Supported: darwin, linux)
endif

.PHONY: all clean docker-build tests check-params

all: check-params build

check-params:
	@if [ -z "$(OS)" ] || [ -z "$(ARCH)" ]; then \
	    echo "Error: OS and ARCH must be specified."; \
	    echo "Usage: make OS=<os> ARCH=<arch>"; \
	    echo "Supported OS: darwin (macOS), linux"; \
	    echo "Supported Arch: amd64, arm, arm64"; \
	    exit 1; \
	fi

build: check-params
	@echo "Building for $(OS)/$(ARCH) (GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM))..."
	GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) go build \
	    -ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH)" \
	    -o "$(OUTPUT_NAME)" \
	    cmd/gitlab-autoscaler/main.go

# Build in Docker
docker-build: check-params
	@echo "Building $(OUTPUT_NAME) in Docker for $(OS)/$(ARCH) (GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM))..."
	docker run --rm \
	    -v "$(PWD):/app" \
	    -w /app \
	    $(DOCKER_IMAGE) \
	    /bin/bash -c "GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) go build \
	        -ldflags '-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH)' \
	        -o $(OUTPUT_NAME) cmd/gitlab-autoscaler/main.go"

clean:
	rm -f $(OUTPUT_NAME)

# Run the full Go test suite with race detector enabled.
tests:
	go test -race ./... -v -count=1