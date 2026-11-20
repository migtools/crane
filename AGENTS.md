# Crane AI Development Guidelines

## Project Context

Crane is a Kubernetes migration tool that helps migrate workloads between clusters. It follows Unix philosophy: small, focused tools assembled in powerful ways.

**Architecture:**
- CLI tool written in Go
- Plugin-based transformation system
- Non-destructive pipeline: export → transform → apply
- Works with Kubernetes API, unstructured resources

**Key repositories:**
- migtools/crane (this repo) - CLI tool
- migtools/crane-lib - transformation logic
- migtools/crane-plugins - community plugins
- migtools/pvc-transfer - PV migration

## Code Quality Standards

### Go Conventions
- Follow standard Go idioms and effective Go practices
- Use `gofmt` for formatting (already enforced)
- Prefer explicit error messages with context
- When working with Kubernetes API objects:
  - Use `*unstructured.Unstructured` for dynamic resources
  - Always check type assertions and provide informative error messages
  - Include the actual resource type and API resource name in errors

### Error Handling
**CRITICAL:** Error messages must be actionable and include:
- The actual type received (not nil pointers to variables)
- The API resource being processed
- Enough context to debug without re-running

Example of good error handling:
```go
// BAD - shows nil pointer
fmt.Errorf("expected *unstructured.Unstructured but got %T", u)

// GOOD - shows actual object type
fmt.Errorf("expected *unstructured.Unstructured but got %T", object)
```

### Testing
- All new features require tests
- Bug fixes should include regression tests when possible
- Use table-driven tests for multiple scenarios
- Test files: `*_test.go` in same package
- Run cluster-free unit checks: `make test-unit`
- Compile all E2E suites without executing them: `make test-e2e-compile`
- Live E2E suites live in `e2e-tests/tests/` and require configured clusters and external tools. A bare `go test ./...` includes these suites.

### Code Organization
- Commands in `cmd/<command>/` (apply, export, transform, etc.)
- Each command is self-contained with its own package
- Shared utilities should go in crane-lib, not duplicated
- Plugin system for transformations (JSONPatch format)

## Development Workflow

### Before Starting Work
1. Check existing issues and PRs to avoid duplication
2. For bugs: verify reproduction steps
3. For features: ensure alignment with Crane's Unix philosophy

### Making Changes
1. Keep changes focused - one logical change per PR
2. Bug fixes should be minimal, surgical changes
3. Don't refactor unrelated code in the same PR
4. Preserve backward compatibility unless explicitly breaking

### Commits
- Use conventional commits format: `type: description`
  - `fix:` for bug fixes
  - `feat:` for new features
  - `refactor:` for code restructuring
  - `test:` for test additions
  - `docs:` for documentation
- Keep commits atomic and logical
- Include issue references when applicable

### Pull Requests
**Title:** Clear, concise (< 70 chars)

Use the PR title aliases documented in `CONTRIBUTING.md`; these differ from Conventional Commit prefixes.

**Body must include:**
- ## Summary - what changed and why
- ## Impact - severity, affected components
- ## Test plan - how to verify

**Before submitting:**
- [ ] Code compiles: `make build`
- [ ] Cluster-free unit tests pass: `make test-unit`
- [ ] E2E packages compile: `make test-e2e-compile`
- [ ] Check tracked Go formatting: `make fmt-check`; report pre-existing differences separately
- [ ] State whether focused live E2E execution ran and which prerequisites were unavailable if skipped
- [ ] Error messages are informative
- [ ] No unnecessary refactoring
- [ ] Backward compatible (or documented breaking change)

## Kubernetes-Specific Guidelines

### Working with Unstructured Resources
- Always validate type assertions
- Use `meta.Accessor` for metadata access when possible
- Handle API errors gracefully (not found, forbidden, etc.)
- Resource names format: `Kind_group_version_namespace_name.yaml`

### Discovery and Export
- Respect namespace boundaries
- Handle cluster-scoped vs namespaced resources correctly
- Paginate large resource lists
- Log resource counts and progress

### Transformations
- JSONPatch operations only (RFC 6902)
- Transformations must be idempotent
- Document plugin behavior clearly
- Test transformations with real Kubernetes objects

## Common Pitfalls

1. **Don't assume object types** - always check type assertions
2. **Don't use nil pointer variables in error messages** - use the actual object
3. **Don't modify cluster state in export** - read-only operations
4. **Don't hardcode API versions** - use discovery
5. **Don't skip error context** - include resource names and types

## AI Assistant Guidelines

### E2E test changes

When creating, changing, reviewing, or debugging tests under `e2e-tests/`
or their CI workflows, read and follow
`.claude/skills/crane-e2e-tests/SKILL.md` before making changes.

### When fixing bugs:
- Read the affected file first
- Understand the context and data flow
- Make minimal, targeted changes
- Test error paths explicitly
- Update error messages to be developer-friendly

### When adding features:
- Check if similar functionality exists
- Consider plugin system for extensibility
- Follow existing patterns in cmd/ structure
- Document new flags and behavior

### When reviewing code:
- Verify error messages are actionable
- Check for proper type assertions
- Ensure Kubernetes API best practices
- Look for potential nil pointer dereferences

## Project-Specific Context

**Migration Philosophy:**
- Non-destructive operations
- Transparent, auditable pipelines
- Output to disk for versioning
- Repeatable with same inputs

**User Experience:**
- Clear progress logging
- Helpful error messages
- Dry-run capabilities
- GitOps-friendly output

## Resources

- [Effective Go](https://go.dev/doc/effective_go)
- [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- [Konveyor Documentation](https://konveyor.github.io/crane/)
