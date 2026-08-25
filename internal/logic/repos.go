package logic

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultReposRoot returns the directory holding the managed clones.
func DefaultReposRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("impossibile determinare la home directory: %w", err)
	}
	return filepath.Join(home, ".hub-cli", "repos"), nil
}

// ResolveReposRoot returns configured when set, otherwise the default root.
func ResolveReposRoot(configured string) (string, error) {
	if strings.TrimSpace(configured) == "" {
		return DefaultReposRoot()
	}
	abs, err := filepath.Abs(strings.TrimSpace(configured))
	if err != nil {
		return "", fmt.Errorf("percorso repos non valido %q: %w", configured, err)
	}
	return abs, nil
}

// GitRemoteURL returns the "origin" remote URL of the given repository, so a
// managed clone never needs its URL configured by hand.
func GitRemoteURL(repoPath string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoPath
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("impossibile leggere il remote origin di %s: %s", repoPath, detail)
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", fmt.Errorf("il repository %s non ha un remote origin", repoPath)
	}
	return url, nil
}

// normalizeRemoteURL reduces a remote URL to "host/path", dropping scheme,
// "user@" prefix and ".git" suffix, so HTTPS and SSH forms match. Case is
// preserved: callers compare case-insensitively.
func normalizeRemoteURL(remoteURL string) string {
	s := strings.TrimSpace(remoteURL)
	s = strings.ReplaceAll(s, "\\", "/")

	if idx := strings.Index(s, "://"); idx != -1 {
		s = s[idx+3:]
	}
	if idx := strings.Index(s, "@"); idx != -1 {
		s = s[idx+1:]
	}
	// scp-like syntax "host:ORG/repo". Index > 1 skips a Windows drive letter.
	if idx := strings.Index(s, ":"); idx > 1 {
		s = s[:idx] + "/" + s[idx+1:]
	}

	s = strings.Trim(s, "/")
	s = strings.TrimSuffix(s, ".git")
	return strings.Trim(s, "/")
}

// sameRemote reports whether two remote URLs point at the same repository.
func sameRemote(a, b string) bool {
	return strings.EqualFold(normalizeRemoteURL(a), normalizeRemoteURL(b))
}

// RepoSlug derives a stable directory name from a remote URL, so entries
// pointing at the same remote share one clone. Both "https://github.com/ORG/repo.git"
// and "git@github.com:ORG/repo.git" yield "ORG-repo".
func RepoSlug(remoteURL string) string {
	segments := strings.Split(normalizeRemoteURL(remoteURL), "/")

	// Drop the host: the path alone identifies the repository. A dotless
	// internal hostname stays in the slug, which is harmless.
	if len(segments) > 1 && strings.Contains(segments[0], ".") {
		segments = segments[1:]
	}

	var parts []string
	for _, seg := range segments {
		if cleaned := sanitizeSlugSegment(seg); cleaned != "" {
			parts = append(parts, cleaned)
		}
	}
	if len(parts) == 0 {
		return "repo"
	}
	return strings.Join(parts, "-")
}

