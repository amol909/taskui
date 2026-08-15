package main

import (
	"os"
	"path/filepath"
	"testing"
)

// resolvedPath returns the absolute, symlink-resolved form of p, matching
// what resolveProjectRoot produces, so tests can compare against it reliably
// (macOS temp dirs are themselves often behind a symlink, e.g. /tmp ->
// /private/tmp).
func resolvedPath(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", p, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(%q): %v", abs, err)
	}
	return resolved
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
}

func TestResolveProjectRoot_GitDir(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, ".git"))
	start := filepath.Join(root, "a", "b")
	mustMkdirAll(t, start)

	got, err := resolveProjectRoot(start)
	if err != nil {
		t.Fatalf("resolveProjectRoot: %v", err)
	}
	if got == nil {
		t.Fatalf("expected a resolved project root, got nil")
	}
	if want := resolvedPath(t, root); got.Path != want {
		t.Errorf("Path = %q, want %q", got.Path, want)
	}
	if got.Marker != ".git" {
		t.Errorf("Marker = %q, want %q", got.Marker, ".git")
	}
}

func TestResolveProjectRoot_GitFile(t *testing.T) {
	// Git worktrees and submodules use a .git *file*, not a directory.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere/.git\n"), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}
	start := filepath.Join(root, "a", "b")
	mustMkdirAll(t, start)

	got, err := resolveProjectRoot(start)
	if err != nil {
		t.Fatalf("resolveProjectRoot: %v", err)
	}
	if got == nil {
		t.Fatalf("expected a resolved project root, got nil")
	}
	if want := resolvedPath(t, root); got.Path != want {
		t.Errorf("Path = %q, want %q", got.Path, want)
	}
}

func TestResolveProjectRoot_GitBeatsNearerLanguageMarker(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, ".git"))
	pkgDir := filepath.Join(root, "pkg", "web")
	mustMkdirAll(t, pkgDir)
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	got, err := resolveProjectRoot(pkgDir)
	if err != nil {
		t.Fatalf("resolveProjectRoot: %v", err)
	}
	if got == nil {
		t.Fatalf("expected a resolved project root, got nil")
	}
	if want := resolvedPath(t, root); got.Path != want {
		t.Errorf("Path = %q, want %q (git root should beat nearer package.json)", got.Path, want)
	}
	if got.Marker != ".git" {
		t.Errorf("Marker = %q, want %q", got.Marker, ".git")
	}
}

func TestResolveProjectRoot_TaskuiRootOverridesEverything(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, ".git"))
	pkgDir := filepath.Join(root, "pkg", "web")
	mustMkdirAll(t, pkgDir)
	if err := os.WriteFile(filepath.Join(pkgDir, ".taskui-root"), []byte("  my-subpackage  \n"), 0o644); err != nil {
		t.Fatalf("write .taskui-root: %v", err)
	}

	got, err := resolveProjectRoot(pkgDir)
	if err != nil {
		t.Fatalf("resolveProjectRoot: %v", err)
	}
	if got == nil {
		t.Fatalf("expected a resolved project root, got nil")
	}
	if want := resolvedPath(t, pkgDir); got.Path != want {
		t.Errorf("Path = %q, want %q", got.Path, want)
	}
	if got.Name != "my-subpackage" {
		t.Errorf("Name = %q, want %q", got.Name, "my-subpackage")
	}
	if got.Marker != "taskui-root" {
		t.Errorf("Marker = %q, want %q", got.Marker, "taskui-root")
	}
}

func TestResolveProjectRoot_GoModOnlyNoGit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	start := filepath.Join(root, "a")
	mustMkdirAll(t, start)

	got, err := resolveProjectRoot(start)
	if err != nil {
		t.Fatalf("resolveProjectRoot: %v", err)
	}
	if got == nil {
		t.Fatalf("expected a resolved project root, got nil")
	}
	if want := resolvedPath(t, root); got.Path != want {
		t.Errorf("Path = %q, want %q", got.Path, want)
	}
}

func TestResolveProjectRoot_NoMarkers(t *testing.T) {
	// A bare temp dir with no markers anywhere up to the real filesystem
	// root should resolve to nil, nil.
	dir := t.TempDir()

	got, err := resolveProjectRoot(dir)
	if err != nil {
		t.Fatalf("resolveProjectRoot: unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for a dir with no markers, got %+v", got)
	}
}

func TestResolveProjectRoot_SymlinkedPathResolves(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, ".git"))

	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	got, err := resolveProjectRoot(link)
	if err != nil {
		t.Fatalf("resolveProjectRoot: %v", err)
	}
	if got == nil {
		t.Fatalf("expected a resolved project root, got nil")
	}
	if want := resolvedPath(t, root); got.Path != want {
		t.Errorf("Path = %q, want %q", got.Path, want)
	}
}

func TestResolveProjectRoot_HomeDirectoryGuard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustMkdirAll(t, filepath.Join(home, ".git"))

	got, err := resolveProjectRoot(home)
	if err != nil {
		t.Fatalf("resolveProjectRoot: unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for a .git directly at $HOME, got %+v", got)
	}
}

func TestResolveProjectRoot_HomeDirectoryGuardOverriddenByTaskuiRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustMkdirAll(t, filepath.Join(home, ".git"))
	if err := os.WriteFile(filepath.Join(home, ".taskui-root"), []byte(""), 0o644); err != nil {
		t.Fatalf("write .taskui-root: %v", err)
	}

	got, err := resolveProjectRoot(home)
	if err != nil {
		t.Fatalf("resolveProjectRoot: %v", err)
	}
	if got == nil {
		t.Fatalf("expected an explicit .taskui-root at $HOME to override the home-directory guard")
	}
	if want := resolvedPath(t, home); got.Path != want {
		t.Errorf("Path = %q, want %q", got.Path, want)
	}
}
