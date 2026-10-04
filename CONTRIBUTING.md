# Contributing to ViSiON/3

Contributions are welcome. Open an issue or a pull request; CI runs
`gofmt`, `go vet`, `go test` and `golangci-lint` on every PR.

## Tests for behavior changes

When a code change alters behavior users can see or rely on—including screen
presentation, calculations, command handling, or state changes—add or update a
test that demonstrates the expected behavior. Prefer a unit test for a rule,
calculation, parser, or state transition. Add an integration or terminal
regression test when the change affects how components work together or what a
caller sees and can do. Bug fixes should include a test that fails before the
fix and passes after it.

For menu work, cover the action's outcome, not only that its menu opens. Keep
the test isolated from live users, real file transfers, and external services.
The [terminal regression guide](docs/development/mcp-regression.md) describes
the live test rig and its current coverage.

## Doc comments

Every package and every exported identifier (type, function, method,
constant, variable) must have a Go doc comment. CI enforces this with
revive's `exported` and `package-comments` rules (see `.golangci.yml`); run
`golangci-lint run ./...` locally before pushing.

The linter only checks that a comment exists and starts with the right name.
Reviewers check that it is worth reading.

### Style

We follow the standard [Go doc comment](https://go.dev/doc/comment)
conventions:

- Start with the name being documented: `// Package tosser ...`,
  `// FindProtocol returns ...`, `// NodeStatus is ...`.
- Write full sentences ending with a period. Plain text only: no Markdown
  headers and no `@param`/`@return` tags.
- Put a package comment directly above the `package` clause, with no blank
  line in between. Use a separate `doc.go` when it runs longer than a few lines.
- Document a group of related constants with a comment on the `const (...)`
  block, and add a comment to each value whose meaning isn't obvious.
- For a method that only exists to satisfy an interface, name the interface
  and note anything unusual: `// Deadline implements context.Context; telnet
  sessions have no deadline.`

### What to say

A comment has to tell the reader something the name and signature don't. A
comment like `// Close closes the service.` doesn't count. Depending on the
identifier, cover:

- **Purpose**: what it is for and who calls it.
- **Arguments**: constraints, units, accepted formats, and what nil or empty
  values mean.
- **Results**: what comes back when nothing matches (nil, the zero value, or
  an error), and whether the caller owns or may modify it.
- **Errors**: conditions the caller is expected to handle, and sentinel
  errors to check with `errors.Is`.
- **Side effects**: files written, goroutines started, channels closed, and
  global state touched.
- **Concurrency**: whether it is safe for concurrent use, and which lock the
  caller must or must not hold.
- **Examples**: the input and output formats for parsers and formatters
  (FTN addresses, pipe codes, and so on).

Unexported code doesn't need doc comments, but complex unexported logic
benefits from the same treatment.
