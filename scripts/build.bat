@echo off
REM build.bat - Windows build script for Crane with network retry logic
REM
REM This script provides multiple strategies for building Crane in Windows
REM environments with network restrictions or proxy requirements.

setlocal enabledelayedexpansion

REM Configuration
set BINARY_NAME=crane.exe
set BUILD_DIR=.\bin
set MAX_RETRIES=3
set TIMEOUT=300

REM Default values
set METHOD=standard
set VERBOSE=0

REM Parse command line arguments
:parse_args
if "%~1"=="" goto :check_help
if "%~1"=="-h" goto :show_help
if "%~1"=="--help" goto :show_help
if "%~1"=="-m" (
    set METHOD=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="--method" (
    set METHOD=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-o" (
    set BUILD_DIR=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="--output" (
    set BUILD_DIR=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-n" (
    set BINARY_NAME=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="--name" (
    set BINARY_NAME=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-p" (
    set PROXY_URL=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="--proxy" (
    set PROXY_URL=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-v" (
    set VERBOSE=1
    shift
    goto :parse_args
)
if "%~1"=="--verbose" (
    set VERBOSE=1
    shift
    goto :parse_args
)
echo [ERROR] Unknown option: %~1
goto :show_help

:check_help
if "%METHOD%"=="help" goto :show_help

echo [INFO] Starting Crane build process...
echo [INFO] Method: %METHOD%
echo [INFO] Output: %BUILD_DIR%\%BINARY_NAME%

REM Check Go installation
call :check_go
if !errorlevel! neq 0 exit /b 1

REM Create output directory
if not exist "%BUILD_DIR%" mkdir "%BUILD_DIR%"

REM Execute based on method
if "%METHOD%"=="standard" call :build_standard
if "%METHOD%"=="retry" call :build_with_retry
if "%METHOD%"=="vendor" call :build_vendor
if "%METHOD%"=="direct" call :build_direct
if "%METHOD%"=="proxy" call :build_proxy

if !errorlevel! neq 0 (
    echo [ERROR] Build failed
    exit /b 1
)

REM Verify build result
if exist "%BUILD_DIR%\%BINARY_NAME%" (
    echo [SUCCESS] Build completed successfully!
    echo [INFO] Binary location: %BUILD_DIR%\%BINARY_NAME%
    
    REM Test the binary
    "%BUILD_DIR%\%BINARY_NAME%" --help >nul 2>&1
    if !errorlevel! equ 0 (
        echo [SUCCESS] Binary test passed
    ) else (
        echo [WARNING] Binary test failed - binary may not be functional
    )
) else (
    echo [ERROR] Build failed - binary not created
    exit /b 1
)

goto :eof

:show_help
echo Crane Windows Build Script
echo ==========================
echo.
echo Usage: %~nx0 [OPTIONS]
echo.
echo OPTIONS:
echo     -h, --help          Show this help message
echo     -m, --method METHOD Build method to use (default: standard)
echo     -o, --output DIR    Output directory (default: .\bin)
echo     -n, --name NAME     Binary name (default: crane.exe)
echo     -p, --proxy URL     HTTP proxy URL
echo     -v, --verbose       Verbose output
echo.
echo BUILD METHODS:
echo     standard            Standard build with go build
echo     retry               Build with retry logic for network timeouts
echo     vendor              Create vendor directory and build offline
echo     direct              Bypass Go module proxy (GOPROXY=direct)
echo     proxy               Use HTTP proxy (requires --proxy option)
echo.
echo EXAMPLES:
echo     %~nx0                                    # Standard build
echo     %~nx0 --method retry                     # Build with retries
echo     %~nx0 --method vendor                    # Offline build with vendor
echo     %~nx0 --method direct                    # Bypass module proxy
echo     %~nx0 --method proxy --proxy http://proxy:8080  # Use corporate proxy
echo.
echo TROUBLESHOOTING NETWORK ISSUES:
echo     If you're experiencing "TLS handshake timeout" errors:
echo     1. Try: %~nx0 --method retry
echo     2. Try: %~nx0 --method direct
echo     3. Try: %~nx0 --method vendor (for offline build)
echo     4. If behind corporate firewall: %~nx0 --method proxy --proxy http://your-proxy:port
echo.
echo WINDOWS-SPECIFIC NOTES:
echo     - Ensure Go is installed and in your PATH
echo     - For corporate networks, you may need to configure proxy settings
echo     - Run as Administrator if you encounter permission issues
echo     - Antivirus software may interfere with the build process
exit /b 0

:check_go
where go >nul 2>&1
if !errorlevel! neq 0 (
    echo [ERROR] Go is not installed or not in PATH
    echo [INFO] Please install Go from https://golang.org/dl/
    exit /b 1
)
for /f "tokens=*" %%i in ('go version') do set GO_VERSION=%%i
echo [INFO] Go version: !GO_VERSION!
exit /b 0

:build_standard
echo [INFO] Performing standard build...
go build -o "%BUILD_DIR%\%BINARY_NAME%" main.go
exit /b !errorlevel!

:build_with_retry
echo [INFO] Building with retry logic (max retries: %MAX_RETRIES%)...

REM First try to download dependencies
call :download_deps

for /L %%i in (1,1,%MAX_RETRIES%) do (
    echo [INFO] Build attempt %%i/%MAX_RETRIES%...
    
    go build -o "%BUILD_DIR%\%BINARY_NAME%" main.go
    if !errorlevel! equ 0 (
        echo [SUCCESS] Build successful on attempt %%i
        exit /b 0
    ) else (
        if %%i lss %MAX_RETRIES% (
            echo [WARNING] Attempt %%i failed, retrying in 10 seconds...
            timeout /t 10 /nobreak >nul
        ) else (
            echo [ERROR] All %MAX_RETRIES% attempts failed
            exit /b 1
        )
    )
)
exit /b 1

:build_vendor
echo [INFO] Creating vendor directory and building offline...

if not exist "vendor" (
    echo [INFO] Creating vendor directory...
    go mod vendor
    if !errorlevel! neq 0 (
        echo [ERROR] Failed to create vendor directory
        exit /b 1
    )
    echo [SUCCESS] Vendor directory created
) else (
    echo [INFO] Vendor directory already exists
)

echo [INFO] Building with vendor directory...
go build -mod=vendor -o "%BUILD_DIR%\%BINARY_NAME%" main.go
exit /b !errorlevel!

:build_direct
echo [INFO] Building with direct module fetching (bypassing proxy)...
set GOPROXY=direct
set GOSUMDB=off
go build -o "%BUILD_DIR%\%BINARY_NAME%" main.go
exit /b !errorlevel!

:build_proxy
if "%PROXY_URL%"=="" (
    echo [ERROR] Proxy URL not specified. Use --proxy option.
    exit /b 1
)

echo [INFO] Building with HTTP proxy: %PROXY_URL%
set HTTP_PROXY=%PROXY_URL%
set HTTPS_PROXY=%PROXY_URL%
go build -o "%BUILD_DIR%\%BINARY_NAME%" main.go
exit /b !errorlevel!

:download_deps
echo [INFO] Downloading dependencies with retry logic...

for /L %%i in (1,1,%MAX_RETRIES%) do (
    echo [INFO] Download attempt %%i/%MAX_RETRIES%...
    
    go mod download
    if !errorlevel! equ 0 (
        echo [SUCCESS] Dependencies downloaded successfully on attempt %%i
        exit /b 0
    ) else (
        if %%i lss %MAX_RETRIES% (
            echo [WARNING] Download attempt %%i failed, retrying in 10 seconds...
            timeout /t 10 /nobreak >nul
        ) else (
            echo [WARNING] All %MAX_RETRIES% download attempts failed
            exit /b 1
        )
    )
)
exit /b 1