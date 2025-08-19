# Building Crane

This document provides comprehensive instructions for building Crane from source in various environments, including those with network restrictions.

## Prerequisites

- Go 1.21 or later ([Download Go](https://golang.org/dl/))
- Git (for cloning the repository)
- Internet connection (for standard build) or pre-downloaded dependencies (for offline build)

## Quick Start

### Standard Build

```bash
git clone https://github.com/konveyor/crane.git
cd crane
go build -o crane main.go
```

### Using Make

```bash
make build              # Standard build
make build-all          # Multi-platform build
make install            # Install to GOPATH/bin
```

### Using Build Script

```bash
# Linux/macOS
./scripts/build.sh

# Windows
scripts\build.bat
```

## Network Troubleshooting

If you encounter network issues such as "TLS handshake timeout" when downloading Go modules, try the following solutions:

### 1. Retry Build with Extended Timeouts

```bash
# Using Make
make build-with-retries

# Using build script
./scripts/build.sh --method retry

# Windows
scripts\build.bat --method retry
```

### 2. Bypass Go Module Proxy

```bash
# Using Make
make build-direct

# Using build script
./scripts/build.sh --method direct

# Windows
scripts\build.bat --method direct

# Manual
GOPROXY=direct GOSUMDB=off go build -o crane main.go
```

### 3. Offline Build with Vendor Directory

```bash
# Using Make
make vendor-build

# Using build script
./scripts/build.sh --method vendor

# Windows
scripts\build.bat --method vendor

# Manual steps
go mod vendor
go build -mod=vendor -o crane main.go
```

### 4. Corporate Proxy Configuration

If you're behind a corporate firewall, configure your proxy settings:

#### Environment Variables
```bash
export HTTP_PROXY=http://proxy.company.com:8080
export HTTPS_PROXY=http://proxy.company.com:8080
export GOPROXY=https://proxy.golang.org
go build -o crane main.go
```

#### Using Build Script
```bash
# Linux/macOS
./scripts/build.sh --method proxy --proxy http://proxy.company.com:8080

# Windows
scripts\build.bat --method proxy --proxy http://proxy.company.com:8080
```

#### Using Make
```bash
make build-with-proxy PROXY_URL=http://proxy.company.com:8080
```

### 5. Alternative GOPROXY Servers

If the default Go proxy is blocked, try alternative proxy servers:

```bash
# Microsoft Go proxy
export GOPROXY=https://goproxy.cn

# Athens proxy
export GOPROXY=https://athens.azurefd.net

# Build with alternative proxy
go build -o crane main.go
```

## Windows-Specific Instructions

### Prerequisites for Windows

1. Install Go from [golang.org/dl](https://golang.org/dl/)
2. Ensure Go is in your PATH (check with `go version`)
3. Install Git from [git-scm.com](https://git-scm.com/)

### Common Windows Issues

#### Issue: "TLS handshake timeout"
**Solution**: This is typically due to corporate firewalls or antivirus software.

```cmd
REM Try direct module fetching
set GOPROXY=direct
set GOSUMDB=off
go build -o crane.exe main.go

REM Or use the build script
scripts\build.bat --method direct
```

#### Issue: Permission denied
**Solution**: Run Command Prompt as Administrator or exclude the build directory from antivirus scanning.

#### Issue: Proxy authentication required
**Solution**: Include credentials in proxy URL:
```cmd
scripts\build.bat --method proxy --proxy http://username:password@proxy.company.com:8080
```

## Build Options and Environment Variables

### Make Targets

| Target | Description |
|--------|-------------|
| `build` | Standard build for current platform |
| `build-all` | Build for multiple platforms (Linux, macOS, Windows) |
| `build-with-retries` | Build with network retry logic |
| `build-direct` | Bypass Go module proxy |
| `build-with-proxy` | Build using HTTP proxy |
| `vendor-init` | Create vendor directory |
| `vendor-build` | Build using vendor directory (offline) |
| `test` | Run tests |
| `test-coverage` | Run tests with coverage report |
| `clean` | Clean build artifacts |
| `install` | Install binary to GOPATH/bin |

### Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `GOPROXY` | Go module proxy | `https://proxy.golang.org,direct` |
| `GOSUMDB` | Go checksum database | `sum.golang.org` |
| `HTTP_PROXY` | HTTP proxy URL | `http://proxy.company.com:8080` |
| `HTTPS_PROXY` | HTTPS proxy URL | `http://proxy.company.com:8080` |
| `NO_PROXY` | Comma-separated list of hosts to bypass proxy | `localhost,127.0.0.1` |
| `GOOS` | Target operating system | `linux`, `darwin`, `windows` |
| `GOARCH` | Target architecture | `amd64`, `arm64` |

### Build Script Options

#### Linux/macOS (build.sh)

```bash
./scripts/build.sh [OPTIONS]

OPTIONS:
    -h, --help          Show help message
    -m, --method METHOD Build method (standard, retry, vendor, direct, proxy)
    -o, --output DIR    Output directory (default: ./bin)
    -n, --name NAME     Binary name (default: crane)
    -r, --retries NUM   Number of retry attempts (default: 3)
    -t, --timeout SEC   Timeout per attempt in seconds (default: 300)
    -p, --proxy URL     HTTP proxy URL
    -v, --verbose       Verbose output
```

#### Windows (build.bat)

```cmd
scripts\build.bat [OPTIONS]

OPTIONS:
    -h, --help          Show help message
    -m, --method METHOD Build method (standard, retry, vendor, direct, proxy)
    -o, --output DIR    Output directory (default: .\bin)
    -n, --name NAME     Binary name (default: crane.exe)
    -p, --proxy URL     HTTP proxy URL
    -v, --verbose       Verbose output
```

## Cross-Platform Builds

### Build for All Platforms

```bash
# Using Make
make build-all

# Manual
GOOS=linux GOARCH=amd64 go build -o bin/crane-linux-amd64 main.go
GOOS=darwin GOARCH=amd64 go build -o bin/crane-darwin-amd64 main.go
GOOS=darwin GOARCH=arm64 go build -o bin/crane-darwin-arm64 main.go
GOOS=windows GOARCH=amd64 go build -o bin/crane-windows-amd64.exe main.go
```

### Build for Specific Platform

```bash
# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o crane-linux-arm64 main.go

# Windows AMD64
GOOS=windows GOARCH=amd64 go build -o crane-windows-amd64.exe main.go

# macOS ARM64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o crane-darwin-arm64 main.go
```

## Troubleshooting Build Issues

### Common Error Messages and Solutions

#### "TLS handshake timeout"
```
Error: Get "https://proxy.golang.org/...": net/http: TLS handshake timeout
```

**Solutions:**
1. Use direct module fetching: `GOPROXY=direct go build -o crane main.go`
2. Try alternative proxy: `GOPROXY=https://goproxy.cn go build -o crane main.go`
3. Use vendor build: `go mod vendor && go build -mod=vendor -o crane main.go`
4. Configure corporate proxy: `HTTP_PROXY=http://proxy:port go build -o crane main.go`

#### "x509: certificate signed by unknown authority"
```
Error: x509: certificate signed by unknown authority
```

**Solutions:**
1. Update CA certificates on your system
2. Use direct mode: `GOPROXY=direct GOSUMDB=off go build -o crane main.go`
3. Set insecure flag (not recommended): `GOINSECURE=proxy.golang.org go build -o crane main.go`

#### "module not found" or "no matching versions"
```
Error: module github.com/konveyor/crane-lib@v0.0.8 not found
```

**Solutions:**
1. Check internet connectivity
2. Verify go.mod and go.sum files are present and valid
3. Clear module cache: `go clean -modcache`
4. Re-download modules: `go mod download`

#### "permission denied" (Windows)
```
Error: fork/exec ...: permission denied
```

**Solutions:**
1. Run Command Prompt as Administrator
2. Exclude build directory from antivirus scanning
3. Check Windows Defender or corporate security software

#### "failed to connect to proxy" 
```
Error: proxyconnect tcp: dial tcp proxy:8080: connectex: No connection could be made
```

**Solutions:**
1. Verify proxy URL and credentials
2. Check if proxy requires authentication
3. Try alternative proxy servers
4. Use direct mode to bypass proxy

### Build Performance Tips

1. **Use module cache**: After first successful build, subsequent builds will be faster
2. **Vendor dependencies**: For repeated offline builds, use vendor directory
3. **Parallel builds**: Use `go build -p 4` to use 4 parallel compile jobs
4. **Disable CGO**: Use `CGO_ENABLED=0` for static binaries (if applicable)

### Getting Help

If you continue to experience build issues:

1. Check the [GitHub Issues](https://github.com/konveyor/crane/issues) for similar problems
2. Create a new issue with:
   - Your operating system and version
   - Go version (`go version`)
   - Complete error message
   - Network environment details (corporate proxy, firewall, etc.)
   - Build method attempted

## Advanced Build Configuration

### Static Binary Build

```bash
CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o crane main.go
```

### Debug Build

```bash
go build -gcflags="all=-N -l" -o crane main.go
```

### Optimized Release Build

```bash
go build -ldflags="-s -w" -o crane main.go
```

### Build with Version Information

```bash
VERSION=$(git describe --tags --always --dirty)
COMMIT=$(git rev-parse --short HEAD)
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')

go build -ldflags="-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" -o crane main.go
```

This comprehensive guide should help you successfully build Crane in any environment. For the most up-to-date build instructions, always refer to the project's README and release notes.