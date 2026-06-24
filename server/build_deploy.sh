#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SERVER_DIR="$PROJECT_ROOT/server"
UI_DIR="$PROJECT_ROOT/ui"
DEPLOY_DIR="$SERVER_DIR/deploy"

if [ -f "$SERVER_DIR/startup/VERSION.txt" ]; then
    VERSION=$(cat "$SERVER_DIR/startup/VERSION.txt")
fi
if [ -z "$VERSION" ]; then
    VERSION="dev"
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

TARGET_OS=""
TARGET_ARCH=""
BUILD_TARGET=""
TARGET_EXPLICIT=false
BUILD_DOCKER=false
DOCKER_IMAGE=""
DOCKER_PLATFORM=""
DOCKER_OS=""
DOCKER_ARCH=""
DOCKER_DEPLOY_ARCHIVE=""

usage() {
    echo -e "${BLUE}XACT Deployment Builder${NC}"
    echo "=========================="
    echo ""
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -t, --target OS/ARCH   Specify target platform (e.g., linux/amd64, windows/amd64, darwin/arm64)"
    echo "  --all                  Build all targets"
    echo "  --docker               Build a Docker image from the Linux deploy artifacts"
    echo "  --docker-image NAME    Docker image tag (default: xact:$VERSION)"
    echo "  --docker-platform P    Docker platform to package (default: linux/amd64, or --target when Linux)"
    echo "  -h, --help             Show this help message"
    echo ""
    echo "If no target is specified, builds for the current host OS and architecture."
    echo ""
    echo "Supported targets:"
    echo "  OS:   linux, windows, darwin"
    echo "  ARCH: amd64, arm64, arm"
    exit 0
}

parse_target() {
    local target="$1"
    if [[ ! "$target" =~ ^([a-z]+)/([a-z0-9]+)$ ]]; then
        echo -e "${RED}Invalid target format: $target${NC}"
        echo "Expected format: OS/ARCH (e.g., linux/amd64)"
        exit 1
    fi
    TARGET_OS="${BASH_REMATCH[1]}"
    TARGET_ARCH="${BASH_REMATCH[2]}"

    case "$TARGET_OS" in
        linux|windows|darwin) ;;
        *) echo -e "${RED}Unsupported OS: $TARGET_OS${NC}"; exit 1 ;;
    esac

    case "$TARGET_ARCH" in
        amd64|arm64|arm) ;;
        *) echo -e "${RED}Unsupported ARCH: $TARGET_ARCH${NC}"; exit 1 ;;
    esac
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        -t|--target)
            parse_target "$2"
            BUILD_TARGET="single"
            TARGET_EXPLICIT=true
            shift 2
            ;;
        --all)
            BUILD_TARGET="all"
            shift
            ;;
        --docker)
            BUILD_DOCKER=true
            shift
            ;;
        --docker-image)
            DOCKER_IMAGE="$2"
            shift 2
            ;;
        --docker-platform)
            DOCKER_PLATFORM="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            usage
            ;;
    esac
done

if [ -z "$BUILD_TARGET" ]; then
    if [ "$BUILD_DOCKER" = true ] && [ -z "$DOCKER_PLATFORM" ]; then
        TARGET_OS=linux
        TARGET_ARCH=amd64
    elif [ -z "$TARGET_OS" ]; then
        TARGET_OS=$(go env GOOS)
        TARGET_ARCH=$(go env GOARCH)
    fi
    BUILD_TARGET="single"
fi

