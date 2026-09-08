package logic

import "testing"

func TestIncrementPatch(t *testing.T) {
	cases := map[string]string{
		"3.0.9-dev":     "3.0.10-dev", // il tag della webapp su Site A collaudo
		"1.0.0-dev":     "1.0.1-dev",
		"2.0.0":         "2.0.1",
		"1.0.2-staging": "1.0.3-staging",
		"0.9.99-dev":    "0.9.100-dev",
	}
	for in, want := range cases {
		got, err := IncrementPatch(in)
		if err != nil {
			t.Errorf("IncrementPatch(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("IncrementPatch(%q) = %q, atteso %q", in, got, want)
		}
	}
}

// A tag that is not X.Y.Z must fail rather than return something invented: the
// caller falls back to the current tag and lets the user type one.
func TestIncrementPatchRejectsNonSemver(t *testing.T) {
	for _, in := range []string{"dev", "latest", "1.0", "3.0.x-dev", ""} {
		if got, err := IncrementPatch(in); err == nil {
			t.Errorf("IncrementPatch(%q) = %q, atteso errore", in, got)
		}
	}
}
