# AGENTS.md

A Bubble Tea TUI for gh-stack. See [README.md](README.md) for features, keys, and development setup.

Agents MAY update this file without asking. When a change makes something here wrong, fix it in the same change. When you learn something a future agent would need, such as a workflow, a pitfall, or a convention, add it. Keep entries short.

## Layout

- `internal/tui`: model, key handling, and rendering.
- `internal/github`: GraphQL queries and PR, CI, and reviewer state.
- `internal/cache`: SQLite cache of GitHub results, generated with sqlc.
- `internal/git`, `internal/stack`: local repository and stack state.
- `internal/demo`: made-up stacks and PRs in every state, for `ghst demo` and the rendering tests.

The model reads stacks through the `Local` interface and PRs through `Remote` (`internal/tui/sources.go`). `stack.Source` and `github.Client` are the real ones and `internal/demo` the fake ones. The model MUST NOT check for the demo. Add a method to an interface instead.

## Testing

Run `go test ./...` before finishing a change.

Rendering tests SHOULD start from `demoModel` in `internal/tui/render_test.go`, which loads the demo stacks through the real loading path on a fixed clock. Tests that assert the whole screen MUST use `assertScreen` in `internal/tui/model_test.go`. It compares the rendered screen, without color, to `internal/tui/testdata/<TestName>.golden`. Tests that check for one piece of text SHOULD keep using `strings.Contains`.

When a UI change is intended, rewrite the golden files:

```sh
go test ./internal/tui -update
```

Then read the `git diff` of `testdata` and confirm every change is intended. Never run `-update` just to make a failing test pass. Pass `-update` to `internal/tui` only; packages without golden files reject the flag.

### Looking at the UI

Goldens MUST NOT be the only check of a visual change; look at it in color too. `scripts/shot.nu` runs `ghst demo` in a private tmux server and renders the screen to PNG with freeze. It needs the dev shell, which provides tmux, freeze, and the JetBrains Mono Nerd Font in `$GHST_SHOT_FONT`.

```sh
nix develop -c nu scripts/shot.nu start 80 50
nix develop -c nu scripts/shot.nu keys j l Space
nix develop -c nu scripts/shot.nu shot cursor
nix develop -c nu scripts/shot.nu stop
```

`shot` writes `tmp/shots/<name>.png`, which can be read as an image, and prints its path. `text` prints the screen as plain text and `resize` changes its size. Keys are tmux key names. freeze has no font fallback, so a glyph the font lacks, such as `↻`, shows as a box in shots but not in a terminal.

People can run `go run . demo` themselves.

## Generated and pinned files

- After changing `internal/cache/queries.sql` or `internal/cache/schema.sql`, run `sqlc generate`. `nix flake check` fails when the generated code is stale.
- After changing `go.mod` dependencies, update `vendorHash` in `flake.nix`. Set it to `""`, run `nix build`, and copy the `got:` hash from the error. Then delete the `result` symlink the build leaves behind.
- Format with `nix fmt`.
