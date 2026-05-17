package logic

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GetDeployedTag returns the currently deployed tag for a Helm release.
// helmSetKey may point to a plain tag ("<path>.tag") or to a full image
// reference ("<path>.image", value "<prefix>/<name>:<tag>"); in the latter
// case only the tag portion is returned.
func GetDeployedTag(releaseName, namespace, helmSetKey string) (string, error) {
	out, err := exec.Command(
		"helm", "get", "values", releaseName,
		"--namespace", namespace,
		"-o", "json",
	).Output()
	if err != nil {
		return "", fmt.Errorf("helm get values fallito: %w", err)
	}

	var values map[string]interface{}
	if err := json.Unmarshal(out, &values); err != nil {
		return "", fmt.Errorf("parsing JSON helm values: %w", err)
	}

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

func navigateJSON(data map[string]interface{}, dotPath string) (interface{}, error) {
	parts := strings.SplitN(dotPath, ".", 2)
	val, ok := data[parts[0]]
	if !ok {
		return nil, fmt.Errorf("chiave %q non trovata nei valori helm", parts[0])
	}
	if len(parts) == 1 {
		return val, nil
	}
	nested, ok := val.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("percorso %q: %q non è un oggetto", dotPath, parts[0])
	}
	return navigateJSON(nested, parts[1])
}
