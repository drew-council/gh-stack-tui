package ui

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// plain is the rendered lines without color or trailing spaces.
func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	return out
}

func TestMarkdown(t *testing.T) {
	body := "<!-- Describe the change. -->\n## Why\nSessions *expired* early, see #12.\n\n\n\n" +
		"### What\n- **First** item that is long enough to wrap around\n" +
		"- [x] Second\n\n> quoted\n\n```go\nfunc main() {}\n```\n"
	got := plain(NewStyles().Markdown(body, 30))
	want := []string{
		"Why",
		"",
		"Sessions expired early, see",
		"#12.",
		"",
		"What",
		"",
		"• First item that is long",
		"enough to wrap around",
		"[✓] Second",
		"",
		"│ quoted",
		"",
		"  func main() {}",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestMarkdownFitsWidthAndStyles(t *testing.T) {
	st := NewStyles()
	lines := st.Markdown("## Head\n\n"+strings.Repeat("word ", 40)+"`code`", 24)
	for _, l := range lines {
		if w := lipgloss.Width(l); w > 24 {
			t.Fatalf("line is %d columns wide, over 24: %q", w, ansi.Strip(l))
		}
	}
	// glamour sets a color along with other attributes in one sequence.
	peach := strings.TrimSuffix(strings.TrimPrefix(FgSeq(st.Peach), "\x1b["), "m")
	if !strings.Contains(strings.Join(lines, "\n"), peach) {
		t.Fatal("inline code should be in the palette's peach")
	}
	if got := st.Markdown("", 24); len(got) != 0 {
		t.Fatalf("no text should render no lines, got %q", got)
	}
}