// sanitizeSlugSegment keeps only characters that are safe in a directory name.
func sanitizeSlugSegment(segment string) string {
	var b strings.Builder
	for _, r := range segment {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}

// ManagedRepoPath returns where the managed clone of remoteURL lives, without
// creating or touching anything.
func ManagedRepoPath(reposRoot, remoteURL string) (string, error) {
	root, err := ResolveReposRoot(reposRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, RepoSlug(remoteURL)), nil
}

// EnsureRepo makes sure a managed clone of remoteURL exists under reposRoot and
// is checked out on branch at exactly origin/<branch>. Nobody edits these clones
// by hand, so anything a previous run left behind is discarded rather than
// preserved. Returns the path of the clone.
func EnsureRepo(remoteURL, branch, reposRoot string, out io.Writer) (string, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	branch = strings.TrimSpace(branch)
	if remoteURL == "" {
		return "", fmt.Errorf("URL del remote mancante: impossibile preparare il repo gestito")
	}
	if branch == "" {
		return "", fmt.Errorf("branch non specificato per il repo %s", remoteURL)
	}

	dir, err := ManagedRepoPath(reposRoot, remoteURL)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("creazione della cartella dei repo gestiti fallita: %w", err)
	}

	if isGitRepo(dir) {
		actual, err := GitRemoteURL(dir)
		if err != nil {
			return "", err
		}
		if !sameRemote(actual, remoteURL) {
			return "", fmt.Errorf(
				"il repo gestito %s è associato al remote %s invece di %s: rimuovere la cartella e rilanciare",
				dir, actual, remoteURL)
		}
	} else {
		if _, err := os.Stat(dir); err == nil {
			return "", fmt.Errorf("%s esiste ma non è un repository git: rimuovere la cartella e rilanciare", dir)
		}
		if err := cloneManagedRepo(remoteURL, dir, out); err != nil {
			return "", err
		}
	}

	if err := runGit(dir, out, "fetch", "origin", "--prune"); err != nil {
		return "", err
	}

	// Discard leftovers from the previous run before switching branch.
	if err := runGit(dir, nil, "reset", "--hard"); err != nil {
		return "", err
	}
	if err := runGit(dir, nil, "clean", "-fd"); err != nil {
		return "", err
	}

	if !remoteBranchExists(dir, branch) {
		return "", fmt.Errorf("il branch %q non esiste su origin (%s)", branch, remoteURL)
	}
	if err := runGit(dir, nil, "checkout", "-B", branch, "origin/"+branch); err != nil {
		return "", err
	}

	return dir, nil
}

// syncAttempts caps the realign → re-apply → push loop: three losses in a row
// mean a branch too busy for an automatic sync.
const syncAttempts = 3

// SyncToBranch applies a change to a managed clone and pushes it to branch. The
// clone is realigned and apply is invoked again on every attempt, so a concurrent
// push is neither merged nor overwritten: apply must therefore be idempotent and
// returns the paths it changed, or none when there was nothing to sync.
func SyncToBranch(
	remoteURL, branch, reposRoot, message string,
	apply func(repoDir string) ([]string, error),
	out io.Writer,
) error {
	var lastErr error

	for attempt := 1; attempt <= syncAttempts; attempt++ {
		dir, err := EnsureRepo(remoteURL, branch, reposRoot, out)
		if err != nil {
			return err
		}

		files, err := apply(dir)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return nil
		}

		switch err := GitCommitFiles(dir, message, files...); {
		case err == nil:
		case errors.Is(err, ErrNothingToCommit):
			return nil
		default:
			return err
		}

		err = GitPushBranch(dir, branch)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrPushRejected) {
			return err
		}
		lastErr = err
	}

	return fmt.Errorf(
		"sync su %s non riuscito dopo %d tentativi: %w (push concorrenti sullo stesso branch)",
		branch, syncAttempts, lastErr)
}

// cloneManagedRepo performs the first clone. A failure is almost always network
// or credentials, and the message says so.
func cloneManagedRepo(remoteURL, dir string, out io.Writer) error {
	var captured bytes.Buffer
	cmd := exec.Command("git", "clone", remoteURL, dir)
	cmd.Stdout = writerOrDiscard(out)
	cmd.Stderr = io.MultiWriter(writerOrDiscard(out), &captured)

	if err := cmd.Run(); err != nil {
		// Drop the partial result so the next run starts clean.
		_ = os.RemoveAll(dir)
		detail := strings.TrimSpace(captured.String())
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf(
			"clone di %s fallito: %s. Verificare connettività e credenziali git (SSH agent o credential manager)",
			remoteURL, detail)
	}
	return nil
}

// isGitRepo reports whether dir holds a git repository.
func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// remoteBranchExists reports whether origin/<branch> is known locally after a fetch.
func remoteBranchExists(dir, branch string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	cmd.Dir = dir
	return cmd.Run() == nil
}

// runGit executes a git subcommand in dir, streaming to out when non-nil.
// stderr is captured either way so failures carry git's own message.
func runGit(dir string, out io.Writer, args ...string) error {
	var captured bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out != nil {
		cmd.Stdout = out
		cmd.Stderr = io.MultiWriter(out, &captured)
	} else {
		cmd.Stderr = &captured
	}

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(captured.String())
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("git %s fallito in %s: %s", args[0], dir, detail)
	}
	return nil
}

func writerOrDiscard(out io.Writer) io.Writer {
	if out == nil {
		return io.Discard
	}
	return out
}
