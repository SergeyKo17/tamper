# Contributing to Tamper

## Getting Started

1. Fork the repository
2. Clone your fork
3. Create a feature branch from `main`
4. Make your changes
5. Open a pull request

## Branch Naming

Use prefixed branch names:

- `feat/short-description` — new feature
- `fix/short-description` — bug fix
- `refactor/short-description` — code restructuring without behavior change
- `docs/short-description` — documentation only
- `test/short-description` — test additions or fixes
- `chore/short-description` — CI, dependencies, tooling

## Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

<optional body>
```

**Types:** `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `ci`, `build`

**Scope** is optional. When used, it should name the package or area affected: `config`, `proxy`, `fault`, `cmd`.

Examples:
```
feat(config): add hot reload via fsnotify
fix(proxy): handle nil response from target
refactor: extract fault injection into separate package
docs: add usage examples to README
```

Rules:
- Use imperative mood: "add feature", not "added feature"
- Keep the first line under 72 characters
- Explain **why**, not what, in the body when needed

## Code Style

### Go Formatting

- Run `golangci-lint run` before committing. CI will reject unlinted code.
- Follow [Effective Go](https://go.dev/doc/effective_go) conventions.

### Error Handling

Wrap errors with context using `fmt.Errorf`:

```go
return fmt.Errorf("read config: %w", err)
```

The prefix should describe the action that failed, in lowercase, without a trailing colon.

### Comments

- All exported types, functions, and methods must have a godoc comment.
- Start the comment with the identifier name: `// Watch starts a file watcher...`
- Do not comment unexported code unless the reason behind it is non-obvious.
- Do not write comments that restate what the code does.

### Naming

- Use Go conventions: `MixedCaps`, not `snake_case`.
- Acronyms are all caps: `HTTP`, `gRPC`, `URL`.
- Keep names short in small scopes, descriptive in large scopes.

## Testing

- Run tests before submitting: `go test ./...`
- New features must include tests.
- Bug fixes should include a regression test when practical.

## Pull Requests

- Keep PRs focused — one logical change per PR.
- PR title follows the same format as commit messages.
- Describe what changed and why in the PR body.
- Ensure all tests pass before requesting review.

## Reporting Bugs

Open an issue with:
- What you did (steps to reproduce)
- What you expected
- What happened instead
- Tamper version and Go version
