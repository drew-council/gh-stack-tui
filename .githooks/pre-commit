#!/usr/bin/env nu

if (which nix | is-empty) {
    print "error: nix is not installed, cannot `nix fmt`"
    exit 1
}

let staged_files = (
    git diff --cached --name-only --diff-filter=ACMR
    | lines
    | where { |line| (not ($line | str contains "lock")) }
)

if ($staged_files | is-empty) {
    exit 0
}

print "running nix fmt..."
try {
    nix fmt ...$staged_files
} catch {
    print $"(ansi red)error: nix fmt formatting failed.(ansi reset)"
    exit 1
}

git add ...$staged_files

let head = (do { git rev-parse --verify HEAD } | complete)
if ($head.exit_code == 0) {
    let lock_log = (git log -1 --format="%ad" -- flake.lock)
    if (not ($lock_log | is-empty)) {
        let time_since_update = (date now) - ($lock_log | into datetime)
        if ($time_since_update > 1.wk) {
            print $"(ansi yellow)warning: flake.lock is out of date.(ansi reset)"
        }
    }
}

print "formatting complete"