if [ "$BUILD_DOCKER" = true ]; then
    if [ -z "$DOCKER_IMAGE" ]; then
        DOCKER_IMAGE="xact:$VERSION"
    fi
    if [ -z "$DOCKER_PLATFORM" ]; then
        if [ "$BUILD_TARGET" = "single" ] && [ "$TARGET_OS" = "linux" ]; then
            DOCKER_PLATFORM="$TARGET_OS/$TARGET_ARCH"
        else
            DOCKER_PLATFORM="linux/amd64"
        fi
    fi
    if [[ ! "$DOCKER_PLATFORM" =~ ^linux/(amd64|arm64|arm)$ ]]; then
        echo -e "${RED}Unsupported Docker platform: $DOCKER_PLATFORM${NC}"
        echo "Docker images can be built for linux/amd64, linux/arm64, or linux/arm."
        exit 1
    fi
    DOCKER_OS="${DOCKER_PLATFORM%%/*}"
    DOCKER_ARCH="${DOCKER_PLATFORM##*/}"

    if [ "$BUILD_TARGET" = "single" ]; then
        if [ "$TARGET_EXPLICIT" = true ] && { [ "$TARGET_OS" != "$DOCKER_OS" ] || [ "$TARGET_ARCH" != "$DOCKER_ARCH" ]; }; then
            echo -e "${RED}--docker-platform ($DOCKER_PLATFORM) must match --target ($TARGET_OS/$TARGET_ARCH) for a single-target build${NC}"
            exit 1
        fi
        TARGET_OS="$DOCKER_OS"
        TARGET_ARCH="$DOCKER_ARCH"
    fi
fi

echo -e "${BLUE}XACT Deployment Builder${NC}"
echo "=========================="

finish() {
    echo -e "${YELLOW}Cleaning up intermediate files...${NC}"
    rm -rf "$DEPLOY_DIR/intermediate"
}
trap finish EXIT

echo -e "${YELLOW}Creating deploy directory structure...${NC}"
mkdir -p "$DEPLOY_DIR"/{linux,windows,darwin}
mkdir -p "$DEPLOY_DIR/intermediate/server"
mkdir -p "$DEPLOY_DIR/intermediate/ui"

echo -e "${YELLOW}Building UI...${NC}"
cd "$UI_DIR"
npm install --silent 2>/dev/null || true
npm run build --silent 2>/dev/null || npm run build
cp -r dist "$DEPLOY_DIR/intermediate/ui/web_assets"

echo -e "${GREEN}UI built successfully${NC}"

build_platform() {
    local OS="$1"
    local ARCH="$2"

    echo -e "${YELLOW}Building Go server for $OS ($ARCH)...${NC}"
    local XACT_BIN="xact_${OS}_${ARCH}"
    local RESTORE_BIN="restore_${OS}_${ARCH}"
    local SUFFIX=""
    if [ "$OS" = "windows" ]; then
        SUFFIX=".exe"
    fi
    cd "$SERVER_DIR"
    CGO_ENABLED=0 GOOS=$OS GOARCH=$ARCH go build -o "$DEPLOY_DIR/intermediate/server/${XACT_BIN}${SUFFIX}" ./startup
    CGO_ENABLED=0 GOOS=$OS GOARCH=$ARCH go build -o "$DEPLOY_DIR/intermediate/server/${RESTORE_BIN}${SUFFIX}" ./cmd/restore
}

if [ "$BUILD_TARGET" = "single" ]; then
    build_platform "$TARGET_OS" "$TARGET_ARCH"
else
    build_platform linux amd64
    build_platform linux arm64
    build_platform linux arm
    build_platform windows amd64
    build_platform darwin amd64
    build_platform darwin arm64
fi

echo -e "${GREEN}All server binaries built${NC}"

random_hex() {
    local BYTES="$1"
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex "$BYTES"
    else
        dd if=/dev/urandom bs=1 count="$BYTES" 2>/dev/null | od -An -tx1 | tr -d ' \n'
    fi
}

verify_tar_extracts_to_current_dir() {
    local ARCHIVE="$1"
    local TOP_DIR="$2"

    if tar -tzf "$ARCHIVE" | grep -Eq '^(\./?|\.)$'; then
        echo -e "${RED}Archive layout error: $ARCHIVE contains a top-level '.' entry${NC}"
        return 1
    fi
    if tar -tzf "$ARCHIVE" | grep -Eq "^(\./)?${TOP_DIR}/"; then
        echo -e "${RED}Archive layout error: $ARCHIVE contains nested $TOP_DIR directory${NC}"
        return 1
    fi
}

