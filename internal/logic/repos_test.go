package logic

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoSlug(t *testing.T) {
	cases := map[string]string{
		"https://github.com/ACME/docker.git":            "ACME-docker",
		"https://github.com/ACME/docker":                "ACME-docker",
		"git@github.com:ACME/docker.git":                "ACME-docker",
		"ssh://git@github.com/ACME/docker.git":          "ACME-docker",
		"https://github.com/ACME-B/docker-images.git": "ACME-B-docker-images",
		"https://dev.azure.com/org/project/_git/repo":      "org-project-_git-repo",
	}
	for in, want := range cases {
		if got := RepoSlug(in); got != want {
			t.Errorf("RepoSlug(%q) = %q, atteso %q", in, got, want)
		}
	}
}

func TestSameRemote(t *testing.T) {
	if !sameRemote("https://github.com/ORG/repo.git", "git@github.com:ORG/repo") {
		t.Error("https e ssh dello stesso repo dovrebbero coincidere")
	}
	if sameRemote("https://github.com/ORG/repo", "https://gitlab.com/ORG/repo") {
		t.Error("host diversi non devono coincidere")
	}
}

// gitRun runs a git command in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newSourceRepo builds a repo standing in for origin, with master and coll branches.
func newSourceRepo(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	gitRun(t, src, "init", "-b", "master")
	writeTestFile(t, filepath.Join(src, "values.yaml"), "tag: 1.0.0\n")
	gitRun(t, src, "add", ".")
	gitRun(t, src, "commit", "-m", "init")
	gitRun(t, src, "checkout", "-b", "coll")
	writeTestFile(t, filepath.Join(src, "coll.txt"), "collaudo\n")
	gitRun(t, src, "add", ".")
	gitRun(t, src, "commit", "-m", "coll")
	gitRun(t, src, "checkout", "master")
	return src
}

func TestEnsureRepo(t *testing.T) {
	src := newSourceRepo(t)
	root := t.TempDir()

	dir, err := EnsureRepo(src, "master", root, nil)
	if err != nil {
		t.Fatalf("primo clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "values.yaml")); err != nil {
		t.Fatalf("values.yaml assente dopo il clone: %v", err)
	}

	// Branch switch inside the same managed clone.
	if _, err := EnsureRepo(src, "coll", root, nil); err != nil {
		t.Fatalf("checkout coll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "coll.txt")); err != nil {
		t.Fatalf("coll.txt assente dopo il checkout di coll: %v", err)
	}

	// Back to master: the coll-only file must be gone.
	if _, err := EnsureRepo(src, "master", root, nil); err != nil {
		t.Fatalf("ritorno su master: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "coll.txt")); err == nil {
		t.Error("coll.txt ancora presente dopo il ritorno su master")
	}

	// Leftovers from a previous run must be discarded.
	writeTestFile(t, filepath.Join(dir, "values.yaml"), "tag: MANOMESSO\n")
	writeTestFile(t, filepath.Join(dir, "scarto.txt"), "spazzatura\n")
	if _, err := EnsureRepo(src, "master", root, nil); err != nil {
		t.Fatalf("riallineamento: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "values.yaml"))
	if normalizeEOL(got) != "tag: 1.0.0\n" {
		t.Errorf("modifica locale non scartata: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "scarto.txt")); err == nil {
		t.Error("file non tracciato non rimosso da clean -fd")
	}

	// A push by somebody else is picked up without any manual pull.
	writeTestFile(t, filepath.Join(src, "values.yaml"), "tag: 2.0.0\n")
	gitRun(t, src, "add", ".")
	gitRun(t, src, "commit", "-m", "altrui")
	if _, err := EnsureRepo(src, "master", root, nil); err != nil {
		t.Fatalf("fetch delle modifiche altrui: %v", err)
	}
	got, _ = os.ReadFile(filepath.Join(dir, "values.yaml"))
	if normalizeEOL(got) != "tag: 2.0.0\n" {
		t.Errorf("commit altrui non recepito: %q", got)
	}
}

func TestListRemoteBranches(t *testing.T) {
	src := newSourceRepo(t)

	branches, err := ListRemoteBranches(src)
	if err != nil {
		t.Fatalf("ls-remote: %v", err)
	}

	// newSourceRepo publishes master and coll, in alphabetical order.
	want := []string{"coll", "master"}
	if len(branches) != len(want) {
		t.Fatalf("branch = %v, attesi %v", branches, want)
	}
	for i := range want {
		if branches[i] != want[i] {
			t.Errorf("branch[%d] = %q, atteso %q", i, branches[i], want[i])
		}
	}

	if _, err := ListRemoteBranches(filepath.Join(t.TempDir(), "inesistente")); err == nil {
		t.Error("remote inesistente: atteso errore")
	}
}

func TestEnsureRepoErrors(t *testing.T) {
	src := newSourceRepo(t)
	root := t.TempDir()

	// The sentinel lets PSN offer the branches the remote actually has instead
	// of stopping on a declaration that no longer matches.
	if _, err := EnsureRepo(src, "inesistente", root, nil); err == nil {
		t.Error("branch inesistente: atteso errore")
	} else {
		if !errors.Is(err, ErrBranchNotFound) {
			t.Errorf("atteso ErrBranchNotFound, ottenuto %v", err)
		}
		if !strings.Contains(err.Error(), "inesistente") {
			t.Errorf("messaggio poco chiaro: %v", err)
		}
	}

	if _, err := EnsureRepo(src, "", root, nil); err == nil {
		t.Error("branch vuoto: atteso errore")
	}

	// A directory that is not a git repo must never be clobbered.
	blocked := t.TempDir()
	dir := filepath.Join(blocked, RepoSlug(src))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "importante.txt"), "non toccare\n")
	if _, err := EnsureRepo(src, "master", blocked, nil); err == nil {
		t.Error("cartella non-git: atteso errore")
	}
	if _, err := os.Stat(filepath.Join(dir, "importante.txt")); err != nil {
		t.Error("la cartella non-git è stata toccata")
	}

	// A clone whose origin points elsewhere is a slug collision: refuse it.
	other := newSourceRepo(t)
	collision := t.TempDir()
	cloneDir := filepath.Join(collision, RepoSlug(src))
	cmd := exec.Command("git", "clone", other, cloneDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone di appoggio: %v\n%s", err, out)
	}
	if _, err := EnsureRepo(src, "master", collision, nil); err == nil {
		t.Error("origin diverso: atteso errore")
	}
}

// normalizeEOL hides the CRLF conversion git applies on checkout (core.autocrlf).
func normalizeEOL(b []byte) string {
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}
