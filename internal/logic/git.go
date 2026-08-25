package logic

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNothingToCommit means a redeploy of the same tag left the files unchanged.
var ErrNothingToCommit = errors.New("nessuna modifica da committare")

// ErrPushRejected means origin moved ahead: the signal to realign and re-apply.
var ErrPushRejected = errors.New("push rifiutato: il branch remoto è più avanti")

// GitCommitFiles commits the working-tree state of the given files and nothing
// else: unlike a plain "git commit", the pathspec form cannot sweep in
// unrelated staged work. Returns ErrNothingToCommit when they already match.
func GitCommitFiles(repoPath, message string, files ...string) error {
	if len(files) == 0 {
		return ErrNothingToCommit
	}

	args := append([]string{"commit", "-m", message, "--"}, files...)
	var combined bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(combined.String())
		if strings.Contains(detail, "nothing to commit") ||
			strings.Contains(detail, "no changes added to commit") {
			return ErrNothingToCommit
		}
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("git commit fallito in %s: %s", repoPath, detail)
	}
	return nil
}

// GitPushBranch pushes HEAD to the named branch on origin. The refspec is
// explicit so the push depends neither on an upstream nor on the checked-out
// branch. Returns ErrPushRejected when origin moved ahead.
func GitPushBranch(repoPath, branch string) error {
	var combined bytes.Buffer
	cmd := exec.Command("git", "push", "origin", "HEAD:refs/heads/"+branch)
	cmd.Dir = repoPath
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(combined.String())
		lower := strings.ToLower(detail)
		if strings.Contains(lower, "rejected") ||
			strings.Contains(lower, "non-fast-forward") ||
			strings.Contains(lower, "fetch first") {
			return ErrPushRejected
		}
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("git push su %s fallito: %s", branch, detail)
	}
	return nil
}

// DeployedService pairs a deployed service with the tag it went out with.
type DeployedService struct {
	Name string
	Tag  string
}

// DeployCommitMessageFor builds one message for every service deployed in a
// run: a single commit per workflow means fewer pushes and fewer races.
func DeployCommitMessageFor(services []DeployedService) string {
	parts := make([]string, 0, len(services))
	for _, svc := range services {
		parts = append(parts, fmt.Sprintf("%s → %s", svc.Name, svc.Tag))
	}
	return "chore(deploy): " + strings.Join(parts, ", ")
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