create_package() {
    local OS="$1"
    local ARCH="$2"
    local OUTPUT_DIR="$DEPLOY_DIR/$OS"
    local PLATFORM_DIR="$OUTPUT_DIR/xact-$OS-$ARCH"
    local ARCHIVE_DIR="$DEPLOY_DIR"
    local XACT_BIN="xact_${OS}_${ARCH}"
    local RESTORE_BIN="restore_${OS}_${ARCH}"
    local SUFFIX=""

    if [ "$OS" = "windows" ]; then
        SUFFIX=".exe"
        XACT_BIN="xact_${OS}_${ARCH}.exe"
        RESTORE_BIN="restore_${OS}_${ARCH}.exe"
    fi

    if [ ! -f "$DEPLOY_DIR/intermediate/server/${XACT_BIN}" ]; then
        echo -e "${RED}Binary not found: ${XACT_BIN}${NC}"
        return 1
    fi

    echo -e "${YELLOW}Creating $OS-$ARCH package...${NC}"

    rm -rf "$PLATFORM_DIR"
    mkdir -p "$PLATFORM_DIR"/{plugins,data,certs,logs,web}

    local MQTT_SECRET
    local JWT_SECRET_VALUE
    local API_KEY_HASH_SECRET_VALUE
    local NATS_INTERNAL_SECRET
    local NATS_BROWSER_SECRET
    MQTT_SECRET="$(random_hex 24)"
    JWT_SECRET_VALUE="$(random_hex 32)"
    API_KEY_HASH_SECRET_VALUE="$(random_hex 32)"
    NATS_INTERNAL_SECRET="$(random_hex 32)"
    NATS_BROWSER_SECRET="$(random_hex 32)"

    cp "$DEPLOY_DIR/intermediate/server/${XACT_BIN}" "$PLATFORM_DIR/xact${SUFFIX}"
    cp "$DEPLOY_DIR/intermediate/server/${RESTORE_BIN}" "$PLATFORM_DIR/restore${SUFFIX}"
    chmod +x "$PLATFORM_DIR/xact${SUFFIX}"
    chmod +x "$PLATFORM_DIR/restore${SUFFIX}"

    cp -r "$DEPLOY_DIR/intermediate/ui/web_assets/"* "$PLATFORM_DIR/web/"

    # Ship defaults as an example file so redeploying over an existing install
    # does not overwrite operator-managed secrets in .env.
    rm -f "$PLATFORM_DIR/.env"
    cat > "$PLATFORM_DIR/.env.example" << ENVEOF
# XACT Server Configuration
XACT_ENV=production

# Database
SQLITE_PATH=./data/xact.db
XACT_BOOTSTRAP_ADMIN_PASSWORD=
XACT_BOOTSTRAP_ADMIN_PASSWORD_FILE=

# Clustered
CLUSTERED=no

# Plugins
PLUGIN_DIR=./plugins
ENABLE_AUTH_PLUGIN=no

# Embedded MQTT Broker
EMBEDDED_MQTT_SERVER=yes
MQTT_BROKER_URL=mqtt://127.0.0.1:1883
MQTT_BROKER_PASSWORD=${MQTT_SECRET}

# Evaluation defaults: serve the app directly over HTTP on the local network.
# For production, set ENABLE_HTTPS=yes with certificates in HTTP_CERTS_DIR.
ENABLE_HTTPS=no
HTTP_CERTS_DIR=./certs
API_HOST=0.0.0.0
API_PORT=8080
MAX_REQUEST_BODY_BYTES=8388608
# Set to comma-separated UI origins when exposing the API cross-origin.
CORS_ALLOWED_ORIGINS=

# MQTT Ingest Client
MQTT_CLIENT_ENABLED=yes
MQTT_CLIENT_ID=xact-ingest
MQTT_CLIENT_USERNAME=a
# For MQTT over TLS, set MQTT_BROKER_URL to mqtts:// or ssl://. Local
# self-signed certs can be trusted with MQTT_CLIENT_TLS_CA_FILE=./certs/server.crt.
MQTT_CLIENT_TLS_CA_FILE=
MQTT_CLIENT_TLS_SERVER_NAME=
MQTT_CLIENT_TLS_INSECURE_SKIP_VERIFY=false
MQTT_CLIENT_WORKERS=4
MQTT_CLIENT_QUEUE_SIZE=1000

# NATS
JWT_SECRET=${JWT_SECRET_VALUE}
API_KEY_HASH_SECRET=${API_KEY_HASH_SECRET_VALUE}
NATS_HOST=127.0.0.1
NATS_PORT=4222
NATS_WS_HOST=0.0.0.0
NATS_WS_PORT=9222
NATS_DEBUG=false
NATS_TRACE=false
NATS_LOG_FILE=./logs/nats.log
NATS_INTERNAL_PASSWORD=${NATS_INTERNAL_SECRET}
NATS_BROWSER_TOKEN=${NATS_BROWSER_SECRET}
NATS_BROWSER_ALLOW_COMMANDS=no
EXPOSE_NATS_INTERNAL_CONFIG=no
NATS_WS_PATH=/xact/ws

# Embedded MCP Endpoint
MCP_ENABLED=no
MCP_ROUTE=/api/v1/mcp
MCP_WRITE_TOOLS_ENABLED=no
MCP_TOOL_TIMEOUT_SECONDS=30
MCP_MAX_PAYLOAD_BYTES=1048576
MCP_DOCS_ROOT=
MCP_EXAMPLES_ROOT=

# Events / Audit
# 0 disables application-side event purging. Use a positive value only when retention policy allows deletion.
EVENT_RETENTION_DAYS=0

# Scheduler
ENABLE_UNSAFE_SCHEDULER_TASKS=no
SCHEDULER_OUTPUT_DIR=./backups
SCHEDULER_WORK_DIR=./backups

# Restore CLI
# Set these only for non-interactive restore operations.
XACT_RESTORE_CONFIRM=no
XACT_RESTORE_SHA256=
XACT_RESTORE_SAFETY_DIR=./backups

# Directories
NATS_STORE_DIR=./data/nats-store

# Static Files
# proxy or server
STATIC_SERVE_MODE=server
STATIC_DIR=./web
ENVEOF

    if [ "$OS" = "windows" ]; then
        cat > "$PLATFORM_DIR/start.bat" << 'BATEOF'
@echo off
setlocal

set "SCRIPT_DIR=%~dp0"
cd /d "%SCRIPT_DIR%"

if not exist .env (
    if exist .env.example (
        echo Creating .env from .env.example
        copy .env.example .env >nul
    )
)

if exist data (
    echo Data directory exists, preserving...
) else (
    mkdir data
)

xact.exe
BATEOF

        openssl req -x509 -newkey rsa:4096 -keyout "$PLATFORM_DIR/certs/server.key" \
            -out "$PLATFORM_DIR/certs/server.crt" -days 365 -nodes \
            -subj "/CN=localhost/O=XACT/C=US" \
            -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" 2>/dev/null || true
    else
        cat > "$PLATFORM_DIR/start.sh" << 'SHEOF'
#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

if [ ! -f ".env" ] && [ -f ".env.example" ]; then
    echo "Creating .env from .env.example"
    cp .env.example .env
fi

if [ ! -d "data" ]; then
    echo "Creating data directory..."
    mkdir -p data
fi
mkdir -p data/client-body-temp

if [ ! -d "logs" ]; then
    mkdir -p logs
fi

echo "Starting XACT server"

exec ./xact
SHEOF
        chmod +x "$PLATFORM_DIR/start.sh"

        openssl req -x509 -newkey rsa:4096 -keyout "$PLATFORM_DIR/certs/server.key" \
            -out "$PLATFORM_DIR/certs/server.crt" -days 365 -nodes \
            -subj "/CN=localhost/O=XACT/C=US" \
            -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" 2>/dev/null || true
    fi

    local ARCHIVE
    local PACKAGE_ENTRIES=()
    while IFS= read -r ENTRY; do
        PACKAGE_ENTRIES+=("$ENTRY")
    done < <(cd "$PLATFORM_DIR" && find . -mindepth 1 -maxdepth 1 -print | sed 's#^\./##' | sort)
    if [ "${#PACKAGE_ENTRIES[@]}" -eq 0 ]; then
        echo -e "${RED}Package directory is empty: $PLATFORM_DIR${NC}"
        return 1
    fi

    if [ "$OS" = "windows" ]; then
        ARCHIVE="$ARCHIVE_DIR/xact-$OS-$ARCH-$VERSION.zip"
        rm -f "$ARCHIVE"
        (cd "$PLATFORM_DIR" && zip -q -r "$ARCHIVE" "${PACKAGE_ENTRIES[@]}")
        echo -e "${GREEN}Created xact-$OS-$ARCH-$VERSION.zip${NC}"
    else
        ARCHIVE="$ARCHIVE_DIR/xact-$OS-$ARCH-$VERSION.tar.gz"
        rm -f "$ARCHIVE"
        tar -czf "$ARCHIVE" -C "$PLATFORM_DIR" "${PACKAGE_ENTRIES[@]}"
        verify_tar_extracts_to_current_dir "$ARCHIVE" "xact-$OS-$ARCH"
        echo -e "${GREEN}Created xact-$OS-$ARCH-$VERSION.tar.gz${NC}"
    fi
}

