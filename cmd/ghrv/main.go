package main

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/drew-council/gh-stack-tui/internal/cache"
	"github.com/drew-council/gh-stack-tui/internal/config"
	"github.com/drew-council/gh-stack-tui/internal/demo"
	"github.com/drew-council/gh-stack-tui/internal/desktop"
	"github.com/drew-council/gh-stack-tui/internal/git"
	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/reviewtui"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var opts reviewtui.Options
	var dir, reviewIn string
	cmd := &cobra.Command{
		Use:          "ghrv",
		Short:        "A terminal UI for the pull requests that wait on your review",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if config.ForCompletion(cmd) {
				return nil
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.ApplyGhrv(cmd.Flags()); err != nil {
				return err
			}
			if opts.Reviews, err = desktop.NewReviews(reviewIn); err != nil {
				return err
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
		"how often to refresh the queue from GitHub",
	)
	f.DurationVar(
		&opts.PollInterval,
		"poll",
		2*time.Second,
		"how often to check which reviews have a tab open",
	)
	f.StringVar(
		&opts.ReviewCmd,
		"review-cmd",
		"tuicr pr {url}",
		"review command; {url} becomes the PR's URL and {pr} its number",
	)
	f.StringVar(
		&reviewIn,
		"review-in",
		"auto",
		"where reviews open: "+desktop.ReviewPlaces+"; reviews open on GitHub when the review command is not installed",
	)
	f.BoolVar(&opts.Show.Drafts, "drafts", false, "keep draft PRs in the queue")
	f.BoolVar(&opts.Show.Bots, "bots", false, "keep PRs opened by bots in the queue")

	cmd.Flags().StringVarP(&dir, "dir", "C", ".", "run as if started in `dir`")
	cmd.Flags().StringSliceVar(
		&opts.Query.Teams,
		"team",
		nil,
		"only take team requests to this team `slug`; repeat for several (default: every team of yours)",
	)

	cmd.AddCommand(demoCmd(&opts))
	return cmd
}

func demoCmd(opts *reviewtui.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "demo",
		Short: "Show a made-up review queue to try the UI",
		Long: "Show a made-up review queue instead of a repository's.\n" +
			"Reviews open in made-up tabs, so nothing runs.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return runDemo(*opts) },
	}
}

func run(dir string, opts reviewtui.Options) error {
	repo, err := git.Open(dir)
	if err != nil {
		return err
	}
	// go-gh's repository detection works in the current directory, and
	// reviews run in the repository.
	if err := os.Chdir(repo.Root); err != nil {
		return err
	}
	opts.Dir, opts.Local = repo.Root, repo
	if opts.Repo, err = github.CurrentRepo(); err != nil {
		return err
	}
	opts.Connect = func() (reviewtui.Remote, error) { return github.NewClient(opts.Repo) }
	// Without a cache the TUI still works, only slower to show the queue.
	cacheDir, err := cache.Dir()
	if err == nil {
		opts.Cache, err = cache.Open(cacheDir)
	}
	opts.CacheErr = err
	_, err = tea.NewProgram(reviewtui.New(opts)).Run()
	opts.Cache.Close()
	return err
}

// runDemo shows the demo queue. Its tabs are made up, so reviews open
// nothing.
func runDemo(opts reviewtui.Options) error {
	remote := &demo.ReviewRemote{Delay: 600 * time.Millisecond, Now: time.Now}
	opts.Repo = remote.Repository()
	opts.Connect = func() (reviewtui.Remote, error) { return remote, nil }
	opts.Local = demo.Checkout{}
	opts.Reviews = demo.Reviews("#337")
	_, err := tea.NewProgram(reviewtui.New(opts)).Run()
	return err
}
