package stack

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// ghStackModule is the module the shim reads gh-stack's files with.
const ghStackModule = "github.com/github/gh-stack"

// GHStack is the gh stack a user has installed, beside the gh-stack release
// this build reads stack files as.
type GHStack struct {
	// Expected is the gh-stack release this build was built against, or ""
	// when the build does not record it.
	Expected string
	// Installed is the version `gh stack --version` reports, or "" when it
	// could not tell.
	Installed string
	// Missing is "gh" or "gh stack" when that is not installed.
	Missing string
	// Failed is the first line of what `gh stack --version` wrote to stderr
	// when it failed for another reason.
	Failed string
}

// Warning is a short note for the user, or "" when the installed gh stack
// matches or the versions cannot be compared. Patch releases may differ.
func (g GHStack) Warning() string {
	switch g.Missing {
	case "gh":
		return "gh not found; stack commands will fail"
	case "gh stack":
		return "gh stack not installed; gh extension install github/gh-stack"
	}
	if g.Failed != "" {
		return "gh stack --version failed: " + g.Failed
	}
	want, ok := parseVersion(g.Expected)
	if !ok {
		return ""
	}
	have, ok := parseVersion(g.Installed)
	if !ok || have.major == want.major && have.minor == want.minor {
		return ""
	}
	return fmt.Sprintf(
		"gh stack %s installed; ghst expects %d.%d.x",
		have, want.major, want.minor,
	)
}

// CheckGHStack runs `gh stack --version` in dir and compares it with the
// gh-stack release this build was built against.
func CheckGHStack(dir string) GHStack {
	g := GHStack{Expected: ExpectedGHStack()}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "stack", "--version")
	cmd.Dir = dir
	out, err := cmd.Output()
	var exit *exec.ExitError
	switch {
	case errors.Is(err, exec.ErrNotFound):
		g.Missing = "gh"
	case errors.As(err, &exit) && ctx.Err() == nil:
		stderr := strings.TrimSpace(string(exit.Stderr))
		if strings.Contains(stderr, `unknown command "stack"`) {
			g.Missing = "gh stack"
			break
		}
		g.Failed, _, _ = strings.Cut(stderr, "\n")
		if g.Failed == "" {
			g.Failed = exit.Error()
		}
	case err == nil:
		g.Installed = installedVersion(string(out))
	}
	return g
}

// ExpectedGHStack is the version of gh-stack this binary was built against,
// without a leading "v", or "" when the build does not record it.
func ExpectedGHStack() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return expectedVersion(info)
}

func expectedVersion(info *debug.BuildInfo) string {
	for _, d := range info.Deps {
		if d.Path != ghStackModule {
			continue
		}
		if d.Replace != nil {
			// A replacement by a directory has no version.
			d = d.Replace
		}
		if v, ok := parseVersion(d.Version); ok {
			return v.String()
		}
	}
	return ""
}

type version struct{ major, minor, patch int }

func (v version) String() string { return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch) }

var versionRE = regexp.MustCompile(`\bv?(\d+)\.(\d+)\.(\d+)`)

// parseVersion reads the first semantic version in s, ignoring any
// prerelease or build suffix.
func parseVersion(s string) (version, bool) {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return version{}, false
	}
	var v version
	for i, p := range []*int{&v.major, &v.minor, &v.patch} {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return version{}, false
		}
		*p = n
	}
	return v, true
}

// installedVersion reads the version from `gh stack --version`, which prints
// "gh stack version 0.2.1". It returns "" for builds without one, such as
// "dev".
func installedVersion(out string) string {
	v, ok := parseVersion(out)
	if !ok {
		return ""
	}
	return v.String()
}
