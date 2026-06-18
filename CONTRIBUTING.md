# Contributing to DarkStream

Thank you for your interest in contributing to DarkStream! We welcome contributions from the community. This document outlines the process and guidelines for contributing.

## Development Setup

DarkStream is built using Go. The project uses a Go workspace (`go.work`) encompassing several modules: `video-converter-cli`, `video-converter-common`, `video-converter-master`, and `video-converter-worker`.

### Prerequisites

1.  **Go 1.24+**: Ensure you have Go version 1.24 or later installed.
2.  **Git**: For version control.

### Cloning and Setup

```bash
# Clone the repository
git clone https://github.com/your-org/darkstream.git
cd darkstream

# The go.work file is already set up to include all necessary modules.
# You can verify everything builds correctly:
go build ./...
```

## Running Tests

We maintain a comprehensive test suite. Before submitting any changes, you must ensure that all tests pass.

### Running Race Detector Tests

To run the race detector locally across all modules, use the included helper script. This script handles permissions and runs tests safely:

```bash
./scripts/run-race-tests.sh
```

You can also run tests for a single module:

```bash
./scripts/run-race-tests.sh video-converter-worker
```

This script will also generate coverage profiles.

## Formatting and Linting

We enforce strict coding standards using `golangci-lint`.

1.  Before committing, run the included linter script at the root of the repository:

```bash
./lint.sh
```

Ensure that this script runs without any errors. Please see `LINTER_CONFIGURATION.md` for information on our linter setup and rules.

## Pull Request Process

1.  **Fork** the repository and create a new branch for your feature or bug fix.
2.  **Make your changes**, ensuring they follow the coding style and conventions.
3.  **Write or update tests** to cover your changes.
4.  Run `./scripts/run-race-tests.sh` to confirm all tests pass.
5.  Run `./lint.sh` to ensure code quality.
6.  Update documentation (like `README.md` or API docs) if your changes impact them.
7.  Submit a Pull Request with a clear title and description explaining:
    *   **What** you changed
    *   **Why** you made the change
    *   **How** you verified it

## Getting Help

If you have questions or need assistance, feel free to open an issue or reach out to the maintainers.