build_docker_image() {
    local OS="$1"
    local ARCH="$2"
    local PLATFORM_DIR="$DEPLOY_DIR/$OS/xact-$OS-$ARCH"
    local IMAGE_ROOT="$DEPLOY_DIR/intermediate/docker-image"

    if [ "$OS" != "linux" ]; then
        echo -e "${RED}Docker image requires a Linux target, got $OS/$ARCH${NC}"
        return 1
    fi
    if [ ! -x "$PLATFORM_DIR/xact" ] || [ ! -x "$PLATFORM_DIR/restore" ] || [ ! -d "$PLATFORM_DIR/web" ]; then
        echo -e "${RED}Docker artifacts missing in $PLATFORM_DIR${NC}"
        return 1
    fi
    if ! command -v docker >/dev/null 2>&1; then
        echo -e "${RED}docker command not found${NC}"
        return 1
    fi

    echo -e "${YELLOW}Preparing Docker image artifacts for $OS/$ARCH...${NC}"
    rm -rf "$IMAGE_ROOT"
    mkdir -p "$IMAGE_ROOT"
    cp "$PLATFORM_DIR/xact" "$IMAGE_ROOT/xact"
    cp "$PLATFORM_DIR/restore" "$IMAGE_ROOT/restore"
    cp -r "$PLATFORM_DIR/web" "$IMAGE_ROOT/web"

    echo -e "${YELLOW}Building Docker image $DOCKER_IMAGE for $OS/$ARCH...${NC}"
    local ATTEMPT=1
    local MAX_ATTEMPTS=3
    until docker build \
        --platform "$OS/$ARCH" \
        --build-arg "XACT_ARTIFACT_DIR=server/deploy/intermediate/docker-image" \
        -t "$DOCKER_IMAGE" \
        "$PROJECT_ROOT"; do
        if [ "$ATTEMPT" -ge "$MAX_ATTEMPTS" ]; then
            echo -e "${RED}Docker image build failed after $MAX_ATTEMPTS attempts${NC}"
            return 1
        fi
        local WAIT_SECONDS=$((ATTEMPT * 15))
        echo -e "${YELLOW}Docker image build failed; retrying in ${WAIT_SECONDS}s...${NC}"
        sleep "$WAIT_SECONDS"
        ATTEMPT=$((ATTEMPT + 1))
    done
    echo -e "${GREEN}Built Docker image $DOCKER_IMAGE${NC}"
}

