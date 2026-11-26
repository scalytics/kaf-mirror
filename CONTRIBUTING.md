# Contributing to kaf-mirror

Thanks for helping improve **kaf-mirror**. This guide covers how to propose changes and what we expect in pull requests.

## How to contribute
- **Discuss first**: For significant changes, open an issue to align on scope and design.
- **Branches & PRs**: Fork/branch from `main`, keep commits focused, and include a clear PR description referencing any related issues.
- **Code style**: Follow idiomatic Go. Run `go fmt` on touched files and keep dependencies tidy.
- **Tests**: Add or update tests for any behavioral change. Run `go test ./...` before submitting. `make test` and `make test-smoke` also run `node --test tests/web/*.test.cjs`, so those targets need Node.js.
- **Documentation**: Update README/config samples/CLI help when behavior or flags change.

## Licensing
- The project is released under the **Apache License 2.0** (see `LICENSE`).
- Add the Apache 2.0 header to new Go files:  
  `// Copyright 2025 Scalytics, Inc. and Scalytics Europe, LTD`  
  `// Licensed under the Apache License, Version 2.0 (the "License"); ...`

## Opening a pull request
- Describe the problem and solution, along with any trade-offs.
- Note testing performed (`go test ./...`, manual steps, etc.).
- Avoid committing built artifacts or secrets; keep PRs source-only.

## Development quickstart
```bash
go fmt ./...
go test ./...
```

`make test-smoke` runs the web smoke tests and then `go test ./...`. The web tests need Node.js (`node --test`). `make test` runs that smoke target.

If you need help or review, mention maintainers in the PR and link to the related issue or discussion.
