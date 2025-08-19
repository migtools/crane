#!/bin/bash
#
# build.sh - Robust build script for Crane with network retry logic
#
# This script provides multiple strategies for building Crane in environments
# with network restrictions or proxy requirements.

set -e

# Configuration
BINARY_NAME="${BINARY_NAME:-crane}"
BUILD_DIR="${BUILD_DIR:-./bin}"
MAX_RETRIES=3
TIMEOUT=300  # 5 minutes

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Go is installed
check_go() {
    if ! command -v go &> /dev/null; then
        log_error "Go is not installed or not in PATH"
        log_info "Please install Go from https://golang.org/dl/"
        exit 1
    fi
    log_info "Go version: $(go version)"
}

# Display help
show_help() {
    cat << EOF
Crane Build Script
==================

Usage: $0 [OPTIONS]

OPTIONS:
    -h, --help          Show this help message
    -m, --method METHOD Build method to use (default: standard)
    -o, --output DIR    Output directory (default: ./bin)
    -n, --name NAME     Binary name (default: crane)
    -r, --retries NUM   Number of retry attempts (default: 3)
    -t, --timeout SEC   Timeout per attempt in seconds (default: 300)
    -p, --proxy URL     HTTP proxy URL
    -v, --verbose       Verbose output

BUILD METHODS:
    standard            Standard build with go build
    retry               Build with retry logic for network timeouts
    vendor              Create vendor directory and build offline
    direct              Bypass Go module proxy (GOPROXY=direct)
    proxy               Use HTTP proxy (requires --proxy option)

EXAMPLES:
    $0                                    # Standard build
    $0 --method retry                     # Build with retries
    $0 --method vendor                    # Offline build with vendor
    $0 --method direct                    # Bypass module proxy
    $0 --method proxy --proxy http://proxy:8080  # Use corporate proxy

TROUBLESHOOTING NETWORK ISSUES:
    If you're experiencing "TLS handshake timeout" errors:
    1. Try: $0 --method retry
    2. Try: $0 --method direct
    3. Try: $0 --method vendor (for offline build)
    4. If behind corporate firewall: $0 --method proxy --proxy http://your-proxy:port

EOF
}

# Standard build
build_standard() {
    log_info "Performing standard build..."
    mkdir -p "$BUILD_DIR"
    go build -o "$BUILD_DIR/$BINARY_NAME" main.go
}

# Build with retry logic
build_with_retry() {
    log_info "Building with retry logic (max retries: $MAX_RETRIES)..."
    mkdir -p "$BUILD_DIR"
    
    for attempt in $(seq 1 $MAX_RETRIES); do
        log_info "Build attempt $attempt/$MAX_RETRIES..."
        
        if timeout $TIMEOUT go build -o "$BUILD_DIR/$BINARY_NAME" main.go; then
            log_success "Build successful on attempt $attempt"
            return 0
        else
            if [ $attempt -lt $MAX_RETRIES ]; then
                log_warning "Attempt $attempt failed, retrying in 10 seconds..."
                sleep 10
            else
                log_error "All $MAX_RETRIES attempts failed"
                return 1
            fi
        fi
    done
}

# Vendor build (offline)
build_vendor() {
    log_info "Creating vendor directory and building offline..."
    
    if [ ! -d "vendor" ]; then
        log_info "Creating vendor directory..."
        if ! go mod vendor; then
            log_error "Failed to create vendor directory"
            return 1
        fi
        log_success "Vendor directory created"
    else
        log_info "Vendor directory already exists"
    fi
    
    mkdir -p "$BUILD_DIR"
    log_info "Building with vendor directory..."
    go build -mod=vendor -o "$BUILD_DIR/$BINARY_NAME" main.go
}

# Direct build (bypass proxy)
build_direct() {
    log_info "Building with direct module fetching (bypassing proxy)..."
    mkdir -p "$BUILD_DIR"
    GOPROXY=direct GOSUMDB=off go build -o "$BUILD_DIR/$BINARY_NAME" main.go
}

# Proxy build
build_proxy() {
    if [ -z "$PROXY_URL" ]; then
        log_error "Proxy URL not specified. Use --proxy option."
        return 1
    fi
    
    log_info "Building with HTTP proxy: $PROXY_URL"
    mkdir -p "$BUILD_DIR"
    HTTP_PROXY="$PROXY_URL" HTTPS_PROXY="$PROXY_URL" go build -o "$BUILD_DIR/$BINARY_NAME" main.go
}

# Download dependencies with retry
download_deps() {
    log_info "Downloading dependencies with retry logic..."
    
    for attempt in $(seq 1 $MAX_RETRIES); do
        log_info "Download attempt $attempt/$MAX_RETRIES..."
        
        if timeout $TIMEOUT go mod download; then
            log_success "Dependencies downloaded successfully on attempt $attempt"
            return 0
        else
            if [ $attempt -lt $MAX_RETRIES ]; then
                log_warning "Download attempt $attempt failed, retrying in 10 seconds..."
                sleep 10
            else
                log_error "All $MAX_RETRIES download attempts failed"
                return 1
            fi
        fi
    done
}

# Main execution
main() {
    local method="standard"
    local verbose=false
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -m|--method)
                method="$2"
                shift 2
                ;;
            -o|--output)
                BUILD_DIR="$2"
                shift 2
                ;;
            -n|--name)
                BINARY_NAME="$2"
                shift 2
                ;;
            -r|--retries)
                MAX_RETRIES="$2"
                shift 2
                ;;
            -t|--timeout)
                TIMEOUT="$2"
                shift 2
                ;;
            -p|--proxy)
                PROXY_URL="$2"
                shift 2
                ;;
            -v|--verbose)
                verbose=true
                shift
                ;;
            *)
                log_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # Enable verbose mode
    if [ "$verbose" = true ]; then
        set -x
    fi
    
    log_info "Starting Crane build process..."
    log_info "Method: $method"
    log_info "Output: $BUILD_DIR/$BINARY_NAME"
    
    # Check Go installation
    check_go
    
    # Execute based on method
    case $method in
        standard)
            build_standard
            ;;
        retry)
            # First try to download dependencies
            download_deps || log_warning "Dependency download failed, attempting build anyway..."
            build_with_retry
            ;;
        vendor)
            build_vendor
            ;;
        direct)
            build_direct
            ;;
        proxy)
            build_proxy
            ;;
        *)
            log_error "Unknown build method: $method"
            log_info "Available methods: standard, retry, vendor, direct, proxy"
            exit 1
            ;;
    esac
    
    # Verify build result
    if [ -f "$BUILD_DIR/$BINARY_NAME" ]; then
        log_success "Build completed successfully!"
        log_info "Binary location: $BUILD_DIR/$BINARY_NAME"
        
        # Test the binary
        if "$BUILD_DIR/$BINARY_NAME" --help >/dev/null 2>&1; then
            log_success "Binary test passed"
        else
            log_warning "Binary test failed - binary may not be functional"
        fi
        
        # Show binary info
        log_info "Binary size: $(du -h "$BUILD_DIR/$BINARY_NAME" | cut -f1)"
    else
        log_error "Build failed - binary not created"
        exit 1
    fi
}

# Run main function with all arguments
main "$@"