create_docker_deploy_package() {
    local ARCH="$1"
    local PACKAGE_ROOT="$DEPLOY_DIR/intermediate/docker-deploy"
    local ENV_SRC="$PROJECT_ROOT/deploy/docker/.env.example"
    local COMPOSE_SRC="$PROJECT_ROOT/deploy/docker/docker-compose.yml"
    local ARCHIVE="$DEPLOY_DIR/xact-docker-$ARCH-$VERSION.tar.gz"

    if [ ! -f "$ENV_SRC" ]; then
        echo -e "${RED}Docker env example not found: $ENV_SRC${NC}"
        return 1
    fi
    if [ ! -f "$COMPOSE_SRC" ]; then
        echo -e "${RED}Docker compose file not found: $COMPOSE_SRC${NC}"
        return 1
    fi

    echo -e "${YELLOW}Creating Docker deploy package for $ARCH...${NC}"
    rm -rf "$PACKAGE_ROOT"
    mkdir -p "$PACKAGE_ROOT"

    awk -v image="$DOCKER_IMAGE" '
        BEGIN { replaced = 0 }
        /^XACT_IMAGE=/ {
            print "XACT_IMAGE=" image
            replaced = 1
            next
        }
        { print }
        END {
            if (!replaced) {
                print "XACT_IMAGE=" image
            }
        }
    ' "$ENV_SRC" > "$PACKAGE_ROOT/.env.example"
    cp "$COMPOSE_SRC" "$PACKAGE_ROOT/docker-compose.yml"
    mkdir -p \
        "$PACKAGE_ROOT/plugins/authentication" \
        "$PACKAGE_ROOT/plugins/widgets" \
        "$PACKAGE_ROOT/plugins/map-layer" \
        "$PACKAGE_ROOT/plugins/themes" \
        "$PACKAGE_ROOT/postgres-data"

    rm -f "$ARCHIVE"
    tar -czf "$ARCHIVE" -C "$PACKAGE_ROOT" \
        .env.example \
        docker-compose.yml \
        plugins \
        postgres-data
    DOCKER_DEPLOY_ARCHIVE="$ARCHIVE"
    echo -e "${GREEN}Created xact-docker-$ARCH-$VERSION.tar.gz${NC}"
}

