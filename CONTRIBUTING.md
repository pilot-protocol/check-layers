# Contributing

Thanks for your interest in contributing to `check-layers` — Architecture layer-violation linter — reads layers.yaml, runs go list, fails CI on forbidden cross-layer imports.

## Quick start

```bash
git clone https://github.com/pilot-protocol/check-layers.git
cd check-layers
go test -race ./...
```

## Pull requests

1. Open an issue first for non-trivial changes so design can be discussed.
2. Branch off `main`; keep changes focused and self-contained.
3. Tests are required for new behavior; passing CI is required to merge.
4. Coverage should not regress (Codecov reports per-PR delta).
5. Conventional commit style is preferred (`feat:`, `fix:`, `docs:`, `chore:`, …) but not enforced.

## Code of conduct

Be respectful and constructive. Project maintainers will moderate.

## License

By contributing you agree your contributions will be released under the
project's license (AGPL-3.0-or-later — see `LICENSE`).
