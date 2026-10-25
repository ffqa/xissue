package domain

import (
	"fmt"
	"regexp"
	"strings"
)

var repoNamePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}._ -]{0,63}$`)

// ValidateRepoName keeps repository names safe as single workspace directory
// components. Repo names are concatenated into run worktree paths, so separators,
// parent-directory names, and leading dots are rejected at every boundary.
func ValidateRepoName(name string) error {
	if name == "" {
		return fmt.Errorf("repository name is required")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("repository name must not begin or end with spaces")
	}
	if name == "" {
		return fmt.Errorf("repository name is required")
	}
	if name == "." || name == ".." || strings.EqualFold(name, ".git") {
		return fmt.Errorf("invalid repository name %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("repository name must not contain path separators")
	}
	if !repoNamePattern.MatchString(name) {
		return fmt.Errorf("invalid repository name %q (use letters, digits, spaces, dot, dash or underscore)", name)
	}
	return nil
}
