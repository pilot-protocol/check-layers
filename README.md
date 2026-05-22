# check-layers

Generic Go-module architecture linter. Reads a `layers.yaml` file at the repo root, walks `go list -json ./...`, and exits non-zero on any import that violates the declared layering. Drop it into any Go repo with a `layers.yaml` and it works — nothing in it is project-specific.

## Install

```bash
go install github.com/pilot-protocol/check-layers@latest
```

## Run

From the target repo root:

```bash
check-layers          # P1 + P2 only
check-layers --tests  # also enforce P5 (tests stay within their layer)
```

## Rules

- **P1** — strict downward imports: every Go import must target a layer `≤ source layer - 1`.
- **P2** — single upward interface per layer: when a destination layer declares `public:` in `layers.yaml`, cross-layer imports may only target one of those packages; sibling packages listed in `packages:` but absent from `public:` are layer-internal.
- **P5** — tests stay within their layer (enabled with `--tests`).

Known transitional violations listed under `known_transitional` in `layers.yaml` are reported as warnings, not failures — so the build stays green during refactors even while violations exist.

## layers.yaml

```yaml
layers:
  - id: 0
    name: stdlib
    packages: []
  - id: 1
    name: protocol
    packages:
      - github.com/example/proj/pkg/protocol
    public:
      - github.com/example/proj/pkg/protocol
  - id: 2
    name: driver
    packages:
      - github.com/example/proj/pkg/driver
    public:
      - github.com/example/proj/pkg/driver
known_transitional:
  - from: github.com/example/proj/internal/legacy
    to:   github.com/example/proj/internal/newer
```
