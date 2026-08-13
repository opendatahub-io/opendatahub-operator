package e2escope

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// errNoDiffBase means no diff base could be determined: no PULL_BASE_SHA,
// and no upstream/main or origin/main to merge-base against. The caller
// must run the full suite, same as for any other unresolvable diff.
var errNoDiffBase = errors.New("could not determine a diff base")

// gitSHA matches a full or abbreviated hexadecimal commit object name.
// PULL_BASE_SHA reaches changedFiles as a git revision argument -- a value
// that isn't a plain SHA (e.g. one starting with "-") would be parsed as
// an option instead of a revision, so it's rejected here rather than
// trusted as-is.
var gitSHA = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// gitDiffBase picks the commit to diff against. PULL_BASE_SHA, set by Prow
// on every presubmit, takes priority, since a real origin remote doesn't
// exist in a Prow checkout. Locally it merge-bases against upstream/main
// first, then origin/main -- origin is often a personal fork that isn't
// kept in sync, so it can silently be behind the real main branch.
func gitDiffBase(ctx context.Context, repoRoot string) (string, error) {
	if base := os.Getenv("PULL_BASE_SHA"); base != "" {
		if !gitSHA.MatchString(base) {
			return "", fmt.Errorf("PULL_BASE_SHA %q is not a hexadecimal commit SHA", base)
		}
		return base, nil
	}

	if base, err := mergeBase(ctx, repoRoot, "upstream/main"); err == nil {
		return base, nil
	}
	if base, err := mergeBase(ctx, repoRoot, "origin/main"); err == nil {
		return base, nil
	}
	return "", errNoDiffBase
}

// mergeBase relies on `git merge-base` itself failing when ref doesn't
// resolve, rather than checking with a separate `rev-parse` first.
func mergeBase(ctx context.Context, repoRoot, ref string) (string, error) {
	out, err := runGit(ctx, repoRoot, "merge-base", "HEAD", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// errNoDiff means base and HEAD are identical, so there's nothing to
// classify. The caller must treat this the same as any other unresolvable
// diff: run the full suite.
var errNoDiff = errors.New("HEAD is the diff base -- no diff")

// changedFiles lists the paths that differ between base and HEAD.
func changedFiles(ctx context.Context, repoRoot, base string) ([]string, error) {
	head, err := runGit(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(head) == base {
		return nil, errNoDiff
	}

	out, err := runGit(ctx, repoRoot, "diff", "--name-only", base+"...HEAD", "--")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// runGit runs git with args from dir and returns stdout.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return stdout.String(), nil
}
