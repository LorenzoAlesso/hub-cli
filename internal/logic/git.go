package logic

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GitAdd stages specific files in the given repository.
func GitAdd(repoPath string, files ...string) error {
	args := append([]string{"add", "--"}, files...)
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git add fallito in %s: %w", repoPath, err)
	}
	return nil
}

// GitCommit creates a conventional commit in the given repository.
// Expected format: "chore(deploy): <service> → <new-tag>".
func GitCommit(repoPath, message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git commit fallito in %s: %w", repoPath, err)
	}
	return nil
}

// GitPush pushes the current branch to origin.
func GitPush(repoPath string) error {
	cmd := exec.Command("git", "push")
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git push fallito in %s: %w", repoPath, err)
	}
	return nil
}

// DeployCommitMessage builds a deploy commit message: "chore(deploy): <service> → <new-tag>".
func DeployCommitMessage(serviceName, newTag string) string {
	return fmt.Sprintf("chore(deploy): %s → %s", serviceName, newTag)
}

// GitCurrentBranch returns the checked-out branch of the given repository.
func GitCurrentBranch(repoPath string) (string, error) {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("impossibile leggere il branch di %s: %w", repoPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GitIsClean reports whether the repository has no staged or unstaged changes.
// Untracked files are ignored: they don't block a checkout.
func GitIsClean(repoPath string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain", "--untracked-files=no")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("impossibile leggere lo stato di %s: %w", repoPath, err)
	}
	return strings.TrimSpace(string(out)) == "", nil
}

// GitCheckout switches the repository to the given branch.
func GitCheckout(repoPath, branch string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("git", "checkout", branch)
	cmd.Dir = repoPath
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git checkout %s fallito in %s: %s", branch, repoPath, strings.TrimSpace(stderr.String()))
	}
	return nil
}
