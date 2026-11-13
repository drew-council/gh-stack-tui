package desktop

import (
	"errors"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// Place is where a review opens.
type Place int

const (
	// InTab runs the review command in a tab next to the TUI.
	InTab Place = iota
	// Inline runs the review command in the TUI's own terminal, which it takes
	// over until the command exits.
	Inline
	// InBrowser opens the changes on GitHub.
	InBrowser
)

// ReviewPlaces lists the values of --review-in, for help and errors.
const ReviewPlaces = "auto, herdr, tmux, inline, or browser"

// Reviews decides where reviews open. Both TUIs ask it, so a review opens the
// same way in each.
type Reviews struct {
	// Tabs are where review commands run. Nil runs them inline.
	Tabs Tabs
	// Browser opens every review on GitHub, without running a command.
	Browser bool
	// Installed reports whether a program can run. Nil looks it up in PATH.
	Installed func(program string) bool
}

// NewReviews resolves a --review-in preference, one of [ReviewPlaces]. "auto"
// and "" open reviews in a herdr tab inside herdr, a tmux window inside tmux,
// and in the TUI's terminal otherwise.
func NewReviews(pref string) (Reviews, error) {
	switch pref {
	case "browser":
		return Reviews{Browser: true}, nil
	case "", "auto", "herdr", "tmux", "inline":
		return Reviews{Tabs: NewTabs(TabTarget(pref))}, nil
	}
	return Reviews{}, errors.New("--review-in must be " + ReviewPlaces)
}

// Where returns where command opens. A command whose program is not
// installed opens on GitHub, since nothing could run it; missing then names
// the program.
func (r Reviews) Where(command string) (place Place, missing string) {
	if r.Browser {
		return InBrowser, ""
	}
	if p := Program(command); p != "" && !r.installed(p) {
		return InBrowser, p
	}
	if r.Tabs != nil {
		return InTab, ""
	}
	return Inline, ""
}

func (r Reviews) installed(program string) bool {
	if r.Installed != nil {
		return r.Installed(program)
	}
	_, err := exec.LookPath(program)
	return err == nil
}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// wrappers run the word after them as the program.
var wrappers = []string{"exec", "command"}

// shellWords are builtins and keywords that sh runs without a program on
// PATH.
var shellWords = []string{
	"cd", "if", "for", "while", "until", "case", "eval", ".", "source", "set",
	"export", "time", "!",
}

// Program returns the program a shell command runs: its first word after any
// variable assignments and exec or command. It returns "" when that word is
// not a plain program name, such as a builtin, a subshell, or a path from ~,
// so the command is trusted to run.
func Program(command string) string {
	for _, w := range strings.Fields(command) {
		if assignment.MatchString(w) || slices.Contains(wrappers, w) {
			continue
		}
		if slices.Contains(shellWords, w) || strings.HasPrefix(w, "~") ||
			strings.ContainsAny(w, "()$`{}'\"\\;&|<>") {
			return ""
		}
		return w
	}
	return ""
}
