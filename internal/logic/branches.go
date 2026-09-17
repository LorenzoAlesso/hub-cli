package logic

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// RemoteBranches lists the branches origin had at the last fetch in repoDir,
// read from the local refs rather than asked of the remote. On a managed clone
// that fetch is the one EnsureRepo has just made, so the answer is current
// without another round trip over the network.
func RemoteBranches(repoDir string) ([]string, error) {
	out, err := exec.Command("git", "-C", repoDir,
		"for-each-ref", "--format=%(refname)", "refs/remotes/origin").Output()
	if err != nil {
		return nil, fmt.Errorf("lettura dei branch di origin in %s: %w", repoDir, err)
	}

	var branches []string
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimPrefix(strings.TrimSpace(line), "refs/remotes/origin/")
		// origin/HEAD points at another branch, it is not a branch of its own.
		if name == "" || name == "HEAD" || strings.HasPrefix(name, "refs/") {
			continue
		}
		branches = append(branches, name)
	}
	sort.Strings(branches)
	return branches, nil
}

// BranchesContaining returns the branches of origin whose tree has relPath. It
// answers the question a missing Dockerfile raises — which branch is it on? —
// from the local refs, and asks git about every branch in a single process: a
// repository with dozens of site branches would otherwise mean dozens of calls.
func BranchesContaining(repoDir, relPath string) ([]string, error) {
	branches, err := RemoteBranches(repoDir)
	if err != nil || len(branches) == 0 {
		return nil, err
	}

	path := strings.TrimPrefix(filepath.ToSlash(relPath), "./")
	var query strings.Builder
	for _, b := range branches {
		query.WriteString("refs/remotes/origin/" + b + ":" + path + "\n")
	}

	cmd := exec.Command("git", "-C", repoDir, "cat-file", "--batch-check")
	cmd.Stdin = strings.NewReader(query.String())
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ricerca di %s nei branch di origin: %w", path, err)
	}

	// --batch-check answers one line per query, in the same order, ending in
	// " missing" when the path is not in that branch's tree.
	var found []string
	for i, line := range strings.Split(strings.TrimRight(string(out), "\r\n"), "\n") {
		if i < len(branches) && !strings.HasSuffix(strings.TrimRight(line, "\r"), " missing") {
			found = append(found, branches[i])
		}
	}
	return found, nil
}
