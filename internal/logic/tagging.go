package logic

import (
	"fmt"
	"strconv"
	"strings"
)

// semverTag is a tag of the form X.Y.Z with an optional "-<suffix>".
type semverTag struct {
	major, minor, patch int
	suffix              string // includes the leading "-"
}

func parseTag(tag string) (semverTag, error) {
	versionPart, suffix := tag, ""
	if idx := strings.Index(tag, "-"); idx != -1 {
		versionPart, suffix = tag[:idx], tag[idx:]
	}

	parts := strings.Split(versionPart, ".")
	if len(parts) != 3 {
		return semverTag{}, fmt.Errorf("formato tag non valido: %q (atteso X.Y.Z o X.Y.Z-suffix)", tag)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semverTag{}, fmt.Errorf("major non numerico in tag %q", tag)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semverTag{}, fmt.Errorf("minor non numerico in tag %q", tag)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semverTag{}, fmt.Errorf("patch non numerica in tag %q", tag)
	}
	return semverTag{major: major, minor: minor, patch: patch, suffix: suffix}, nil
}

func (t semverTag) String() string {
	return fmt.Sprintf("%d.%d.%d%s", t.major, t.minor, t.patch, t.suffix)
}

// sameSeries reports whether two tags differ only by their patch.
func (t semverTag) sameSeries(o semverTag) bool {
	return t.major == o.major && t.minor == o.minor && t.suffix == o.suffix
}

// IncrementPatch bumps the patch component of a semver-ish tag.
// Example: "2.0.0-dev" → "2.0.1-dev". Any "-<suffix>" is preserved.
func IncrementPatch(tag string) (string, error) {
	t, err := parseTag(tag)
	if err != nil {
		return "", err
	}
	t.patch++
	return t.String(), nil
}

// NextTag proposes the tag after current: its patch bumped, and past every tag
// of the same series already in the registry. An image pushed and never
// deployed still owns its tag, and proposing it again would overwrite it.
// Tags of other series, or not in X.Y.Z form, are ignored.
func NextTag(current string, existing []string) (string, error) {
	next, err := parseTag(current)
	if err != nil {
		return "", err
	}
	next.patch++

	for _, tag := range existing {
		t, err := parseTag(tag)
		if err != nil || !t.sameSeries(next) {
			continue
		}
		if t.patch >= next.patch {
			next.patch = t.patch + 1
		}
	}
	return next.String(), nil
}

// HighestInSeries returns the highest tag in existing of the same series as
// current, or "" when there is none.
func HighestInSeries(current string, existing []string) string {
	ref, err := parseTag(current)
	if err != nil {
		return ""
	}

	best, found := semverTag{}, false
	for _, tag := range existing {
		t, err := parseTag(tag)
		if err != nil || !t.sameSeries(ref) {
			continue
		}
		if !found || t.patch > best.patch {
			best, found = t, true
		}
	}
	if !found {
		return ""
	}
	return best.String()
}
