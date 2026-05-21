# Testing Strategy

This document describes the testing frameworks, organization, and automation used across the project.

## Python Testing (dokja_lab, dokja_services, dokja_domain)

- **Framework:** `pytest` (version >= 7.4.0).
- **Organization:**
  - Tests are located in `tests/` directories within each service (e.g., `dokja_lab/tests/`).
- **Patterns and Tools:**
  - **Fixtures:** Shared fixtures are managed via `conftest.py`.
  - **Mocking:** `unittest.mock` is the preferred tool for isolation.
  - **Coverage:** `pytest-cov` is used to track and report test coverage, with configurations for both terminal and HTML reports found in `pyproject.toml`.

## Go Testing (dokja_orch, dokja_legacy, dokja_domain/dokja_book)

- **Framework:** Standard `go test` library.
- **Organization:**
  - Test files are co-located with their respective source code files and use the `_test.go` suffix (e.g., `dokja_orch/internal/logger/logger_test.go`).

## TypeScript Testing (dokja_interfaces/discord)

- **Framework:** Native `node:test` runner.
- **Organization:**
  - Tests are located in a dedicated `tests/` directory (e.g., `dokja_interfaces/discord/tests/*.test.ts`).

## Test Automation

- **Orchestration:** A root-level script `scripts/test_all.sh` is used to run the entire test suite across all languages and components.
- **Reporting:** The orchestration script generates a unified report to provide a high-level view of the project's health.
