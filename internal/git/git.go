// Package git wraps the handful of git plumbing commands the TUI needs.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Repo runs git commands against a single working tree.
type Repo struct {
	// Root is the top level of the working tree.
	Root string
	// GitDir is the absolute git directory, which is where gh-stack keeps its
	// stack file.
	GitDir string
}

// Open resolves the repository containing dir.
func Open(dir string) (*Repo, error) {
	r := &Repo{Root: dir}
	root, err := r.run("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	gitDir, err := r.run("rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	return &Repo{Root: root, GitDir: gitDir}, nil
}

func (r *Repo) run(args ...string) (string, error) {
	out, err := r.output(args...)
	return strings.TrimSpace(out), err
}

func (r *Repo) output(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return stdout.String(), err
		}
		return stdout.String(), fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// CurrentBranch returns the checked out branch, or "" when HEAD is detached.
func (r *Repo) CurrentBranch() string {
	out, err := r.run("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// BranchHeads maps every local branch to the commit it points at.
func (r *Repo) BranchHeads() (map[string]string, error) {
	out, err := r.run("for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads")
	if err != nil {
		return nil, err
	}
	heads := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		name, sha, ok := strings.Cut(line, " ")
		if ok {
			heads[name] = sha
		}
	}
	return heads, nil
}

// IsAncestor reports whether a is reachable from b.
func (r *Repo) IsAncestor(a, b string) bool {
	_, err := r.run("merge-base", "--is-ancestor", a, b)
	return err == nil
}

// MergeBase returns the best common ancestor of a and b.
func (r *Repo) MergeBase(a, b string) (string, error) {
	return r.run("merge-base", a, b)
}

// Dirty reports whether the working tree has uncommitted changes.
func (r *Repo) Dirty() bool {
	out, err := r.run("status", "--porcelain", "--untracked-files=no")
	return err == nil && out != ""
}

// Commit is a single commit in a branch's range.
type Commit struct {
	SHA     string
	Subject string
	Author  string
	Time    time.Time
}

// Log lists the commits in base..head, newest first.
func (r *Repo) Log(base, head string) ([]Commit, error) {
	out, err := r.run("log", "--format=%H%x1f%s%x1f%an%x1f%ct", base+".."+head)
	if err != nil || out == "" {
		return nil, err
	}
	var commits []Commit
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.Split(line, "\x1f")
		if len(parts) != 4 {
			continue
		}
		unix, _ := strconv.ParseInt(parts[3], 10, 64)
		commits = append(commits, Commit{
			SHA:     parts[0],
			Subject: parts[1],
			Author:  parts[2],
			Time:    time.Unix(unix, 0),
		})
	}
	return commits, nil
}

// FileChange is one file touched between two commits.
type FileChange struct {
	Path      string
	Status    string // A, M, D, T, ...
	Additions int
	Deletions int
	Binary    bool
}

// Diff lists the files changed between base and head.
func (r *Repo) Diff(base, head string) ([]FileChange, error) {
	numstat, err := r.output("diff", "--no-renames", "--numstat", "-z", base, head)
	if err != nil {
		return nil, err
	}
	nameStatus, err := r.output("diff", "--no-renames", "--name-status", "-z", base, head)
	if err != nil {
		return nil, err
	}

	// --name-status -z alternates status and path fields.
	status := map[string]string{}
	fields := strings.Split(strings.TrimSuffix(nameStatus, "\x00"), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		status[fields[i+1]] = fields[i]
	}

	var files []FileChange
	for rec := range strings.SplitSeq(strings.TrimSuffix(numstat, "\x00"), "\x00") {
		parts := strings.SplitN(rec, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		f := FileChange{Path: parts[2], Status: status[parts[2]]}
		if parts[0] == "-" {
			f.Binary = true
		} else {
			f.Additions, _ = strconv.Atoi(parts[0])
			f.Deletions, _ = strconv.Atoi(parts[1])
		}
		files = append(files, f)
	}
	return files, nil
}

// Fingerprint returns a cheap summary of the local state that changes whenever
// a branch moves, HEAD moves, or the stack file is rewritten. It is polled to
// decide when a full local reload is worth doing.
func (r *Repo) Fingerprint(extra ...string) (string, error) {
	refs, err := r.run("for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
	if err != nil {
		return "", err
	}
	head, _ := r.run("rev-parse", "HEAD")
	var b strings.Builder
	b.WriteString(r.CurrentBranch())
	b.WriteString("\n")
	b.WriteString(head)
	b.WriteString("\n")
	b.WriteString(refs)
	for _, e := range extra {
		b.WriteString("\n")
		b.WriteString(e)
	}
	return b.String(), nil
}
