package logic

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitOutput runs a git command and returns its stdout.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

// newBareOrigin builds a pushable repository standing in for origin, holding
// values.yaml on master.
func newBareOrigin(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	bare := filepath.Join(base, "origin.git")
	gitRun(t, base, "init", "--bare", "-b", "master", "origin.git")

	seed := t.TempDir()
	gitRun(t, seed, "init", "-b", "master")
	writeTestFile(t, filepath.Join(seed, "values.yaml"), "tag: 1.0.0\n")
	writeTestFile(t, filepath.Join(seed, "altro.txt"), "roba\n")
	gitRun(t, seed, "add", ".")
	gitRun(t, seed, "commit", "-m", "init")
	gitRun(t, seed, "remote", "add", "origin", bare)
	gitRun(t, seed, "push", "origin", "master")
	return bare
}

// pushCompeting simulates a colleague pushing to the same branch.
func pushCompeting(t *testing.T, origin, name, content string) {
	t.Helper()
	work := t.TempDir()
	gitRun(t, work, "clone", origin, ".")
	writeTestFile(t, filepath.Join(work, name), content)
	gitRun(t, work, "add", ".")
	gitRun(t, work, "commit", "-m", "lavoro di un altro")
	gitRun(t, work, "push", "origin", "master")
}

func readFromOrigin(t *testing.T, origin, name string) (string, bool) {
	t.Helper()
	work := t.TempDir()
	gitRun(t, work, "clone", origin, ".")
	b, err := os.ReadFile(filepath.Join(work, name))
	if err != nil {
		return "", false
	}
	return normalizeEOL(b), true
}

// setTag is the idempotent apply used by the sync: it states the wanted result
// starting from whatever content it is given, never a diff.
func setTag(t *testing.T, dir, tag string) []string {
	t.Helper()
	path := filepath.Join(dir, "values.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(normalizeEOL(raw), "\n") {
		if strings.HasPrefix(line, "tag: ") {
			line = "tag: " + tag
		}
		out = append(out, line)
	}
	writeTestFile(t, path, strings.Join(out, "\n"))
	return []string{"values.yaml"}
}

