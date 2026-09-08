package ui

import "testing"

// The branch changes with the site being worked on, so the picker must open on
// the most likely answer: the one used last, then the working copy branch.
func TestDefaultBranchIndex(t *testing.T) {
	branches := []string{"dev", "dev-site-b", "dev-site-a", "dev-psn", "master"}

	cases := []struct {
		name        string
		lastUsed    string
		workingCopy string
		want        int
	}{
		{"ultimo usato vince", "dev-site-a", "dev-site-b", 2},
		{"senza ultimo usato vale la copia di lavoro", "", "dev-site-b", 1},
		{"ultimo usato non più esistente: ripiega sulla copia", "dev-rimosso", "dev-psn", 3},
		{"nessuno dei due valido", "dev-rimosso", "altro-rimosso", 0},
		{"entrambi vuoti", "", "", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wfDefaultBranchIndex(branches, tc.lastUsed, tc.workingCopy); got != tc.want {
				t.Errorf("indice = %d (%s), atteso %d (%s)",
					got, branches[got], tc.want, branches[tc.want])
			}
		})
	}
}
