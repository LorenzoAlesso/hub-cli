package logic

import "testing"

func TestIncrementPatch(t *testing.T) {
	cases := map[string]string{
		"3.0.9-dev":     "3.0.10-dev", // the patch gains a digit
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

// The proposal steps past whatever the registry already holds in the same
// series, so it never names an image that exists; other series do not count.
func TestNextTagSkipsTagsInTheRegistry(t *testing.T) {
	registry := []string{"3.0.10-dev", "3.0.11-dev", "3.0.13-dev", "3.1.0-dev", "3.0.20-staging", "latest", "dev"}
	cases := map[string]string{
		"3.0.11-dev": "3.0.14-dev", // an image pushed after the deployed one owns 3.0.13
		"3.0.13-dev": "3.0.14-dev",
		"3.1.0-dev":  "3.1.1-dev",
		"2.0.0-dev":  "2.0.1-dev",
	}
	for current, want := range cases {
		got, err := NextTag(current, registry)
		if err != nil {
			t.Errorf("NextTag(%q): %v", current, err)
			continue
		}
		if got != want {
			t.Errorf("NextTag(%q) = %q, atteso %q", current, got, want)
		}
	}

	if got, _ := NextTag("3.0.11-dev", nil); got != "3.0.12-dev" {
		t.Errorf("senza registry: %q, atteso 3.0.12-dev", got)
	}
	if _, err := NextTag("latest", registry); err == nil {
		t.Error("un tag non X.Y.Z deve restituire errore")
	}
}

func TestHighestInSeries(t *testing.T) {
	registry := []string{"3.0.9-dev", "3.0.12-dev", "3.0.10-dev", "3.1.4-dev", "latest"}
	cases := map[string]string{
		"3.0.9-dev":     "3.0.12-dev",
		"3.1.0-dev":     "3.1.4-dev",
		"3.0.9-staging": "",
		"latest":        "",
	}
	for current, want := range cases {
		if got := HighestInSeries(current, registry); got != want {
			t.Errorf("HighestInSeries(%q) = %q, atteso %q", current, got, want)
		}
	}
}
