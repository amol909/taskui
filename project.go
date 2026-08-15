package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// languageMarkers are the class-C markers: any of these in a directory
// signal a project root, but they are the lowest-priority signal - a
// .taskui-root or a .git anywhere in the walk wins over a nearer language
// marker (see resolveProjectRoot).
var languageMarkers = []string{"go.mod", "package.json", "Cargo.toml", "pyproject.toml"}

type ProjectRoot struct {
	Path      string // absolute, symlinks resolved
	Name      string // display name
	GitRemote string // best-effort, "" if unknown
	Marker    string // which marker matched, for debugging/tests
}

// resolveProjectRoot walks up from startDir looking for a project marker.
// Returns nil (not an error) when startDir is not inside any project.
func resolveProjectRoot(startDir string) (*ProjectRoot, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}

	// Walk from resolved up to the filesystem root, recording the first
	// (nearest) directory that contains each marker class. Priority is
	// applied across the whole walk, not within one directory: a
	// .taskui-root or .git found farther up still beats a nearer language
	// marker.
	var nearestTaskuiRoot, nearestGit, nearestLang string

	d := resolved
	for {
		if nearestTaskuiRoot == "" {
			if info, statErr := os.Stat(filepath.Join(d, ".taskui-root")); statErr == nil && !info.IsDir() {
				nearestTaskuiRoot = d
			}
		}
		if nearestGit == "" {
			// .git may be a file (git worktrees and submodules use a
			// file), so just check for existence, not is-dir.
			if _, statErr := os.Lstat(filepath.Join(d, ".git")); statErr == nil {
				nearestGit = d
			}
		}
		if nearestLang == "" {
			for _, marker := range languageMarkers {
				if _, statErr := os.Stat(filepath.Join(d, marker)); statErr == nil {
					nearestLang = d
					break
				}
			}
		}

		if nearestTaskuiRoot != "" {
			break
		}

		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}

	var path, marker string
	switch {
	case nearestTaskuiRoot != "":
		path, marker = nearestTaskuiRoot, "taskui-root"
	case nearestGit != "":
		path, marker = nearestGit, ".git"
	case nearestLang != "":
		path, marker = nearestLang, "language marker"
	default:
		return nil, nil
	}

	// Home-directory guard: a .git or language marker at $HOME (e.g. a
	// dotfiles repo) should not silently become a project. An explicit
	// .taskui-root at $HOME overrides the guard.
	if marker != "taskui-root" {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			resolvedHome, evalErr := filepath.EvalSymlinks(home)
			if evalErr != nil {
				resolvedHome = home
			}
			if path == resolvedHome {
				return nil, nil
			}
		}
	}

	name := filepath.Base(path)
	if marker == "taskui-root" {
		data, readErr := os.ReadFile(filepath.Join(path, ".taskui-root"))
		if readErr == nil {
			if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
				name = trimmed
			}
		}
	}

	gitRemote := ""
	if marker == ".git" {
		// Best-effort only: ignore any error or non-zero exit.
		out, cmdErr := exec.Command("git", "-C", path, "remote", "get-url", "origin").Output()
		if cmdErr == nil {
			gitRemote = strings.TrimRight(string(out), "\n")
		}
	}

	return &ProjectRoot{
		Path:      path,
		Name:      name,
		GitRemote: gitRemote,
		Marker:    marker,
	}, nil
}
