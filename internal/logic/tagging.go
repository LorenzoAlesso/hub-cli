package logic

import (
	"fmt"
	"strconv"
	"strings"
)

// IncrementPatch bumps the patch component of a semver-ish tag.
// Example: "2.0.0-dev" → "2.0.1-dev". Any "-<suffix>" is preserved.
func IncrementPatch(tag string) (string, error) {
	suffix := ""
	versionPart := tag

	if idx := strings.Index(tag, "-"); idx != -1 {
		versionPart = tag[:idx]
		suffix = tag[idx:] // includes the leading "-"
	}

	parts := strings.Split(versionPart, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("formato tag non valido: %q (atteso X.Y.Z o X.Y.Z-suffix)", tag)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", fmt.Errorf("major non numerico in tag %q", tag)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("minor non numerico in tag %q", tag)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", fmt.Errorf("patch non numerica in tag %q", tag)
	}

	return fmt.Sprintf("%d.%d.%d%s", major, minor, patch+1, suffix), nil
}
