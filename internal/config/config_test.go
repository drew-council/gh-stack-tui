package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFileMissing(t *testing.T) {
	c, err := LoadFile(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ReviewIn != nil || c.Ghst != (Ghst{}) || c.Ghrv.Teams != nil {
		t.Errorf("got %+v, want an empty config", c)
	}
}

func TestLoadFile(t *testing.T) {
	c, err := LoadFile(write(t, `{
		"review_in": "tmux",
		"ghst": {"review_cmd": "tuicr -r {range}", "review_pr_cmd": "", "interval": "1m", "poll": "500ms"},
		"ghrv": {"teams": ["a", "b"], "drafts": true, "interval": "45s"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if *c.ReviewIn != "tmux" {
		t.Errorf("review_in = %q", *c.ReviewIn)
	}
	if c.Ghst.ReviewPRCmd == nil || *c.Ghst.ReviewPRCmd != "" {
		t.Errorf("review_pr_cmd = %v, want set and empty", c.Ghst.ReviewPRCmd)
	}
	if got := time.Duration(*c.Ghst.Poll); got != 500*time.Millisecond {
		t.Errorf("ghst.poll = %v", got)
	}
	if !slices.Equal(c.Ghrv.Teams, []string{"a", "b"}) {
		t.Errorf("ghrv.teams = %v", c.Ghrv.Teams)
	}
	if !*c.Ghrv.Drafts || c.Ghrv.Bots != nil || c.Ghrv.ReviewCmd != nil {
		t.Errorf("ghrv = %+v", c.Ghrv)
	}
}

func TestLoadFileErrors(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key":     `{"ghst": {"intreval": "30s"}}`,
		"bad duration":    `{"ghrv": {"poll": "soon"}}`,
		"number duration": `{"ghrv": {"poll": 30}}`,
		"malformed":       `{"review_in": "auto",}`,
		"trailing data":   `{} {}`,
		"bad review_in":   `{"review_in": "Browser"}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := write(t, body)
			_, err := LoadFile(path)
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Errorf("err = %v, want an error naming %s", err, path)
			}
		})
	}
}

func TestPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got, _ := Path(); got != filepath.Join(xdg, "gh-stack-tui", "config.json") {
		t.Errorf("with XDG_CONFIG_HOME: %s", got)
	}

	want := filepath.Join(home, ".config", "gh-stack-tui", "config.json")
	for _, v := range []string{"", "relative"} {
		t.Setenv("XDG_CONFIG_HOME", v)
		if got, _ := Path(); got != want {
			t.Errorf("XDG_CONFIG_HOME=%q: %s, want %s", v, got, want)
		}
	}
}

func TestApplyPrecedence(t *testing.T) {
	flags := pflag.NewFlagSet("ghrv", pflag.ContinueOnError)
	interval := flags.Duration("interval", 30*time.Second, "")
	poll := flags.Duration("poll", 2*time.Second, "")
	reviewIn := flags.String("review-in", "auto", "")
	reviewCmd := flags.String("review-cmd", "tuicr pr {url}", "")
	drafts := flags.Bool("drafts", false, "")
	bots := flags.Bool("bots", false, "")
	teams := flags.StringSlice("team", nil, "")
	if err := flags.Parse([]string{"--interval", "10s", "--team", "cli"}); err != nil {
		t.Fatal(err)
	}

	c, err := LoadFile(write(t, `{
		"review_in": "inline",
		"ghrv": {"teams": ["a", "b"], "bots": true, "interval": "1m", "poll": "5s"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyGhrv(flags); err != nil {
		t.Fatal(err)
	}

	// Passed on the command line: the flag wins.
	if *interval != 10*time.Second {
		t.Errorf("interval = %v, want the flag's 10s", *interval)
	}
	if !slices.Equal(*teams, []string{"cli"}) {
		t.Errorf("team = %v, want the flag's [cli]", *teams)
	}
	// Not passed: the file wins over the default.
	if *poll != 5*time.Second || *reviewIn != "inline" || !*bots {
		t.Errorf("poll, review-in, bots = %v, %q, %v; want the file's", *poll, *reviewIn, *bots)
	}
	// Set by neither: the default stays.
	if *reviewCmd != "tuicr pr {url}" || *drafts {
		t.Errorf("review-cmd, drafts = %q, %v; want the defaults", *reviewCmd, *drafts)
	}
}

func TestApplyGhstEmptyCommand(t *testing.T) {
	flags := pflag.NewFlagSet("ghst", pflag.ContinueOnError)
	prCmd := flags.String("review-pr-cmd", "tuicr pr {pr}", "")
	for _, name := range []string{"review-in", "review-cmd"} {
		flags.String(name, "x", "")
	}
	flags.Duration("interval", 0, "")
	flags.Duration("poll", 0, "")

	c, err := LoadFile(write(t, `{"ghst": {"review_pr_cmd": ""}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyGhst(flags); err != nil {
		t.Fatal(err)
	}
	if *prCmd != "" {
		t.Errorf("review-pr-cmd = %q, want empty", *prCmd)
	}
}

// demo has no --team, so the file's teams are skipped there.
func TestApplySkipsMissingFlags(t *testing.T) {
	flags := pflag.NewFlagSet("demo", pflag.ContinueOnError)
	drafts := flags.Bool("drafts", false, "")
	c, err := LoadFile(write(t, `{"ghrv": {"teams": ["x"], "drafts": true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyGhrv(flags); err != nil || !*drafts {
		t.Fatalf("drafts = %v, err %v", *drafts, err)
	}
}

func TestForCompletion(t *testing.T) {
	root := &cobra.Command{Use: "ghst"}
	demo := &cobra.Command{Use: "demo"}
	root.AddCommand(demo)
	// Cobra adds the completion command only to a root with subcommands.
	root.InitDefaultCompletionCmd()
	zsh, _, err := root.Find([]string{"completion", "zsh"})
	if err != nil {
		t.Fatal(err)
	}
	if !ForCompletion(zsh) || ForCompletion(demo) || ForCompletion(root) {
		t.Fatal("ForCompletion picked the wrong commands")
	}
}