if [ "$BUILD_TARGET" = "single" ]; then
    create_package "$TARGET_OS" "$TARGET_ARCH"
else
    create_package linux amd64
    create_package linux arm64
    create_package linux arm
    create_package windows amd64
    create_package darwin amd64
    create_package darwin arm64
    # Always create a docker package for the default Linux/amd64 image.
    create_docker_deploy_package "amd64"
fi

if [ "$BUILD_DOCKER" = true ]; then
    build_docker_image "$DOCKER_OS" "$DOCKER_ARCH"
    create_docker_deploy_package "$DOCKER_ARCH"
fi

echo ""
cd "$DEPLOY_DIR"
echo -e "${GREEN}Build complete!${NC}"
echo "Output files:"
if [ "$BUILD_TARGET" = "single" ]; then
    if [ "$TARGET_OS" = "windows" ]; then
        ls -lh "$DEPLOY_DIR/xact-$TARGET_OS-$TARGET_ARCH-$VERSION.zip" 2>/dev/null || true
    else
        ls -lh "$DEPLOY_DIR/xact-$TARGET_OS-$TARGET_ARCH-$VERSION.tar.gz" 2>/dev/null || true
    fi
else
    ls -lh "$DEPLOY_DIR"/*.tar.gz "$DEPLOY_DIR"/*.zip 2>/dev/null || true
fi
if [ "$BUILD_DOCKER" = true ]; then
    echo "Docker image: $DOCKER_IMAGE ($DOCKER_PLATFORM)"
    ls -lh "$DOCKER_DEPLOY_ARCHIVE" 2>/dev/null || true
fi
echo ""
echo "Build artifacts located at: $DEPLOY_DIR"
echo ""
echo "Version: $VERSION"
if [ "$BUILD_TARGET" = "single" ]; then
    if [ "$TARGET_OS" = "windows" ]; then
        echo "Output file: xact-$TARGET_OS-$TARGET_ARCH-$VERSION.zip"
    else
        echo "Output file: xact-$TARGET_OS-$TARGET_ARCH-$VERSION.tar.gz"
    fi
else
    echo "Each archive contains:"
    echo "  - xact (server executable)"
    echo "  - restore (restore utility)"
    echo "  - web/ (user-configurable web assets)"
    echo "  - plugins/ (plugin directory)"
    echo "  - data/ (database and NATS store)"
    echo "  - certs/ (HTTPS certificates)"
    echo "  - logs/ (log files)"
    echo "  - .env.example (default configuration; start script creates .env only when missing)"
    echo "  - start.sh/start.bat (launcher)"
    echo ""
    echo "linux/darwin: .tar.gz | windows: .zip"
    echo "Extract linux/darwin with: tar -xzf xact-<OS>-<ARCH>-$VERSION.tar.gz"
    echo "Extract windows with: unzip xact-windows-amd64-$VERSION.zip"
fi
