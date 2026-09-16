package logic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ErrReleaseNotFound means the release has never been installed in the namespace.
var ErrReleaseNotFound = errors.New("release non installato")

// ReleaseValues reads the values the deployed revision of a release runs with:
// the values file and every --set of the last upgrade, merged by Helm.
func ReleaseValues(releaseName, namespace string) (map[string]any, error) {
	var stderr bytes.Buffer
	cmd := exec.Command(
		"helm", "get", "values", releaseName,
		"--namespace", namespace,
		"-o", "json",
	)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if strings.Contains(detail, "release: not found") {
			return nil, ErrReleaseNotFound
		}
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("helm get values fallito: %s", detail)
	}

	var values map[string]any
	if err := json.Unmarshal(out, &values); err != nil {
		return nil, fmt.Errorf("parsing JSON helm values: %w", err)
	}
	return values, nil
}

// TagAt returns the tag stored at helmSetKey in release values. The key may
// point to a plain tag ("<path>.tag") or to a full image reference
// ("<path>.image", value "<prefix>/<name>:<tag>"); in the latter case only the
// tag portion is returned.
func TagAt(values map[string]any, helmSetKey string) (string, error) {
	raw, err := navigateJSON(values, helmSetKey)
	if err != nil {
		return "", err
	}

	var str string
	switch v := raw.(type) {
	case string:
		str = v
	case float64:
		// Numeric tags (e.g. "1.22") decode as float64 from Helm's JSON output.
		str = strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		str = strconv.Itoa(v)
	default:
		return "", fmt.Errorf("valore al path %q non è una stringa", helmSetKey)
	}

	// Full image reference ("<prefix>/<name>:<tag>"): return only the tag.
	if idx := strings.LastIndex(str, ":"); idx != -1 && strings.Contains(str[:idx], "/") {
		return str[idx+1:], nil
	}

	return str, nil
}

// GetDeployedTag returns the currently deployed tag for a Helm release.
func GetDeployedTag(releaseName, namespace, helmSetKey string) (string, error) {
	values, err := ReleaseValues(releaseName, namespace)
	if err != nil {
		return "", err
	}
	return TagAt(values, helmSetKey)
}

// GetDeployedChartVersion returns the chart version currently deployed for a release.
// Parses the "chart" field of `helm list` (format "<name>-<version>").
func GetDeployedChartVersion(releaseName, namespace string) (string, error) {
	out, err := exec.Command(
		"helm", "list",
		"--namespace", namespace,
		"--filter", "^"+releaseName+"$",
		"-o", "json",
	).Output()
	if err != nil {
		return "", fmt.Errorf("helm list fallito: %w", err)
	}

	var releases []struct {
		Chart string `json:"chart"`
	}
	if err := json.Unmarshal(out, &releases); err != nil {
		return "", fmt.Errorf("parsing helm list: %w", err)
	}
	if len(releases) == 0 {
		return "", fmt.Errorf("release %q non trovata", releaseName)
	}

	// Version is the segment after the last "-" if it starts with a digit.
	chart := releases[0].Chart
	if idx := strings.LastIndex(chart, "-"); idx != -1 {
		v := chart[idx+1:]
		if len(v) > 0 && v[0] >= '0' && v[0] <= '9' {
			return v, nil
		}
	}
	return "", fmt.Errorf("impossibile estrarre la versione dal chart %q", chart)
}

// HelmGetValuesCommandLine renders the command ReleaseValues would run, for --dry-run.
func HelmGetValuesCommandLine(releaseName, namespace string) string {
	return fmt.Sprintf("helm get values %s --namespace %s -o json", releaseName, namespace)
}

func navigateJSON(data map[string]any, dotPath string) (any, error) {
	parts := strings.SplitN(dotPath, ".", 2)
	val, ok := data[parts[0]]
	if !ok {
		return nil, fmt.Errorf("chiave %q non trovata nei valori helm", parts[0])
	}
	if len(parts) == 1 {
		return val, nil
	}
	nested, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("percorso %q: %q non è un oggetto", dotPath, parts[0])
	}
	return navigateJSON(nested, parts[1])
}
