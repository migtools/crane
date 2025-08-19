# Contributing to Crane

Thank you for your interest in contributing to Crane! This document provides guidelines and information for contributors.

## Development Setup

### Prerequisites

- Go 1.21 or later
- Git
- Make (optional, but recommended)

### Setting Up Your Development Environment

1. **Fork and Clone**
   ```bash
   git clone https://github.com/YOUR_USERNAME/crane.git
   cd crane
   ```

2. **Build the Project**
   ```bash
   # Standard build
   go build -o crane main.go
   
   # Or using Make
   make build
   
   # Or using build script (for network-restricted environments)
   ./scripts/build.sh --method retry
   ```

3. **Run Tests**
   ```bash
   # Run all tests
   go test ./...
   
   # Or using Make
   make test
   
   # Run tests with coverage
   make test-coverage
   ```

4. **Verify Installation**
   ```bash
   ./crane --help
   ```

### Network Issues During Development

If you encounter network issues while setting up your development environment (common in corporate environments), refer to [BUILDING.md](BUILDING.md) for comprehensive troubleshooting:

- **TLS handshake timeout**: Use `make vendor-build` for offline development
- **Corporate proxy**: Use `make build-with-proxy PROXY_URL=http://proxy:port`
- **Firewall restrictions**: Use `make build-direct` to bypass Go module proxy

### Development Workflow

1. **Create a Branch**
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make Changes**
   - Follow Go best practices
   - Add tests for new functionality
   - Update documentation as needed

3. **Test Your Changes**
   ```bash
   # Run tests
   make test
   
   # Build to ensure it compiles
   make build
   
   # Test the binary
   ./bin/crane --help
   ```

4. **Commit and Push**
   ```bash
   git add .
   git commit -m "feat: add new feature"
   git push origin feature/your-feature-name
   ```

5. **Submit a Pull Request**
   - Create a pull request from your fork
   - Include a clear description of your changes
   - Reference any related issues

### Code Style and Standards

- Follow standard Go formatting (`go fmt`)
- Use meaningful variable and function names
- Add comments for exported functions and complex logic
- Ensure all tests pass before submitting

### Testing

- Write unit tests for new functionality
- Ensure existing tests continue to pass
- Include integration tests where appropriate
- Test cross-platform compatibility when relevant

### Documentation

- Update relevant documentation for new features
- Include usage examples in docstrings
- Update BUILDING.md if you change build processes
- Update README.md for significant changes

### Common Development Tasks

#### Adding a New Command

1. Create a new directory under `cmd/` for your command
2. Implement the command following the existing pattern
3. Add the command to `main.go`
4. Add tests for the new command
5. Update documentation

#### Modifying Dependencies

1. Update `go.mod` as needed
2. Run `go mod tidy`
3. Test that build still works in restricted environments:
   ```bash
   make vendor-build  # Test offline build
   make build-direct  # Test direct build
   ```

#### Debugging Build Issues

If you encounter build issues during development:

1. **Check Go version**: `go version` (ensure Go 1.21+)
2. **Clean and rebuild**: `make clean && make build`
3. **Try alternative build methods**: See [BUILDING.md](BUILDING.md)
4. **Check for proxy issues**: Configure HTTP_PROXY/HTTPS_PROXY if needed

### Release Process

(For maintainers)

1. Update version information
2. Create release notes
3. Tag the release
4. Build multi-platform binaries using `make build-all`
5. Upload binaries to GitHub releases

### Getting Help

- **Documentation**: Check [BUILDING.md](BUILDING.md) for build issues
- **Issues**: Search existing [GitHub Issues](https://github.com/konveyor/crane/issues)
- **Discussions**: Use GitHub Discussions for questions
- **Community**: Join the Konveyor community channels

### Reporting Issues

When reporting build issues, please include:

- Operating system and version
- Go version (`go version`)
- Complete error message
- Network environment (corporate proxy, firewall, etc.)
- Build method attempted
- Steps to reproduce

This helps maintainers quickly identify and resolve issues.

Thank you for contributing to Crane!