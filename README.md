# gh-stack-tui

A terminal UI for monitoring and driving [gh-stack](https://github.com/github/gh-stack) stacks, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

It covers what `gh stack view` shows, and adds:

- **Live updates.** Local branches are polled every couple of seconds, and PR and CI state is refetched from GitHub every 30 seconds. Loading runs in the background and never blocks input, so you can leave it open as a view of your stack.
- **CI checks per PR**, grouped by workflow. Each workflow expands to its jobs, and re-runs are deduplicated.
- **Stack operations without leaving the TUI:** push, sync, rebase, submit, merge, add, and unstack. Output streams into a panel under the stack. `gh stack modify` runs in the foreground and the TUI comes back when it exits.
- **Vim-style keys throughout.** Arrow keys also work.
- **Catppuccin Mocha** colors, laid out like `gh stack view`.
- **Copying** the hovered file path, commit SHA, branch name, or URL.
- **Review:** select layers of the stack and open their combined commit range in [tuicr](https://github.com/agavra/tuicr), in a new herdr tab, a new tmux window, or inline.

## Usage

```sh
go run . [-C dir] [-stack N] [-interval 30s] [-poll 2s] [-review-cmd 'tuicr -r {range}'] [-review-in auto]
```

The TUI shows the stack containing the checked out branch and follows you when you check out a branch in another stack. On trunk, or with a detached HEAD mid-rebase, it keeps showing the last stack. `[` and `]` switch between stacks and pin the choice until you next check out a branch.

Stack operations run `gh stack` against the checked out stack. When you are viewing a different stack, press `c` on one of its branches first.

## Keys

`j`/`k` move between the branches of the stack, exactly like `gh stack view`. Files, commits, and CI checks open under a branch with `f`, `C`, and `x` (or `l` to open everything). To act on one of them, press `l` again to go into the branch: `j`/`k` then step through its items, and `h` goes back to the branch. Merged branches are shown but skipped.

Stack operations follow the aliases of the `gs` wrapper: `p` push, `s` sync, `P` sync --prune, `r` rebase, `c` checkout, `a` add, `m` modify, and `u` unstack. `R` takes the place of `gs review`.

| Navigate | | Hovered branch or item | |
|---|---|---|---|
| `j`/`k` `↓`/`↑` | prev/next branch, or item once inside | `o` | open in browser (PR, file, commit, workflow run, check) |
| `J`/`K` | prev/next branch, from anywhere | `y` | copy branch / path / SHA / URL |
| `gg`/`G` | top/bottom branch | `Y` | copy every file path in the branch |
| `ctrl+d`/`ctrl+u` | half page | `e` | open file in `$EDITOR` |
| `.` | current branch | `c` | checkout branch |
| `[` `]` | prev/next stack | `M` | merge PR and the PRs below it (asks first) |
| mouse | wheel scrolls, click selects or toggles | `D` | mark draft PR ready for review (or every draft in the selection) |

| Expand | | Stack | |
|---|---|---|---|
| `l` `→` `enter` | open the branch, then go into its items | `p` / `s` / `P` | push / sync / sync --prune |
| `h` `←` | back to the branch, then close it | `S` | submit --auto |
| `f` / `C` / `x` | files / commits / CI checks | `rr` `ru` `rd` | rebase stack / upstack / downstack |
| `z` / `Z` | toggle branch / collapse all | `rc` `ra` | rebase continue / abort |
| | | `a` | add a branch (prompts for a name) |
| **Review** | | `m` | modify (interactive) |
| `space` | mark branch | `u` | unstack (asks first) |
| `v` | visual range | `:` | run any `gh stack …` command |
| `R` | review selection | `ctrl+r` | refresh now |
| | | `!` / `?` / `q` | output panel / help / quit |

### Review

`R` reviews the marked branches, or the visual range, or the hovered branch if nothing is selected. The stack is linear, so the selection becomes a single `base..head` range from the bottom selected layer to the top one. It then runs `--review-cmd` with `{range}` filled in. By default that opens a new herdr tab when running inside herdr (`HERDR_ENV=1`), a new tmux window inside tmux, and otherwise suspends the TUI while the review runs. Use `--review-in` to force one of these.

## Development

```sh
go run .
go test ./...
```

This project uses Nix and direnv for a reproducible development environment.

Configured languages: go.

Run `direnv allow` to activate the development shell and `nix fmt` to format the repository.

## License

Licensed under either of Apache License, Version 2.0 or MIT license at your option.
