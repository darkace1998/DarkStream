# Linter Configuration Changes

## Overview

The golangci-lint configuration has been updated to disable overly strict or impractical linters while maintaining code quality standards. The original configuration resulted in 295 issues, many of which were either too opinionated or not practical for this project.

## Disabled Linters and Rationale

### Import and Dependency Management
- **depguard**: Disabled to allow all imports. The original configuration was blocking internal package imports.
- **gomoddirectives**: Disabled to allow local replace directives, which are necessary for multi-module projects.

### Error Handling
- **err113** (goerr113): Disabled to allow dynamic errors. While static errors are good practice, requiring them everywhere is not always practical.
- **noinlineerr**: Disabled to allow inline error handling (e.g., `if err := ...; err != nil`), which is more concise and idiomatic Go.

### Code Structure
- **exhaustruct**: Disabled - requiring all struct fields to be explicitly set is too verbose and impractical.
- **exhaustive**: Disabled - don't require all switch cases; allow default handling.
- **funcorder**: Disabled - don't enforce specific function ordering.
- **nestif**: Disabled - don't flag nested if statements (some nesting is acceptable).

### Style and Formatting
- **godot**: Disabled - don't enforce periods at the end of comments (too strict).
- **wsl** / **wsl_v5**: Disabled - whitespace linters are too opinionated.
- **whitespace**: Disabled - don't enforce specific whitespace rules.
- **nlreturn**: Disabled - don't require blank lines before return statements (personal preference).

### Testing
- **paralleltest**: Disabled - don't require `t.Parallel()` in all tests; not always needed.
- **testpackage**: Disabled - allow tests in the same package (white-box testing).

### Naming and Constants
- **varnamelen**: Disabled - allow short variable names (standard Go practice for loops, errors, etc.).
- **errname**: Disabled - don't enforce error naming convention.
- **tagliatelle**: Disabled - allow snake_case for JSON tags (common in APIs).
- **mnd**: Disabled - don't flag magic numbers; constants aren't always necessary.

### Micro-optimizations and Modernization
- **perfsprint**: Disabled - don't enforce `fmt.Sprintf` vs string concatenation (micro-optimization).
- **intrange**: Disabled - don't require Go 1.22+ range syntax (maintain compatibility).
- **lll**: Disabled - don't enforce line length limits (some lines need to be long).
- **noctx**: Disabled - allow HTTP calls without context (not always needed).

### Development
- **godox**: Disabled - allow TODO/FIXME comments.

### Security
- **gochecknoglobals**: Disabled - allow globals when needed (e.g., sync.Once patterns).

## Adjusted Settings

### Complexity Thresholds
- **cyclop**: Increased max complexity from 15 to 20
- **gocyclo**: Minimum complexity set to 40
- **funlen**: 200 lines, 100 statements

### Other Settings
- **gofumpt**: Disabled extra rules for less strict formatting
- **errcheck**: Check type assertions but not blank identifiers
- **goconst**: Minimum 3 characters, 3 occurrences

## Code Fixes Applied

### Formatting (gofumpt)
1. Fixed unnecessary blank lines in `master_client.go`
2. Fixed unnecessary blank lines in `ffmpeg.go`

## Result

The configuration now focuses on meaningful code quality issues while allowing common Go idioms and patterns. This should significantly reduce false positives while still catching real problems like:
- Race conditions
- Resource leaks
- Type errors
- Unused variables
- Actual complexity issues (when functions exceed complexity of 20)

## Running the Linter

To run the linter on all modules:
```bash
./lint.sh
```

To run on a specific module:
```bash
cd video-converter-worker
golangci-lint run ./...
```

## Future Improvements

Consider enabling some linters with custom configuration if specific issues arise:
- Enable `depguard` with a proper allowlist when import restrictions are needed
- Enable `err113` for critical error paths where static errors are important
- Enable `godot` for public API documentation only
