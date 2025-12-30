# Makefile for gitlab-autoscaler
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

# Archive and packaging settings
ARCHIVE_PREFIX := $(OUTPUT_NAME)-$(VERSION)
CHECKSUM_FILE := SHA256SUMS

# Default candidate paths (system and local)
systemConfigPath  := /etc/gitlab-autoscaler/config.yml
systemPidPath     := /var/run/gitlab-autoscaler.pid
localConfigPath   := ./config.yml
localPidPath      := ./gitlab-autoscaler.pid

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

.PHONY: all clean docker-build tests check-params mock build-all package-tar package-zip package-all checksum sign

all: build

check-params:
	@if [ -z "$(OS)" ] || [ -z "$(ARCH)" ]; then \
	echo "Error: OS and ARCH must be specified."; \
	echo "Usage: make OS=<os> ARCH=<arch>"; \
	echo "Supported OS: darwin (macOS), linux"; \
	echo "Supported Arch: amd64, arm, arm64"; \
	exit 1; \
	fi

# Build single target (uses GOOS/GOARCH/GOARM set above)
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
	rm -f $(OUTPUT_NAME) $(OUTPUT_NAME)-* $(ARCHIVE_PREFIX)-*.tar.gz $(ARCHIVE_PREFIX)-*.zip $(CHECKSUM_FILE) $(CHECKSUM_FILE).asc

# Run the full Go test suite with race detector enabled.
tests:
	go test -race ./... -v -count=1

# Mock generation for AWS SDK
mock:
	@echo "Generating mocks..."
	@mkdir -p ./providers/aws/mocks
	@/bin/bash -c "mockery --config mockery.yml"

# Build all target binaries for supported platforms and rename outputs
build-all:
	@echo "Building all targets..."
	$(MAKE) OS=darwin ARCH=arm64 build && mv $(OUTPUT_NAME) $(OUTPUT_NAME)-darwin-arm64
	$(MAKE) OS=darwin ARCH=amd64 build && mv $(OUTPUT_NAME) $(OUTPUT_NAME)-darwin-amd64
	$(MAKE) OS=linux ARCH=amd64 build && mv $(OUTPUT_NAME) $(OUTPUT_NAME)-linux-amd64
	$(MAKE) OS=linux ARCH=arm64 build && mv $(OUTPUT_NAME) $(OUTPUT_NAME)-linux-arm64
	@echo "All builds finished."

# Package a single built binary into tar.gz (preserves exec bit)
# Usage: make OS=linux ARCH=amd64 package-tar
package-tar: check-params
	@echo "Packaging $(OUTPUT_NAME) into $(ARCHIVE_PREFIX)-$(OS)-$(ARCH).tar.gz"
	tar -czf $(ARCHIVE_PREFIX)-$(OS)-$(ARCH).tar.gz $(OUTPUT_NAME)

# Package a single built binary into zip (Windows friendly)
# Usage: make OS=linux ARCH=amd64 package-zip
package-zip: check-params
	@echo "Packaging $(OUTPUT_NAME) into $(ARCHIVE_PREFIX)-$(OS)-$(ARCH).zip"
	zip -j $(ARCHIVE_PREFIX)-$(OS)-$(ARCH).zip $(OUTPUT_NAME)

# Convenience: build then create both tar.gz and zip for the given target
package-all: check-params
	$(MAKE) build
	$(MAKE) package-tar
	$(MAKE) package-zip

# Package all artifacts produced by build-all
# This target assumes build-all has been run (or will run as dependency).
# It will create per-artifact tar.gz and zip archives and then generate checksums.
.PHONY: package-all-artifacts
package-all-artifacts: build-all
	@echo "Packaging all built artifacts..."
	@for bin in $(OUTPUT_NAME)-*; do \
	  if [ -f "$$bin" ]; then \
	base=$$(basename $$bin); \
	arch=$${base#$(OUTPUT_NAME)-}; \
	tarname="$(ARCHIVE_PREFIX)-$$arch.tar.gz"; \
	zipname="$(ARCHIVE_PREFIX)-$$arch.zip"; \
	echo " - creating $$tarname"; \
	tar -czf "$$tarname" "$$bin"; \
	echo " - creating $$zipname"; \
	zip -j "$$zipname" "$$bin"; \
	  fi; \
	done
	@echo "Generating SHA256 sums into $(CHECKSUM_FILE)"
	@sha256sum $(ARCHIVE_PREFIX)-*.tar.gz $(ARCHIVE_PREFIX)-*.zip > $(CHECKSUM_FILE) || true
	@echo "Packaging complete."

# Generate SHA256 checksums for produced archives (standalone)
checksum:
	@echo "Generating SHA256 sums into $(CHECKSUM_FILE)"
	@sha256sum $(ARCHIVE_PREFIX)-*.tar.gz $(ARCHIVE_PREFIX)-*.zip > $(CHECKSUM_FILE) || true

# Sign checksum file (requires gpg available and key configured in CI)
sign: checksum
	@echo "Signing $(CHECKSUM_FILE) -> $(CHECKSUM_FILE).asc"
	@gpg --armor --output $(CHECKSUM_FILE).asc --detach-sign $(CHECKSUM_FILE) || true
