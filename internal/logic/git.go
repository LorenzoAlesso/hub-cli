package logic

import (
	"fmt"
	"os"
	"os/exec"
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
