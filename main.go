package main

import (
	"errors"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/drew-council/gh-stack-tui/internal/cache"
	"github.com/drew-council/gh-stack-tui/internal/demo"
	"github.com/drew-council/gh-stack-tui/internal/git"
	"github.com/drew-council/gh-stack-tui/internal/stack"
	"github.com/drew-council/gh-stack-tui/internal/tui"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var opts tui.Options
	var dir string
	cmd := &cobra.Command{
		Use:          "ghst",
		Short:        "A terminal UI for gh-stack stacks",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			switch opts.ReviewIn {
			case "auto", "herdr", "tmux", "inline":
			default:
				return errors.New("--review-in must be auto, herdr, tmux, or inline")
			}
			opts.RemoteInterval = max(opts.RemoteInterval, 5*time.Second)
			opts.PollInterval = max(opts.PollInterval, 250*time.Millisecond)
			return nil
		},
		RunE: func(*cobra.Command, []string) error { return run(dir, opts) },
	}

	// Flags for every command that shows the TUI.
	f := cmd.PersistentFlags()
	f.DurationVar(
		&opts.RemoteInterval,
		"interval",
		30*time.Second,
		"how often to refresh PR and CI state from GitHub",
	)
	f.DurationVar(
		&opts.PollInterval,
		"poll",
		2*time.Second,
		"how often to check local branches for changes",
	)
	f.StringVar(
		&opts.ReviewCmd,
		"review-cmd",
		"tuicr -r {range}",
		"review command; {range} becomes base..head",
	)
	f.StringVar(
		&opts.ReviewPRCmd,
		"review-pr-cmd",
		"tuicr pr {pr}",
		"review command for a single branch with an open PR; {pr} becomes its number (empty: always review the range)",
	)
	f.StringVar(
		&opts.ReviewIn,
		"review-in",
		"auto",
		"where reviews open: auto, herdr, tmux, or inline",
	)

	cmd.Flags().StringVarP(&dir, "dir", "C", ".", "run as if started in `dir`")
	cmd.Flags().IntVar(
		&opts.StackNumber,
		"stack",
		0,
		"stack `number` to show at startup (default: the checked out stack)",
	)

	cmd.AddCommand(demoCmd(&opts))
	return cmd
}

func demoCmd(opts *tui.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "demo",
		Short: "Show made-up stacks to try the UI",
		Long: "Show made-up stacks and pull requests instead of a repository's.\n" +
			"Commands still run, in an empty temporary directory, so they fail without touching anything.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return runDemo(*opts) },
	}
}

func run(dir string, opts tui.Options) error {
	repo, err := git.Open(dir)
	if err != nil {
		return err
	}
	// gh, and go-gh's repository detection, work in the current directory.
	if err := os.Chdir(repo.Root); err != nil {
		return err
	}
	// Without a cache the TUI still works, only slower to show PRs.
	cacheDir, err := cache.Dir()
	if err == nil {
		opts.Cache, err = cache.Open(cacheDir)
	}
	opts.CacheErr = err
	_, err = tea.NewProgram(tui.New(stack.NewSource(repo), opts)).Run()
	opts.Cache.Close()
	return err
}

// runDemo shows the demo stacks. Commands run in an empty directory, so they
// fail without touching any repository.
func runDemo(opts tui.Options) error {
	dir, err := os.MkdirTemp("", "ghst-demo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.Chdir(dir); err != nil {
		return err
	}
	remote := &demo.Remote{Delay: 600 * time.Millisecond}
	opts.Connect = func(string) (tui.Remote, error) { return remote, nil }
	_, err = tea.NewProgram(tui.New(demo.NewSource(time.Now, dir), opts)).Run()
	return err
}
