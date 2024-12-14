OUTPUT_NAME = gitlab-autoscaler
DOCKER_IMAGE = golang:1.23
GOARM =
GOOS =
GOARCH =
VERSION = $(shell git describe --tags --abbrev=0)
COMMIT_HASH = $(shell git rev-parse --short HEAD)

# Rules
.PHONY: all clean docker-build

all: check-params build

check-params:
	@if [ -z "$(OS)" ] || [ -z "$(ARCH)" ]; then \
		echo "Error: OS and ARCH must be specified."; \
		echo "Usage: make OS=<os> ARCH=<arch>"; \
		echo "Supported OS: darwin (macOS), linux"; \
		echo "Supported Arch: amd64, arm, arm64"; \
		exit 1; \
	fi

build: set-params
	@echo "Building for $(OS) ($(ARCH))..."
	GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) go build -ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH)" -o "${OUTPUT_NAME}"

# build in docker
docker-build: check-params set-params
	@echo "Building $(OUTPUT_NAME) in Docker for $(OS) ($(ARCH))..."
	docker run --rm -e OUTPUT_NAME="$(OUTPUT_NAME)" -v "$$(pwd)":/app -w /app $(DOCKER_IMAGE) /bin/bash -c "GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) go build -ldflags '-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH)' -o ${OUTPUT_NAME}"


set-params:
	@if [ "$(OS)" == "darwin" ]; then \
		if [ "$(ARCH)" == "amd64" ]; then \
			GOOS="darwin"; \
			GOARCH="amd64"; \
		elif [ "$(ARCH)" == "arm64" ]; then \
			GOOS="darwin"; \
			GOARCH="arm64"; \
		else \
			echo "Unsupported architecture for macOS: $(ARCH)"; \
			exit 1; \
		fi; \
	elif [ "$(OS)" == "linux" ]; then \
		if [ "$(ARCH)" == "amd64" ]; then \
			GOOS="linux"; \
			GOARCH="amd64"; \
		elif [ "$(ARCH)" == "arm" ]; then \
			GOOS="linux"; \
			GOARCH="arm"; \
		elif [ "$(ARCH)" == "arm64" ]; then \
			GOOS="linux"; \
			GOARCH="arm64"; \
		else \
			echo "Unsupported architecture for Linux: $(ARCH)"; \
			exit 1; \
		fi; \
	else \
		echo "Unsupported operating system: $(OS)"; \
		exit 1; \
	fi

clean:
	rm -f $(OUTPUT_NAME)