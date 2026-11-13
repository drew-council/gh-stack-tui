package stack

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"
)

func TestInstalledVersion(t *testing.T) {
	cases := map[string]string{
		"gh stack version 0.2.1\n":                     "0.2.1",
		"gh stack version v0.2.1\n":                    "0.2.1",
		"gh stack version 0.3.0-unstable-2026-11-10\n": "0.3.0",
		"gh stack version 1.12.3+build.4\n":            "1.12.3",
		"gh stack version dev\n":                       "",
		"":                                             "",
	}
	for out, want := range cases {
		if got := installedVersion(out); got != want {
			t.Errorf("installedVersion(%q) = %q, want %q", out, got, want)
		}
	}
}

func TestWarning(t *testing.T) {
	cases := []struct {
		name string
		g    GHStack
		want string
	}{
		{"same", GHStack{Expected: "0.2.1", Installed: "0.2.1"}, ""},
		{"patch", GHStack{Expected: "0.2.1", Installed: "0.2.5"}, ""},
		{
			"minor",
			GHStack{Expected: "0.2.1", Installed: "0.3.0"},
			"gh stack 0.3.0 installed; ghst expects 0.2.x",
		},
		{
			"older",
			GHStack{Expected: "0.2.1", Installed: "0.1.9"},
			"gh stack 0.1.9 installed; ghst expects 0.2.x",
		},
		{
			"major",
			GHStack{Expected: "0.2.1", Installed: "1.2.1"},
			"gh stack 1.2.1 installed; ghst expects 0.2.x",
		},
		{"unknown build", GHStack{Installed: "0.3.0"}, ""},
		{"dev install", GHStack{Expected: "0.2.1"}, ""},
		{
			"no gh",
			GHStack{Expected: "0.2.1", Missing: "gh"},
			"gh not found; stack commands will fail",
		},
		{
			"no extension",
			GHStack{Missing: "gh stack"},
			"gh stack not installed; gh extension install github/gh-stack",
		},
	}
	for _, c := range cases {
		if got := c.g.Warning(); got != c.want {
			t.Errorf("%s: Warning() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestExpectedVersion(t *testing.T) {
	dep := func(v string, replace *debug.Module) *debug.Module {
		return &debug.Module{Path: ghStackModule, Version: v, Replace: replace}
	}
	cases := []struct {
		name string
		deps []*debug.Module
		want string
	}{
		{
			"listed",
			[]*debug.Module{{Path: "example.com/x", Version: "v9.9.9"}, dep("v0.2.1", nil)},
			"0.2.1",
		},
		{"absent", []*debug.Module{{Path: "example.com/x", Version: "v9.9.9"}}, ""},
		{
			"replaced by a release",
			[]*debug.Module{dep("v0.2.1", &debug.Module{Version: "v0.3.0"})},
			"0.3.0",
		},
		{
			"replaced by a directory",
			[]*debug.Module{dep("v0.2.1", &debug.Module{Path: "../gh-stack"})},
			"",
		},
	}
	for _, c := range cases {
		if got := expectedVersion(&debug.BuildInfo{Deps: c.deps}); got != c.want {
			t.Errorf("%s: expectedVersion = %q, want %q", c.name, got, c.want)
		}
	}
}

// Test binaries record their dependencies, so the real build info names the
// gh-stack release in go.mod.
func TestExpectedGHStackFromBuild(t *testing.T) {
	if _, ok := parseVersion(ExpectedGHStack()); !ok {
		t.Fatalf("ExpectedGHStack() = %q, want a version", ExpectedGHStack())
	}
}

// fakeGH puts a gh on PATH that runs script.
func fakeGH(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if script != "" {
		err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+script+"\n"), 0o755)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestCheckGHStack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh is a shell script")
	}
	t.Run("installed", func(t *testing.T) {
		fakeGH(t, `echo "gh stack version 0.3.0-unstable-2026-11-10"`)
		g := CheckGHStack(t.TempDir())
		if g.Installed != "0.3.0" || g.Missing != "" || g.Expected == "" {
			t.Fatalf("got %+v", g)
		}
	})
	t.Run("no extension", func(t *testing.T) {
		fakeGH(t, `echo 'unknown command "stack" for "gh"' >&2; exit 1`)
		if g := CheckGHStack(t.TempDir()); g.Missing != "gh stack" {
			t.Fatalf("got %+v", g)
		}
	})
	t.Run("broken extension", func(t *testing.T) {
		fakeGH(t, `echo 'unknown flag: --version' >&2; echo usage >&2; exit 1`)
		g := CheckGHStack(t.TempDir())
		if g.Missing != "" || g.Warning() != "gh stack --version failed: unknown flag: --version" {
			t.Fatalf("got %+v, warning %q", g, g.Warning())
		}
	})
	t.Run("no gh", func(t *testing.T) {
		fakeGH(t, "")
		if g := CheckGHStack(t.TempDir()); g.Missing != "gh" {
			t.Fatalf("got %+v", g)
		}
	})
}
