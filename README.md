# gh-stack-tui

Two terminal UIs for working with [gh-stack](https://github.com/github/gh-stack) stacks, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea):

- **`ghst`** monitors and drives your own stack. It is made for a narrow pane next to your editor.
- **`ghrv`** shows the pull requests that wait on your review, with the stacks they are part of, and opens them in [tuicr](https://github.com/agavra/tuicr). It is made for a full screen. See [ghrv](#ghrv-the-review-queue).

Both are built from the same packages, look alike, and share a cache.

## ghst

`ghst` covers what `gh stack view` shows, and adds:

- **Live updates.** Local branches are polled every couple of seconds, and PR and CI state is refetched from GitHub every 30 seconds. Loading runs in the background and never blocks input, so you can leave it open as a view of your stack.
- **CI checks per PR**, grouped by workflow. Each workflow expands to its jobs, and re-runs are deduplicated.
- **Stack operations without leaving the TUI:** push, sync, rebase, submit, merge, add, and unstack. Output streams into a panel under the stack. Commands started while one is running queue up and run in order; a failure or `ctrl+c` drops the rest. `gh stack modify` runs in the foreground and the TUI comes back when it exits.
- **Vim-style keys throughout.** Arrow keys also work.
- **Catppuccin Mocha** colors, with the stack drawn as a tree like `gh stack view`.
- **Copying** the hovered file path, commit SHA, branch name, or URL.
- **Review:** select layers of the stack and open their combined commit range in [tuicr](https://github.com/agavra/tuicr), or a single layer's PR, in a new herdr tab, a new tmux window, or inline. Without tuicr, the changes open on GitHub.

### Usage

```sh
go run ./cmd/ghst [-C dir] [--stack N] [--interval 30s] [--poll 2s] [--review-cmd 'tuicr -r {range}'] [--review-pr-cmd 'tuicr pr {pr}'] [--review-in auto]
```

`go run ./cmd/ghst demo` shows made-up stacks instead of a repository's, to try the UI. Commands still run, in an empty temporary directory, so they fail without touching anything.

The TUI shows the stack containing the checked out branch and follows you when you check out a branch in another stack. On trunk, or with a detached HEAD mid-rebase, it keeps showing the last stack. `[` and `]` switch between stacks and pin the choice until you next check out a branch.

Stack operations run `gh stack` against the checked out stack. When you are viewing a different stack, press `c` on one of its branches first.

### Reading the stack

Each branch is a block on the tree, top of the stack first. The left column reads like a sentence: the branch, then its PR number, state, and title, then the branch's own state, such as a rebase or conflicts, followed by where review stands, such as the review decision and unresolved threads. The right column holds CI, then the diff. Every branch that has not merged takes three lines, however long its title. Under them are the branch's files, commits, and checks, each of which opens on its own.

The hovered branch is shaded whole, down to the gap under it. When the cursor is on one of its items, that row is shaded brighter. Mauve means selected for review and nothing else: selected branches are tinted mauve, the one under the cursor brighter, and the stack's border turns mauve in visual mode. Blue marks the checked out branch, and peach anything that needs attention.

| Mark | Meaning |
|---|---|
| `●` `○` | checked out branch, other branch |
| `◆` `◇` | selected for review: the checked out branch, another branch |
| `◎` `✓` | in the merge queue, merged |
| `┊` | needs a rebase onto its parent |

`?` shows these marks too.

### Keys

`j`/`k` move between the branches of the stack, exactly like `gh stack view`. `l` goes one level in and `h` one level out. On a branch, `l` moves onto its files, commits, and checks, and `j`/`k` step between them. `l` on one of them opens it and moves onto its first entry; a workflow opens onto its jobs the same way. `h` closes what the cursor is in and moves onto it, and from a section goes back to the branch. `f`, `C`, and `x` open or close a branch's files, commits, and checks from anywhere in it. Merged branches are shown, one line each, but skipped.

Stack operations follow the aliases of the `gs` wrapper: `p` push, `s` sync, `P` sync --prune, `r` rebase, `c` checkout, `a` add, `m` modify, and `u` unstack. `R` takes the place of `gs review`.

A selection of marked branches, or the visual range, is used by `R`, `D`, `A`, `o`, `y`, and `Y`. `o` opens each selected branch's PR, `y` copies the branch names one per line, and `Y` copies the selected branches' unique changed file paths one per line. Other commands use the hovered branch or item.

| Navigate | | Hovered branch or item | |
|---|---|---|---|
| `j`/`k` `↓`/`↑` | prev/next branch, or item once inside | `o` | open PR(s) or the hovered file, commit, workflow run, or check in the browser |
| `J`/`K` | prev/next branch, from anywhere | `y` | copy branch name(s) / path / SHA / URL |
| `gg`/`G` | top/bottom branch | `Y` | copy every changed file path (selection or branch) |
| `ctrl+d`/`ctrl+u` | half page | `e` | open file in `$EDITOR` |
| `.` | current branch | `c` | checkout branch |
| `[` `]` | prev/next stack | `M` | merge PR and the PRs below it (asks first) |
| mouse | wheel scrolls, click selects or toggles | `D` | mark draft PRs ready (asks for reviewers), or convert open PRs to draft (asks first), on the hovered PR or selection |
| | | `A` | ask for reviewers on the hovered PR or selection, whether it is a draft or ready for review |

| Expand | | Stack | |
|---|---|---|---|
| `l` `→` `enter` | one level in: onto the sections, then open one and onto its first entry | `p` / `s` / `P` | push / sync / sync --prune |
| `h` `←` | one level out: close it and move onto it, then back to the branch | `S` | submit --auto |
| `f` / `C` / `x` | files / commits / CI checks | `rr` `ru` `rd` | rebase stack / upstack / downstack |
| `z` / `Z` | toggle branch / collapse all | `rc` `ra` | rebase continue / abort |
| | | `a` | add a branch (prompts for a name) |
| **Review** | | `m` | modify (interactive) |
| `space` | mark branch | `u` | unstack (asks first) |
| `v` | visual range | `:` | run any `gh stack …` command |
| `R` | review selection | `ctrl+r` | refresh now |
| | | `!` / `?` / `q` | output panel / help / quit |
| | | `ctrl+c` | cancel the running command and the queue, else quit |

#### Review

`R` reviews the marked branches, or the visual range, or the hovered branch if nothing is selected. The stack is linear, so the selection becomes a single `base..head` range from the bottom selected layer to the top one. It then runs `--review-cmd` with `{range}` filled in. When the selection is a single branch with an open PR, whether draft or ready, `--review-pr-cmd` runs instead with `{pr}` set to the PR number, so tuicr reviews the PR and comments can be submitted to it. Pass `--review-pr-cmd ''` to always review the range. By default that opens a new herdr tab when running inside herdr (`HERDR_ENV=1`), a new tmux window inside tmux, and otherwise suspends the TUI while the review runs. When the review command's program is not installed, the review opens on GitHub instead: the PR's files for a single branch with an open PR, and otherwise a comparison of the bottom layer's parent with the top layer, as pushed. Use `--review-in` to force one of these, or `--review-in browser` to always use GitHub.

#### Reviewers

When `D` marks a draft ready and nobody has been asked to review it in person (at most teams, such as the CODEOWNERS teams GitHub requests on its own), a picker asks who should review it first. Type to fuzzy filter by login or name, `tab` picks several people, and `enter` requests the picked people, or the hovered one if nobody is picked, then marks the PR ready. `ctrl+s` marks it ready without asking anyone, and `esc` leaves it a draft.

A selection with several PRs is walked one PR at a time. Each prompt names the PR's branch, number, and title, and skipping one moves on to the next. When the selection has no drafts, `D` converts its PRs back to drafts, asking once per PR; answering no moves on to the next.

`A` opens the same picker for the hovered PR or selection without changing its draft state, so reviewers can be added by hand. It works on drafts and ready PRs alike.

The list puts GitHub's suggested reviewers for the PR first. Next come the people your last 50 PRs in the repository went to, most often first. Everyone else follows by login. The candidates load once in the background after the first GitHub refresh, so the picker opens instantly. GitHub's suggestions load in the background for drafts that need reviewers, and load again when a draft's head commit changes.

#### Cache

GitHub results are kept in a SQLite database under the user cache directory (`~/.cache/gh-stack-tui` on Linux), shared by every running `ghst` and `ghrv`. At startup the TUI shows the cached PRs, with `cached 5m ago` in the header, until GitHub answers. The reviewer list is reused for 24 hours. Rows unused for 30 days are deleted, and a build that changes what is stored starts a new cache file and deletes the old one. Install `ghst` and `ghrv` from the same commit, so they agree on the file.

## ghrv, the review queue

`ghrv` lists the open pull requests of a repository that wait on your review, and opens them in [tuicr](https://github.com/agavra/tuicr) tabs.

```sh
go run ./cmd/ghrv [-C dir] [--team slug]... [--drafts] [--bots] [--interval 30s] [--poll 2s] [--review-cmd 'tuicr pr {url}'] [--review-in auto]
```

`go run ./cmd/ghrv demo` shows a made-up queue, to try the UI. Its tabs are made up too, so reviews open nothing.

The queue holds the pull requests of the working directory's repository where review is requested from you by name or from one of your teams. Your own pull requests are left out. `--team` takes only the requests to that team, plus those to you by name; repeat it for several teams.

The queue is refetched every 30 seconds and never blocks input. A new request is announced on the status line. At startup the last known queue shows at once, with `cached 5m ago` in the header, until GitHub answers.

### Reading the queue

The left panel is the queue and the right one describes the row under the cursor. On a screen narrower than 132 columns only the queue shows.

Pull requests are grouped by stack, as GitHub records stacks, and ordered by author and then by age, so rows stay put between refreshes. A stack that holds a requested pull request is shown whole, top first like `gh stack view`, with its trunk at the bottom. Its header says how many of its pull requests are for you and which one to start at: the lowest.

Pull requests that wait on you are in bright ink. The rest of a stack is muted: it is there so you know what a pull request builds on and what builds on it. Three or more muted pull requests in a row fold into one line that counts them by state. `l` unfolds the stack and `h` folds it again.

A pull request outside any stack whose base is not trunk starts with `on #12`, the pull request it builds on, or `on` and the branch.

| Column | Meaning |
|---|---|
| `YOU` | what the pull request needs from you; see below |
| `VIA` | who was asked: `you` by name, else your teams, without the part of their names they share |
| `REVS` | other reviewers: `✓1` approved, `✗1` requested changes |
| `CI` | `✓` passed, `✗` failed, `●` running, `⊘` cancelled |
| `DIFF` | lines added and deleted, without generated files |
| `ASKED` | how long ago you were asked, in peach after three days |

| `YOU` | Meaning |
|---|---|
| `review` | you were asked and have not reviewed |
| `re-review` | you reviewed, and were asked again, or commits were pushed since you commented or requested changes |
| `waiting` | you commented or requested changes and nothing changed since: the author's turn |
| `approved` | you approved |
| `draft`, `bot`, `merged`, `closed` | the pull request's own state |

Line and file counts leave out generated files, the ones your checkout's `.gitattributes` tags `linguist-generated`, `gitlab-generated`, or `generated`, as `ghst` does. A pull request shows GitHub's totals until its files have loaded, and keeps them when it changes more than 1000 files.

A `▎` in the first column marks a pull request that a tab is reviewing. Mauve means selected, as in `ghst`.

Descriptions are rendered from Markdown with [glamour](https://github.com/charmbracelet/glamour), the renderer of `glow`.

The right panel answers, in order: what the pull request is, what it wants from you and who asked, what you said last and whether it changed since, who else reviewed, which checks fail, what is above and below it in its stack, its description, and its files. It loads when the cursor reaches a pull request, and ahead of time for the ones that wait on you.

### Hidden pull requests

Some requests are not yours to act on now, and the queue leaves them out: drafts, pull requests opened by bots, the ones you approved while a team's request stays open, and the ones waiting on their author. The bottom border counts them. `.` shows them, muted, and `--drafts` and `--bots` keep those two kinds in the queue for good.

### Keys

| Navigate | | Review | |
|---|---|---|---|
| `j`/`k` `↓`/`↑` | next/previous row | `enter` `R` | open review tabs behind the queue, or go to the pull request's tab |
| `[` `]` | previous/next stack or author | `space` | mark a pull request; on a stack's header, its pull requests for you |
| `gg`/`G` | top/bottom | `v` | visual range |
| `ctrl+d`/`ctrl+u` | half page | `o` | open in the browser |
| `J`/`K` | scroll the right panel | `y` / `Y` | copy URL(s) / branch name(s) |
| `l` `→` / `h` `←` | unfold/fold the stack | `.` | show hidden pull requests |
| mouse | wheel scrolls either panel, click moves the cursor | `/` | filter by author, title, number, branch, team, or label |
| | | `ctrl+r` | refresh now |
| | | `esc` | clear the selection, then the filter |
| | | `?` / `q` | help / quit |

`o`, `y`, and `Y` apply to the selection, else to the pull request under the cursor, else to the hovered stack's pull requests for you.

### Reviewing

`enter` on a pull request runs `--review-cmd` in a new tab named after it, such as `#123`. `{url}` becomes the pull request's URL and `{pr}` its number. The tab opens behind the queue, which keeps the focus, so you can keep picking. When a tab with that name is already open, `enter` goes to it instead of opening another.

`enter` with a selection, or on a stack's header, opens every pull request the same way. Tabs open in the order to review them: groups as listed, and each stack from the bottom up. Pull requests that already have a tab are skipped.

Tabs are herdr tabs inside herdr (`HERDR_ENV=1`) and tmux windows inside tmux. Elsewhere, or with `--review-in inline`, the queue is suspended while the reviews run one after another. When the review command's program is not installed, or with `--review-in browser`, the PRs' files open on GitHub instead. Open tabs are checked every two seconds, so a tab you close loses its mark.

Review commands run at the top of the repository.

## Development

```sh
go run ./cmd/ghst
go run ./cmd/ghrv
go test ./...
```

`go run ./cmd/ghst demo` and `go run ./cmd/ghrv demo` show the UIs with made-up data. Rendering tests compare the screen, without color, to golden files in `internal/stacktui/testdata` and `internal/reviewtui/testdata`. After an intended UI change, rewrite them and review the diff before committing:

```sh
go test ./internal/stacktui ./internal/reviewtui -update
```

Pass `-update` to those two packages only. Packages that do not use golden files reject the flag.

`nix build .#ghst` and `nix build .#ghrv` build the programs.

The cache's queries are in `internal/cache/queries.sql` and its schema in `internal/cache/schema.sql`. Run `sqlc generate` after changing either; `nix flake check` fails when the generated code is out of date.

This project uses Nix and direnv for a reproducible development environment.

Configured languages: go.

Run `direnv allow` to activate the development shell and `nix fmt` to format the repository.

## License

Licensed under either of Apache License, Version 2.0 or MIT license at your option.
