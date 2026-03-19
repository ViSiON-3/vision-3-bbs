# ViSiON/3 Development Guidelines

## Core Philosophy

- **Simplicity**: Prioritize simple, clear, and maintainable solutions. Avoid unnecessary complexity or over-engineering.
- **Iterate**: Prefer iterating on existing, working code rather than building entirely new solutions from scratch, unless fundamentally necessary or explicitly requested.
- **Focus**: Concentrate efforts on the specific task assigned. Avoid unrelated changes or scope creep.
- **Quality**: Strive for a clean, organized, well-tested, and secure codebase.
- **Reference Sources**: Always refer to project documentation, existing code, especially the source `vision-2-bbs/` Turbo Pascal source code which this project is attempting to modernize with established patterns in the legacy source code before proposing or implementing changes.

---

## Sub-Agent Instructions

Some subsystems have their own dedicated agent instruction files. **Always read
the relevant sub-agent file before working on that subsystem.** These files
take precedence over general guidelines for their scope.

| Subsystem                         | File              | Design Plan                                      |
| --------------------------------- | ----------------- | ------------------------------------------------ |
| V3Net (native message networking) | `AGENTS.v3net.md` | `docs/plans/2026-03-16-v3net-protocol-design.md` |

If a task touches a subsystem with a sub-agent file, read that file first,
then return here for general coding standards.

---

## Project Context & Understanding

### Documentation First

Always check for and review relevant project documentation before starting any task:

- `README.md` — Project overview, setup, patterns, technology stack
- `docs/sysop/reference/architecture.md ` — System architecture, component relationships
- `docs/sysop/reference/api-reference.md` — API reference


If documentation is missing, unclear, or conflicts with the request, ask for
clarification before proceeding.

### Architecture Adherence

- Understand and respect module boundaries, data flow, system interfaces, and component dependencies outlined in `docs/architecture.md`.
- Validate that changes comply with the established architecture. Warn and propose compliant solutions if a violation is detected.
- When adding a new subsystem, update `docs/architecture.md` to reflect it.

### Pattern & Tech Stack Awareness

- Reference `README.md` and `docs/sysop/reference/technical.md` to understand and utilize existing patterns and technologies.
- Exhaust options using existing implementations before proposing new patterns or libraries.
- Check `go.mod` before adding any dependency. If an equivalent library is already present, use it.

---

## Task Execution & Workflow

### Before Starting Any Significant Task

1. Read the relevant sub-agent file if one exists (see Sub-Agent Instructions above).
2. Read `docs/architecture.md` and any relevant `docs/` files.
3. Scan the existing codebase for related patterns — do not reinvent what exists.
4. **Identify Impact**: Determine affected components, dependencies, and potential side effects.
5. **Plan**: Outline the steps. Tackle one logical change or file at a time.
6. **Verify Testing**: Confirm how the change will be tested. Add tests if necessary before implementing.

### Completing a Task

- Run `go test ./...` (or the affected package subtree) before considering any task done.
- Run `gofmt`, `golint`, and `go vet` on changed files.
- Update relevant documentation if the change affects architecture, patterns, or established behavior.
- Commit with a clear, atomic message describing what changed and why.

---

## Code Quality & Style

### Golang Guidelines

- Follow the official Go style guide. Use `gofmt` for formatting.
- Document all exported types, functions, and packages with proper Go comments.
- Follow RESTful or gRPC design principles as per project standards.
- Write clean, well-organized code with meaningful variable and function names.
- Use `slog` (stdlib, Go 1.21+) for structured logging throughout. Do not
  introduce a different logging library.

### Small Files & Components

- Keep files under 300 lines. Refactor proactively when approaching this limit.
- Break down large packages into smaller, single-responsibility modules.
- Follow Go's package organization principles.

### General Rules

- **DRY**: Actively look for and reuse existing functionality. Refactor to eliminate duplication.
- **No Custom Build Systems**: Use Go's built-in tools and established project tooling.
- **Linting**: Use `gofmt`, `golint`, and `go vet`. Follow `golangci-lint` rules if configured.
- **Pattern Consistency**: Adhere to established project patterns. Don't introduce new ones without discussion. If replacing an old pattern, ensure the old implementation is fully removed.
- **File Naming**: Use `snake_case` for Go file names (e.g., `user_profile.go`). Follow Go conventions for package names (lowercase, short, concise). Avoid "temp", "refactored", "improved" in permanent file names.
- **No One-Time Scripts**: Do not commit one-time utility scripts into the main codebase.

