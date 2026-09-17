package logic

import (
	"reflect"
	"testing"
)

// The branch list is read from the clone, not asked of the remote: after
// EnsureRepo it has to match what origin publishes, without origin/HEAD.
func TestRemoteBranchesReadsTheClone(t *testing.T) {
	dir, err := EnsureRepo(newSourceRepo(t), "master", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := RemoteBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"coll", "master"}; !reflect.DeepEqual(got, want) {
		t.Errorf("branch = %v, attesi %v", got, want)
	}
}

// A file present on one branch only is found there and nowhere else — even
// while the clone is checked out on a branch that does not have it, which is
// exactly the moment the question gets asked.
func TestBranchesContainingFindsOtherBranches(t *testing.T) {
	dir, err := EnsureRepo(newSourceRepo(t), "master", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string][]string{
		"coll.txt":    {"coll"},
		"values.yaml": {"coll", "master"},
		"assente.txt": nil,
	}
	for path, want := range cases {
		got, err := BranchesContaining(dir, path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: presente su %v, attesi %v", path, got, want)
		}
	}
}
