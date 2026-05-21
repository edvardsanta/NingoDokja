# Coding Conventions

This document outlines the coding standards and conventions observed across the polyglot codebase.

## Python (dokja_lab, dokja_services, dokja_domain)

- **Naming Conventions:**
  - Functions and variables: `snake_case`
  - Classes: `PascalCase`
  - Adheres to PEP 8 standards.
- **Formatting and Linting:**
  - **Formatter:** `black` with a line length of 88 characters.
  - **Import Sorting:** `isort` configured with the `black` profile.
  - **Linter:** `flake8`.
  - Configuration is primarily managed in `dokja_lab/pyproject.toml` and `dokja_lab/.flake8`.
- **Static Analysis:**
  - **Type Checking:** `mypy` is used for static type analysis to ensure type safety.

## Go (dokja_orch, dokja_legacy, dokja_domain/dokja_book)

- **Naming Conventions:**
  - Exported symbols: `PascalCase`
  - Internal symbols: `camelCase`
  - Adheres to standard Go naming conventions.
- **Formatting:**
  - Uses `gofmt` for consistent code formatting across all Go components.

## TypeScript (dokja_interfaces/discord)

- **Naming Conventions:**
  - Functions and variables: `camelCase`.
- **Runtime:**
  - `tsx` is used for executing TypeScript code directly.

## General Practices

- **Configuration:** Project-wide settings and tool versions are managed via `.tool-versions` and various `pyproject.toml` or `go.mod` files.
- **Documentation:** README files are present in major directories to explain component responsibilities.
