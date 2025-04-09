#!/bin/bash

# Exit on error
set -e

# Default values
BUILD_DATE=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
VCS_REF=$(git rev-parse --short HEAD)
BUILD_VERSION="1.0.0"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --build-date)
      BUILD_DATE="$2"
      shift 2
      ;;
    --vcs-ref)
      VCS_REF="$2"
      shift 2
      ;;
    --build-version)
      BUILD_VERSION="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Create a temporary file for logging
LOG_FILE=$(mktemp)
echo "Build log will be saved to: $LOG_FILE"

# Build the image with buildx
echo "Building Docker image..."
docker buildx build \
  --platform linux/amd64 \
  --progress=plain \
  --build-arg BUILD_DATE="$BUILD_DATE" \
  --build-arg VCS_REF="$VCS_REF" \
  --build-arg BUILD_VERSION="$BUILD_VERSION" \
  -t tiny-mysql-client:latest \
  . 2>&1 | tee "$LOG_FILE"

# Check if the build was successful
if [ ${PIPESTATUS[0]} -ne 0 ]; then
  echo "Build failed! Check the log file for details: $LOG_FILE"
  exit 1
fi

echo "Build completed successfully!"
echo "Image: tiny-mysql-client:latest" 