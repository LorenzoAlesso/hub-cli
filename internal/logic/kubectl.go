package logic

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// CurrentKubeContext returns the currently active kubectl context.
func CurrentKubeContext() (string, error) {
	out, err := exec.Command("kubectl", "config", "current-context").Output()
	if err != nil {
		return "", fmt.Errorf("impossibile leggere il contesto kubectl: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ListKubeContexts returns all available kubectl context names.
func ListKubeContexts() ([]string, error) {
	out, err := exec.Command(
		"kubectl", "config", "get-contexts",
		"-o", "name",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("impossibile listare i contesti kubectl: %w", err)
	}
	var contexts []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			contexts = append(contexts, line)
		}
	}
	return contexts, nil
}

// SwitchKubeContext switches the active kubectl context.
func SwitchKubeContext(contextName string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("kubectl", "config", "use-context", contextName)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("impossibile cambiare contesto a %q: %s", contextName, stderr.String())
	}
	return nil
}
