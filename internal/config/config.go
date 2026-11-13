// Package config reads defaults for ghst and ghrv from a JSON file, so they
// need not be passed as flags each time.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/drew-council/gh-stack-tui/internal/desktop"
	"github.com/drew-council/gh-stack-tui/internal/ui"
)

// Config is the config file. A nil field is unset and leaves the flag's
// default alone.
type Config struct {
	ReviewIn *string  `json:"review_in"`
	Theme    ui.Theme `json:"theme"` // unset is auto
	Ghst     Ghst     `json:"ghst"`
	Ghrv     Ghrv     `json:"ghrv"`
}

// Ghst holds defaults for ghst's flags.
type Ghst struct {
	ReviewCmd   *string   `json:"review_cmd"`
	ReviewPRCmd *string   `json:"review_pr_cmd"`
	Interval    *Duration `json:"interval"`
	Poll        *Duration `json:"poll"`
}

// Ghrv holds defaults for ghrv's flags.
type Ghrv struct {
	ReviewCmd *string   `json:"review_cmd"`
	Teams     []string  `json:"teams"`
	Drafts    *bool     `json:"drafts"`
	Bots      *bool     `json:"bots"`
	Interval  *Duration `json:"interval"`
	Poll      *Duration `json:"poll"`
}

// Duration is a time.Duration written as a Go duration string, such as "30s".
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string such as \"30s\", not %s", b)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// Path returns where the config file lives:
// $XDG_CONFIG_HOME/gh-stack-tui/config.json, else
// $HOME/.config/gh-stack-tui/config.json, on every OS.
func Path() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	// The XDG spec says to ignore a relative path.
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "gh-stack-tui", "config.json"), nil
}

// Load reads the config file at Path.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	return LoadFile(path)
}

// LoadFile reads the config file at path. A missing file is an empty config.
// Errors name the file.
func LoadFile(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Config{}, fmt.Errorf("%s: unexpected data after the top-level object", path)
	}
	if c.ReviewIn != nil {
		if err := desktop.CheckReviewIn(*c.ReviewIn); err != nil {
			return Config{}, fmt.Errorf("%s: review_in %w", path, err)
		}
	}
	return c, nil
}

// ForCompletion reports whether cmd is one of cobra's shell completion
// commands, which need no config: a bad file must not break completion.
func ForCompletion(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	}
	return cmd.HasParent() && cmd.Parent().Name() == "completion"
}

// ApplyGhst sets ghst's flags from c, except those passed on the command
// line.
func (c Config) ApplyGhst(flags *pflag.FlagSet) error {
	return apply(flags, []setting{
		str("review-in", c.ReviewIn),
		str("review-cmd", c.Ghst.ReviewCmd),
		str("review-pr-cmd", c.Ghst.ReviewPRCmd),
		dur("interval", c.Ghst.Interval),
		dur("poll", c.Ghst.Poll),
	})
}

// ApplyGhrv sets ghrv's flags from c, except those passed on the command
// line.
func (c Config) ApplyGhrv(flags *pflag.FlagSet) error {
	teams := setting{flag: "team"}
	if c.Ghrv.Teams != nil {
		teams.value, teams.ok = strings.Join(c.Ghrv.Teams, ","), true
	}
	return apply(flags, []setting{
		str("review-in", c.ReviewIn),
		str("review-cmd", c.Ghrv.ReviewCmd),
		teams,
		boolean("drafts", c.Ghrv.Drafts),
		boolean("bots", c.Ghrv.Bots),
		dur("interval", c.Ghrv.Interval),
		dur("poll", c.Ghrv.Poll),
	})
}

// setting is a flag's value from the config file, in the flag's own syntax.
type setting struct {
	flag  string
	value string
	ok    bool
}

func str(flag string, v *string) setting {
	if v == nil {
		return setting{flag: flag}
	}
	return setting{flag, *v, true}
}

func dur(flag string, v *Duration) setting {
	if v == nil {
		return setting{flag: flag}
	}
	return setting{flag, time.Duration(*v).String(), true}
}

func boolean(flag string, v *bool) setting {
	if v == nil {
		return setting{flag: flag}
	}
	return setting{flag, strconv.FormatBool(*v), true}
}

// apply sets each flag that the file sets and the command line does not.
// Values go through the flags' own parsing, so the program validates them
// as it does flags. A subcommand may lack a flag, such as demo's --team; its
// setting is skipped.
func apply(flags *pflag.FlagSet, settings []setting) error {
	for _, s := range settings {
		if !s.ok || flags.Lookup(s.flag) == nil || flags.Changed(s.flag) {
			continue
		}
		if err := flags.Set(s.flag, s.value); err != nil {
			return fmt.Errorf("config %s: %w", s.flag, err)
		}
	}
	return nil
}