func TestGitCommitFilesIgnoresTheRestOfTheIndex(t *testing.T) {
	origin := newBareOrigin(t)
	root := t.TempDir()
	dir, err := EnsureRepo(origin, "master", root, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Work already staged by somebody else must not be swept into our commit.
	writeTestFile(t, filepath.Join(dir, "altro.txt"), "modifica non nostra\n")
	gitRun(t, dir, "add", "altro.txt")
	setTag(t, dir, "2.0.0")

	if err := GitCommitFiles(dir, "chore(deploy): svc → 2.0.0", "values.yaml"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	out := gitOutput(t, dir, "show", "--name-only", "--format=", "HEAD")
	touched := strings.Fields(out)
	if len(touched) != 1 || touched[0] != "values.yaml" {
		t.Errorf("il commit tocca %v, atteso solo [values.yaml]", touched)
	}
}

func TestGitCommitFilesNothingToCommit(t *testing.T) {
	origin := newBareOrigin(t)
	root := t.TempDir()
	dir, err := EnsureRepo(origin, "master", root, nil)
	if err != nil {
		t.Fatal(err)
	}

	err = GitCommitFiles(dir, "nessuna modifica", "values.yaml")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Errorf("atteso ErrNothingToCommit, ottenuto %v", err)
	}
	if err := GitCommitFiles(dir, "senza file"); !errors.Is(err, ErrNothingToCommit) {
		t.Errorf("nessun file: atteso ErrNothingToCommit, ottenuto %v", err)
	}
}

func TestGitPushBranchTargetsTheNamedBranch(t *testing.T) {
	origin := newBareOrigin(t)
	root := t.TempDir()
	dir, err := EnsureRepo(origin, "master", root, nil)
	if err != nil {
		t.Fatal(err)
	}

	setTag(t, dir, "3.0.0")
	if err := GitCommitFiles(dir, "chore(deploy): svc → 3.0.0", "values.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := GitPushBranch(dir, "master"); err != nil {
		t.Fatalf("push: %v", err)
	}

	got, ok := readFromOrigin(t, origin, "values.yaml")
	if !ok || got != "tag: 3.0.0\n" {
		t.Errorf("origin ha %q", got)
	}
}

func TestGitPushBranchDetectsRejection(t *testing.T) {
	origin := newBareOrigin(t)
	root := t.TempDir()
	dir, err := EnsureRepo(origin, "master", root, nil)
	if err != nil {
		t.Fatal(err)
	}

	setTag(t, dir, "4.0.0")
	if err := GitCommitFiles(dir, "chore(deploy): svc → 4.0.0", "values.yaml"); err != nil {
		t.Fatal(err)
	}
	pushCompeting(t, origin, "note.txt", "arrivato prima\n")

	if err := GitPushBranch(dir, "master"); !errors.Is(err, ErrPushRejected) {
		t.Errorf("atteso ErrPushRejected, ottenuto %v", err)
	}
}

func TestSyncToBranch(t *testing.T) {
	origin := newBareOrigin(t)
	root := t.TempDir()

	err := SyncToBranch(origin, "master", root, "chore(deploy): svc → 2.0.0",
		func(dir string) ([]string, error) { return setTag(t, dir, "2.0.0"), nil }, nil)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	got, _ := readFromOrigin(t, origin, "values.yaml")
	if got != "tag: 2.0.0\n" {
		t.Errorf("origin ha %q, atteso tag: 2.0.0", got)
	}
}

// Somebody else pushes between our fetch and our push: their commit must
// survive and ours must still land.
func TestSyncToBranchSurvivesConcurrentPush(t *testing.T) {
	origin := newBareOrigin(t)
	root := t.TempDir()

	attempts := 0
	err := SyncToBranch(origin, "master", root, "chore(deploy): svc → 2.0.0",
		func(dir string) ([]string, error) {
			attempts++
			if attempts == 1 {
				pushCompeting(t, origin, "collega.txt", "il mio lavoro\n")
			}
			return setTag(t, dir, "2.0.0"), nil
		}, nil)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if attempts != 2 {
		t.Errorf("tentativi = %d, attesi 2 (il primo perde la race)", attempts)
	}

	got, _ := readFromOrigin(t, origin, "values.yaml")
	if got != "tag: 2.0.0\n" {
		t.Errorf("il nostro tag non è arrivato: %q", got)
	}
	if _, ok := readFromOrigin(t, origin, "collega.txt"); !ok {
		t.Error("il commit del collega è stato perso")
	}
}

func TestSyncToBranchGivesUpAfterRepeatedRaces(t *testing.T) {
	origin := newBareOrigin(t)
	root := t.TempDir()

	attempts := 0
	err := SyncToBranch(origin, "master", root, "chore(deploy): svc → 2.0.0",
		func(dir string) ([]string, error) {
			attempts++
			pushCompeting(t, origin, "collega.txt", strings.Repeat("x", attempts))
			return setTag(t, dir, "2.0.0"), nil
		}, nil)

	if err == nil {
		t.Fatal("atteso errore dopo tentativi ripetuti")
	}
	if attempts != syncAttempts {
		t.Errorf("tentativi = %d, attesi %d", attempts, syncAttempts)
	}
	if !errors.Is(err, ErrPushRejected) {
		t.Errorf("l'errore deve conservare la causa: %v", err)
	}
}

func TestSyncToBranchNoChangeIsNotAnError(t *testing.T) {
	origin := newBareOrigin(t)
	root := t.TempDir()

	// Redeploying the same tag leaves the file untouched: a no-op, not a failure.
	err := SyncToBranch(origin, "master", root, "chore(deploy): svc → 1.0.0",
		func(dir string) ([]string, error) { return setTag(t, dir, "1.0.0"), nil }, nil)
	if err != nil {
		t.Errorf("tag invariato: atteso nessun errore, ottenuto %v", err)
	}

	err = SyncToBranch(origin, "master", root, "niente da fare",
		func(dir string) ([]string, error) { return nil, nil }, nil)
	if err != nil {
		t.Errorf("nessun file da sincronizzare: atteso nessun errore, ottenuto %v", err)
	}
}

func TestDeployCommitMessageFor(t *testing.T) {
	one := DeployCommitMessageFor([]DeployedService{{Name: "app-webapp", Tag: "1.2.3-dev"}})
	if one != "chore(deploy): app-webapp → 1.2.3-dev\n\nDeploy eseguito con hub-cli." {
		t.Errorf("messaggio singolo = %q", one)
	}

	many := DeployCommitMessageFor([]DeployedService{
		{Name: "app-webapp", Tag: "1.2.3-dev"},
		{Name: "app-consul", Tag: "2.0.1-dev"},
	})
	if many != "chore(deploy): app-webapp → 1.2.3-dev, app-consul → 2.0.1-dev\n\nDeploy eseguito con hub-cli." {
		t.Errorf("messaggio multiplo = %q", many)
	}

	// The subject line stays the summary: it is what shows up in git log.
	if subject := strings.SplitN(many, "\n", 2)[0]; strings.Contains(subject, "hub-cli") {
		t.Errorf("il trailer non deve finire nella prima riga: %q", subject)
	}
}
