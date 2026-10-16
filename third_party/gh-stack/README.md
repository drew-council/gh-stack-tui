# gh-stack shim

`shim/ghstack` gives [internal/stack](../../internal/stack/file.go) gh-stack's own reading of the stacks it records for a repository, from [github/gh-stack](https://github.com/github/gh-stack).

- gh-stack keeps its packages under `internal/`.
- Go lets any package whose import path starts with `github.com/github/gh-stack` import those packages, from any module.
- The shim's module path is `github.com/github/gh-stack/shim/ghstack`, so it can import gh-stack.

## API

The shim is a small facade, not a re-export.

- `Load` reads the catalog in Git's common directory, first migrating catalogs older gh-stack versions kept per worktree, as gh-stack's read-only commands do.
- `CatalogPath` names the file `Load` reads, so the TUI can poll it for changes.
- Each branch carries its `Parent` from gh-stack's `ActiveBaseBranch`, rather than the TUI working it out.
- Everything the shim returns is its own type, so it's the only code a gh-stack upgrade can break.
  - Add what the TUI needs here, rather than exposing gh-stack's types.

## Wiring

- `go.mod` requires the shim and replaces it with this directory, so the `go` tool and gopls resolve it here.
- `go test ./...` from the repository root does not reach this module. Run its tests from `shim/ghstack`.

## Upgrading gh-stack

gh-stack has no stable Go API, so an upgrade may need shim changes. Keep it on the release you have installed: the shim and `gh stack` read and write the same files.

1. In `shim/ghstack`, run `go get github.com/github/gh-stack@<version>` and `go mod tidy`.
2. Run `go mod tidy` at the repository root, then update `vendorHash` in `flake.nix`.
3. Fix what doesn't compile in `shim/ghstack`, then run its tests there.
4. Run `go test ./...` at the repository root.
