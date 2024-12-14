#!/bin/bash

# Function to show usage
usage() {
    echo "Usage: $0 [os] [arch]"
    echo "Supported OS: darwin (macOS), linux"
    echo "Supported Arch: amd64, arm, arm64"
    echo "Example: ./build.sh darwin amd64"
    exit 1
}

# Check for the correct number of arguments
if [ $# -ne 2 ]; then
    usage
fi

OS=$1
ARCH=$2
OUTPUT_NAME="gitlab-autoscaler"

# Determine GOARM for ARM architecture
GOARM=""
if [ "$ARCH" == "arm" ]; then
    GOARM=7  # Set to 7 for ARMv7
fi

# Set the output filename based on the OS and architecture
if [ "$OS" == "darwin" ]; then
    if [ "$ARCH" == "amd64" ]; then
        GOOS="darwin"
        GOARCH="amd64"
    elif [ "$ARCH" == "arm64" ]; then
        GOOS="darwin"
        GOARCH="arm64"
    else
        echo "Unsupported architecture for macOS: $ARCH"
        usage
    fi
elif [ "$OS" == "linux" ]; then
    if [ "$ARCH" == "amd64" ]; then

        GOOS="linux"
        GOARCH="amd64"
    elif [ "$ARCH" == "arm" ]; then
        GOOS="linux"
        GOARCH="arm"
    elif [ "$ARCH" == "arm64" ]; then
        GOOS="linux"
        GOARCH="arm64"
    else
        echo "Unsupported architecture for Linux: $ARCH"
        usage
    fi
else
    echo "Unsupported operating system: $OS"
    usage
fi

# Build the application
echo "Building for $OS ($ARCH)..."
 GOOS=$GOOS GOARCH=$GOARCH GOARM=$GOARM go build -ldflags "-X main.Version=$(git describe --tags --abbrev=0) -X main.CommitHash=$(git rev-parse --short HEAD)" -o $OUTPUT_NAME

if [ $? -eq 0 ]; then
    echo "Build successful: $OUTPUT_NAME"
else
    echo "Build failed"
fi