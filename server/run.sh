#!/bin/bash

# Build and run the XACT server
#
# Options:
#   -t, --test     Run with coverage instrumentation and SQLite in-memory mode
#   -p, --proxy    VITE or NGINX serves static files (server does not serve them)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Parse command line options
TEST_MODE=false
PROXY_MODE=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--test)
            TEST_MODE=true
            shift
            ;;
        -p|--proxy)
            PROXY_MODE=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [-t|--test] [-p|--proxy]"
            exit 1
            ;;
    esac
done

echo -e "${GREEN}XACT Server Builder${NC}"
echo "=========================="

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Ensure bin directory exists
mkdir -p bin

# Clean previous build
echo -e "${YELLOW}Cleaning previous build...${NC}"
rm -f bin/xact

# Build flags
BUILD_FLAGS="-race"
if [ "$TEST_MODE" = true ]; then
    BUILD_FLAGS="-cover $BUILD_FLAGS"
    echo -e "${YELLOW}Coverage instrumentation enabled${NC}"
fi

# Build the server
echo -e "${YELLOW}Building server...${NC}"
go build $BUILD_FLAGS -o bin/xact ./startup

if [ $? -eq 0 ]; then
    echo -e "${GREEN}Build successful!${NC}"
else
    echo -e "${RED}Build failed!${NC}"
    exit 1
fi

# Build the restore tool
echo -e "${YELLOW}Building restore tool...${NC}"
go build -o bin/restore ./cmd/restore

if [ $? -eq 0 ]; then
    echo -e "${GREEN}Restore tool built: bin/restore${NC}"
else
    echo -e "${RED}Restore tool build failed!${NC}"
    exit 1
fi

# Run the server
# Detect HTTPS mode from environment or .env file
HTTPS_MODE=false
if [ "$ENABLE_HTTPS" = "yes" ]; then
    HTTPS_MODE=true
elif [ -f .env ] && grep -q '^ENABLE_HTTPS=yes' .env; then
    HTTPS_MODE=true
fi

if [ "$HTTPS_MODE" = true ]; then
    WEB_SCHEME="https"
    WS_SCHEME="wss"
else
    WEB_SCHEME="http"
    WS_SCHEME="ws"
fi

echo ""
echo -e "${GREEN}Starting XACT Server...${NC}"
echo "=========================="
echo "Services:"
echo "  - NATS: nats://localhost:4222"
echo "  - WebSocket: ${WS_SCHEME}://localhost:9222"
echo "  - REST API: ${WEB_SCHEME}://localhost:8080"
echo "  - API Docs: ${WEB_SCHEME}://localhost:8080/api-docs"
echo "=========================="
echo ""

if [ "$TEST_MODE" = true ]; then
    COVERAGE_DIR="$SCRIPT_DIR/coverage_data"
    mkdir -p "$COVERAGE_DIR"

    # Clean up previous coverage files
    echo -e "${YELLOW}Cleaning previous coverage data...${NC}"
    rm -f "$COVERAGE_DIR"/covcounters.* "$COVERAGE_DIR"/covmeta.* "$COVERAGE_DIR"/coverage.out "$COVERAGE_DIR"/coverage.txt "$COVERAGE_DIR"/coverage.html

    # Create temporary NATS store directory
    NATS_TMP_DIR=$(mktemp -d)
    echo -e "${YELLOW}Using NATS temp dir: $NATS_TMP_DIR${NC}"

    # Set environment for test mode: in-memory SQLite, temp NATS store, server serves static files
    GOCOVERDIR="$COVERAGE_DIR" \
        SQLITE_PATH=":memory:" \
        NATS_STORE_DIR="$NATS_TMP_DIR" \
        STATIC_SERVE_MODE="server" \
        TEST_MODE="true" \
        ./bin/xact
    SERVER_EXIT=$?

    # Merge coverage data by running tests (which produces merged coverage.out)
    echo -e "${YELLOW}Merging coverage data...${NC}"
    set +e
    go test -coverprofile="$COVERAGE_DIR/coverage.out" ./...
    TEST_EXIT=$?
    set -e

    # Generate coverage reports
    echo -e "${YELLOW}Generating coverage reports...${NC}"
    go tool cover -func="$COVERAGE_DIR/coverage.out" -o "$COVERAGE_DIR/coverage.txt"
    go tool cover -html="$COVERAGE_DIR/coverage.out" -o "$COVERAGE_DIR/coverage.html"
    echo -e "${GREEN}Coverage reports generated in $COVERAGE_DIR:${NC}"
    ls -la "$COVERAGE_DIR"

    # Clean up NATS temp directory
    rm -rf "$NATS_TMP_DIR"

    # Exit with server exit code if tests passed, otherwise test failure
    if [ $TEST_EXIT -ne 0 ]; then
        echo -e "${RED}Some tests failed (exit code: $TEST_EXIT)${NC}"
    fi
    exit $SERVER_EXIT
elif [ "$PROXY_MODE" = true ]; then
    # Proxy mode: VITE or NGINX serves static files
    STATIC_SERVE_MODE="proxy" ./bin/xact
else
    # Production mode (default): server serves static files from :8080
    STATIC_SERVE_MODE="server" ./bin/xact
fi
