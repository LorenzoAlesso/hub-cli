package logic

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
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

// KubectlUnsetCurrentContext detaches kubectl from any cluster: subsequent
// kubectl calls fail with an explicit error until a context is selected.
func KubectlUnsetCurrentContext() error {
	var stderr bytes.Buffer
	cmd := exec.Command("kubectl", "config", "unset", "current-context")
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("impossibile sganciare il contesto: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

// rolloutTimeout caps the wait for a restarted pod to become ready. JBoss takes
// tens of seconds to boot, so the limit is generous: it exists to end the wait
// on a pod that will never come up, not to time a normal restart.
const rolloutTimeout = 10 * time.Minute

// RolloutRestart recreates the pods of a deployment. Redeploying an unchanged
// tag overwrites the image in the registry but renders an identical manifest:
// Kubernetes sees no difference and keeps running the old image. The command
// patches the kubectl.kubernetes.io/restartedAt annotation on the pod template,
// so the manifest does change and the rollout starts on its own.
func RolloutRestart(deployment, namespace string, out io.Writer) error {
	cmd := exec.Command("kubectl", "rollout", "restart", "deployment/"+deployment, "--namespace", namespace)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rollout restart di %s fallito: %w", deployment, err)
	}
	return nil
}

// RolloutStatus waits for a rollout to complete, so a pod that never comes back
// is reported instead of hiding behind a deploy that looked successful.
func RolloutStatus(deployment, namespace string, out io.Writer) error {
	cmd := exec.Command(
		"kubectl", "rollout", "status", "deployment/"+deployment,
		"--namespace", namespace,
		"--timeout", rolloutTimeout.String(),
	)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rollout di %s non completato: %w", deployment, err)
	}
	return nil
}

// RestartDeployments restarts each deployment and waits for its rollout before
// moving to the next: restarting everything at once would take down several
// services together, and the first failure would be lost in the noise.
func RestartDeployments(deployments []string, namespace string, out io.Writer) error {
	for _, name := range deployments {
		if err := RolloutRestart(name, namespace, out); err != nil {
			return err
		}
		if err := RolloutStatus(name, namespace, out); err != nil {
			return err
		}
	}
	return nil
}

// RolloutRestartCommandLine renders the command RolloutRestart would run, for --dry-run.
func RolloutRestartCommandLine(deployment, namespace string) string {
	return fmt.Sprintf("kubectl rollout restart deployment/%s --namespace %s", deployment, namespace)
}
