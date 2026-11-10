package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Tab is one open tab of a terminal multiplexer.
type Tab struct {
	ID    string
	Label string
}

// Tabs runs commands in tabs next to the TUI, so the TUI stays open.
type Tabs interface {
	// Noun names one tab, such as "herdr tab", for messages.
	Noun() string
	// Open runs command in a new tab named label, started in cwd, and returns
	// the tab's ID. focus switches to the tab.
	Open(cwd, label, command string, focus bool) (string, error)
	// List returns the open tabs.
	List() ([]Tab, error)
	// Focus switches to the tab with the given ID.
	Focus(id string) error
}

// TabTarget resolves a --review-in preference to herdr, tmux, or inline.
// "auto" and "" pick herdr inside herdr, tmux inside tmux, and inline
// otherwise.
func TabTarget(pref string) string {
	if pref != "" && pref != "auto" {
		return pref
	}
	if os.Getenv("HERDR_ENV") == "1" {
		if _, err := exec.LookPath("herdr"); err == nil {
			return "herdr"
		}
	}
	if os.Getenv("TMUX") != "" {
		return "tmux"
	}
	return "inline"
}

// NewTabs returns the tabs of target, as [TabTarget] names it. It returns nil
// for inline: there are no tabs, and commands take over the terminal.
func NewTabs(target string) Tabs {
	switch target {
	case "herdr":
		return herdr{workspace: os.Getenv("HERDR_WORKSPACE_ID")}
	case "tmux":
		return tmux{}
	}
	return nil
}

// herdr opens tabs in one herdr workspace, or the focused one when workspace
// is empty.
type herdr struct{ workspace string }

func (h herdr) Noun() string { return "herdr tab" }

func (h herdr) Open(cwd, label, command string, focus bool) (string, error) {
	args := []string{"tab", "create", "--cwd", cwd, "--label", label}
	if focus {
		args = append(args, "--focus")
	} else {
		args = append(args, "--no-focus")
	}
	if h.workspace != "" {
		args = append(args, "--workspace", h.workspace)
	}
	out, err := exec.Command("herdr", args...).Output()
	if err != nil {
		return "", fmt.Errorf("herdr tab create: %w", stderrOf(err))
	}
	var resp struct {
		Result struct {
			Tab struct {
				TabID string `json:"tab_id"`
			} `json:"tab"`
			RootPane struct {
				PaneID string `json:"pane_id"`
				TabID  string `json:"tab_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || resp.Result.RootPane.PaneID == "" {
		return "", fmt.Errorf(
			"herdr tab create: unexpected response %q",
			strings.TrimSpace(string(out)),
		)
	}
	// The command runs in the tab's shell, so the tab outlives it.
	if _, err := exec.Command("herdr", "pane", "run", resp.Result.RootPane.PaneID, command).
		Output(); err != nil {
		return "", fmt.Errorf("herdr pane run: %w", stderrOf(err))
	}
	id := resp.Result.Tab.TabID
	if id == "" {
		id = resp.Result.RootPane.TabID
	}
	return id, nil
}

func (h herdr) List() ([]Tab, error) {
	args := []string{"tab", "list"}
	if h.workspace != "" {
		args = append(args, "--workspace", h.workspace)
	}
	out, err := exec.Command("herdr", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("herdr tab list: %w", stderrOf(err))
	}
	var resp struct {
		Result struct {
			Tabs []struct {
				TabID string `json:"tab_id"`
				Label string `json:"label"`
			} `json:"tabs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("herdr tab list: %w", err)
	}
	tabs := make([]Tab, len(resp.Result.Tabs))
	for i, t := range resp.Result.Tabs {
		tabs[i] = Tab{ID: t.TabID, Label: t.Label}
	}
	return tabs, nil
}

func (h herdr) Focus(id string) error {
	if _, err := exec.Command("herdr", "tab", "focus", id).Output(); err != nil {
		return fmt.Errorf("herdr tab focus: %w", stderrOf(err))
	}
	return nil
}

// tmux opens windows in the current tmux session.
type tmux struct{}

func (tmux) Noun() string { return "tmux window" }

func (tmux) Open(cwd, label, command string, focus bool) (string, error) {
	args := []string{"new-window", "-P", "-F", "#{window_id}", "-c", cwd, "-n", label}
	if !focus {
		args = append(args, "-d")
	}
	out, err := exec.Command("tmux", append(args, command)...).CombinedOutput()
	if err != nil {
		return "", errors.New("tmux: " + strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (tmux) List() ([]Tab, error) {
	out, err := exec.Command("tmux", "list-windows", "-F", "#{window_id}\t#{window_name}").
		Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-windows: %w", stderrOf(err))
	}
	var tabs []Tab
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if id, label, ok := strings.Cut(line, "\t"); ok {
			tabs = append(tabs, Tab{ID: id, Label: label})
		}
	}
	return tabs, nil
}

func (tmux) Focus(id string) error {
	if out, err := exec.Command("tmux", "select-window", "-t", id).CombinedOutput(); err != nil {
		return errors.New("tmux: " + strings.TrimSpace(string(out)))
	}
	return nil
}

// stderrOf replaces a failed command's error with what it wrote to stderr.
func stderrOf(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return errors.New(strings.TrimSpace(string(exitErr.Stderr)))
	}
	return err
}
