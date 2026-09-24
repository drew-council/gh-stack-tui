package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/git"
	"github.com/drew-council/gh-stack-tui/internal/tui"
)

func main() {
	var opts tui.Options
	dir := flag.String("C", ".", "run as if started in `dir`")
	flag.DurationVar(
		&opts.RemoteInterval,
		"interval",
		30*time.Second,
		"how often to refresh PR and CI state from GitHub",
	)
	flag.DurationVar(
		&opts.PollInterval,
		"poll",
		2*time.Second,
		"how often to check local branches for changes",
	)
	flag.IntVar(
		&opts.StackNumber,
		"stack",
		0,
		"stack `number` to show at startup (default: the checked out stack)",
	)
	flag.StringVar(
		&opts.ReviewCmd,
		"review-cmd",
		"tuicr -r {range}",
		"review command; {range} becomes base..head",
	)
	flag.StringVar(
		&opts.ReviewIn,
		"review-in",
		"auto",
		"where reviews open: auto, herdr, tmux, or inline",
	)
	flag.Parse()

	switch opts.ReviewIn {
	case "auto", "herdr", "tmux", "inline":
	default:
		fmt.Fprintf(os.Stderr, "error: --review-in must be auto, herdr, tmux, or inline\n")
		os.Exit(2)
	}
	opts.RemoteInterval = max(opts.RemoteInterval, 5*time.Second)
	opts.PollInterval = max(opts.PollInterval, 250*time.Millisecond)

	repo, err := git.Open(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if _, err := tea.NewProgram(tui.New(repo, opts)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