---

## Refactoring

- Refactor to improve clarity, reduce duplication, simplify complexity, or adhere to architectural goals.
- When refactoring, look for duplicate code, similar components/files, and opportunities for consolidation.
- Modify existing files directly. Do not duplicate files and rename them (e.g., `profile_service_v2.go`).
- After refactoring, ensure all callers, dependencies, and integration points function correctly. Run relevant tests.

---

## Testing & Validation

### Test-Driven Development (TDD)

- **New Features**: Outline tests, write failing tests, implement code, refactor.
- **Bug Fixes**: Write a test reproducing the bug before fixing it.

### Comprehensive Tests

- Use Go's standard `testing` package for unit tests and benchmarks.
- Use `testify` or other approved testing libraries if already established in the project.
- Implement integration tests for API endpoints and service layers using `httptest`.
- All tests must pass before committing or considering a task complete.
- Use mock data only within test environments. Never use real credentials or live
  network endpoints in tests.

---

## Debugging & Troubleshooting

- **Fix the Root Cause**: Prioritize fixing the underlying issue rather than masking it.
- Check application logs for errors, warnings, or relevant information.
- Use the project's established logging framework (`slog`) with structured logging.
- Check the `fixes/` directory for documented solutions to similar past issues
  before deep-diving.
- Document complex fixes in `fixes/` with a descriptive `.md` file detailing
  the problem, investigation steps, and solution.

---

## Security

- Keep sensitive logic, validation, and data manipulation on the backend.
- Always validate and sanitize incoming data. Implement proper error handling
  to prevent information leakage.
- Be mindful of security implications when adding or updating dependencies.
- Never hardcode secrets or credentials. Use environment variables or secure
  secrets management.
- Cryptographic operations (signing, key generation, verification) must use
  Go stdlib (`crypto/ed25519`, `crypto/rand`, etc.) unless a specific library
  is already established in the project. Do not introduce new crypto libraries.

---

## Version Control

- Commit frequently with clear, atomic messages.
- Keep the working directory clean; ensure no unrelated or temporary files are staged.
- Use `.gitignore` effectively.
- Follow the project's established branching strategy.
- Never commit `.env` files. Use `.env.example` for templates.

---

## Documentation Maintenance

- If code changes impact architecture, technical decisions, or established
  patterns, update the relevant documentation (`README.md`, `docs/sysop/reference/architecture.md`,
  `docs/sysop/reference/technical.md`).
- If a new subsystem is added that warrants its own agent instructions, create
  `internal/{subsystem}/CLAUDE.md` and add a row to the Sub-Agent Instructions
  table at the top of this file.

---

## Go-Specific Best Practices

### Error Handling

- Always check error returns and handle them appropriately.
- Use meaningful error messages and consider custom error types for complex scenarios.
- Follow project conventions for error wrapping: `fmt.Errorf("context: %w", err)`.
- Use `errors.Is` / `errors.As` for error inspection. Never string-match error messages.

### Concurrency

- Use goroutines and channels with caution and care.
- Implement proper synchronization mechanisms (`sync.Mutex`, `sync.WaitGroup`, etc.).
- Be aware of race conditions and deadlocks. Run `go test -race ./...` on concurrent code.
- All goroutines must respect `context.Context` cancellation. No goroutine should
  run indefinitely without a shutdown path.

### Resource Management

- Always close resources (files, connections, HTTP response bodies, etc.) using
  `defer` when appropriate.
- Implement proper context handling for cancellation and timeouts.
- Database connections must be closed or returned to pool; use `defer rows.Close()`
  and `defer stmt.Close()` consistently.

### Interface Design

- Keep interfaces small and focused (prefer 1–3 methods).
- Define interfaces at the consuming package, not the implementing package,
  except for well-established patterns.

### Project Structure

- Follow the project's established directory structure for new packages.
- Use Go modules for dependency management. Keep `go.mod` tidy (`go mod tidy`
  after any dependency change).
- Organize code by functionality rather than type.
