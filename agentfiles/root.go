package agentfiles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// RootEnv names the environment variable that chooses the root directory.
const RootEnv = "DZZR_FILES_ROOT"

// RootFromEnv returns the directory the tools may read, taken from
// DZZR_FILES_ROOT or, when it is unset, the working directory.
func RootFromEnv() (string, error) {
	root := strings.TrimSpace(os.Getenv(RootEnv))
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = wd
	}
	return resolveRoot(root)
}

// resolveRoot turns a root into the absolute, symlink-free path every later
// check is made against.
func resolveRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	// A root that is itself reached through a symlink would fail every
	// containment check below, because the resolved target of a file inside it
	// does not start with the link's path.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

// errOutsideRoot is returned for any path that leaves the root.
var errOutsideRoot = errors.New("путь за пределами " + RootEnv)

// resolve turns a path from the model into an absolute path inside the root.
// A relative path is taken from the root; an absolute one is accepted only if
// it already points inside it.
func resolve(root, userPath string) (string, error) {
	if strings.TrimSpace(userPath) == "" {
		userPath = "."
	}
	clean := filepath.Clean(userPath)
	abs := clean
	if !filepath.IsAbs(clean) {
		abs = filepath.Join(root, clean)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if !within(root, abs) {
		return "", errOutsideRoot
	}
	// The lexical check above is blind to a symlink inside the root that
	// points out of it, so an existing target is checked again once resolved.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && !within(root, resolved) {
		return "", errOutsideRoot
	}
	return abs, nil
}

// within reports whether path is the root itself or something under it.
func within(root, path string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}
