# AGENTS.md

Two Bubble Tea TUIs for gh-stack: `ghst` for your own stack, and `ghrv` for the pull requests that wait on your review. See [README.md](README.md) for features, keys, and development setup.

Agents MAY update this file without asking. When a change makes something here wrong, fix it in the same change. When you learn something a future agent would need, such as a workflow, a pitfall, or a convention, add it. Keep entries short.

## Layout

- `cmd/ghst`, `cmd/ghrv`: the programs. They parse flags and wire the packages below; nothing else belongs there.
- `internal/stacktui`: `ghst`'s model, key handling, and rendering.
- `internal/reviewtui`: `ghrv`'s model, key handling, and rendering.
- `internal/ui`: what the TUIs share: the palette and styles, panels, key hints, the help screen, and the clipboard and browser commands. `internal/ui/uitest` compares screens to golden files.
- `internal/desktop`: the clipboard, the browser, and herdr and tmux tabs.
- `internal/github`: GraphQL and REST queries: PR, CI, and reviewer state for `ghst`, the review queue and GitHub's stacks for `ghrv`.
- `internal/review`: what each PR of a review queue needs from the viewer, and how the queue groups into stacks. No I/O.
- `internal/cache`: SQLite cache of GitHub results, generated with sqlc. Both programs use the same file.
- `internal/git`, `internal/stack`: local repository and stack state, for `ghst`.
- `internal/demo`: made-up stacks, a made-up review queue, and made-up tabs, for `ghst demo`, `ghrv demo`, and the rendering tests.

Code both TUIs need goes in `internal/ui` or `internal/desktop`, not in one TUI's package. A TUI package MUST NOT import the other.

`ghst`'s model reads stacks through the `Local` interface and PRs through `Remote` (`internal/stacktui/sources.go`). `ghrv`'s model reads its queue through its own `Remote` (`internal/reviewtui/sources.go`) and opens reviews through `desktop.Tabs`. `stack.Source`, `github.Client`, and the herdr and tmux tabs are the real ones and `internal/demo` the fake ones. A model MUST NOT check for the demo. Add a method to an interface instead.

GitHub does not say which files are generated. `ghrv` asks the checkout through `Local`, as `ghst` does through `git.Repo`, and flags the files when their details load. Line and file counts on screen MUST come from `Model.size`, not from the PR's own totals.

`ghrv` learns stacks from GitHub's stacks API, not from the local gh-stack file: the stacks are other people's. The queue's query is cheap on purpose, and `FetchReviewDetails` fetches the costly parts per PR, only when a PR's `Version` changes.

## Testing

Run `go test ./...` before finishing a change.

Rendering tests SHOULD start from `demoModel` in the TUI's `render_test.go`, which loads the demo data through the real loading path on a fixed clock. Tests that assert the whole screen MUST use the package's `assertScreen`, which calls `uitest.AssertScreen`. It compares the rendered screen, without color, to `testdata/<TestName>.golden` in the TUI's package. Tests that check for one piece of text SHOULD keep using `strings.Contains`.

When a UI change is intended, rewrite the golden files:

```sh
go test ./internal/stacktui ./internal/reviewtui -update
```

Then read the `git diff` of `testdata` and confirm every change is intended. Never run `-update` just to make a failing test pass. Pass `-update` to those two packages only; packages without golden files reject the flag. A change to `internal/ui` can change both TUIs' goldens.

Goldens have no color, so each line starts with a mark for its background: `█` the cursor, `░` the rest of the hovered branch or stack, `▓` the cursor on a selected row, `▒` a selected row. Which color a row gets is decided in one place per TUI, the comment above `rowBg` in its `view.go`; keep it the single source.

### Looking at the UI

Goldens MUST NOT be the only check of a visual change; look at it in color too. `scripts/shot.nu` runs `ghst demo`, or `ghrv demo` with `--bin ghrv`, in a private tmux server and renders the screen to PNG with freeze. It needs the dev shell, which provides tmux, freeze, and the JetBrains Mono Nerd Font in `$GHST_SHOT_FONT`.

```sh
nix develop -c nu scripts/shot.nu start 80 50
nix develop -c nu scripts/shot.nu keys j l Space
nix develop -c nu scripts/shot.nu shot cursor
nix develop -c nu scripts/shot.nu stop
```

`ghrv` is made for a full screen, so start it wide: `nix develop -c nu scripts/shot.nu start 200 50 --bin ghrv`. `start` also takes `--args` to run something other than the demo and `--vars` to set environment variables for it.

`shot` writes `tmp/shots/<name>.png`, which can be read as an image, and prints its path. `text` prints the screen as plain text and `resize` changes its size. Keys are tmux key names. freeze has no font fallback, so a glyph the font lacks, such as `↻`, shows as a box in shots but not in a terminal. freeze also draws background colors a little off, so padding around a filled label can look uneven in shots. Check the escape codes before fixing spacing seen only in a shot.

People can run `go run ./cmd/ghst demo` and `go run ./cmd/ghrv demo` themselves.

## Generated and pinned files

- After changing `internal/cache/queries.sql` or `internal/cache/schema.sql`, run `sqlc generate`. `nix flake check` fails when the generated code is stale.
- `flake.nix` builds every program under `cmd` once, in `programs`, and gives each its own package. A new program needs a line in `packages`.
- After changing `go.mod` dependencies, update `vendorHash` in `flake.nix`. Set it to `""`, run `nix build`, and copy the `got:` hash from the error. Then delete the `result` symlink the build leaves behind.
- Format with `nix fmt`.
