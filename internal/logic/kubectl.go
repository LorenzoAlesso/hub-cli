package logic

import (
	"bytes"
	"encoding/json"
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

// systemNamespaces are infrastructural namespaces (Kubernetes, AKS add-ons,
// CNI, ingress) hidden from the interactive namespace selection.
var systemNamespaces = map[string]bool{
	"default":            true,
	"calico-system":      true,
	"calico-apiserver":   true,
	"tigera-operator":    true,
	"gatekeeper-system":  true,
	"aks-command":        true,
	"app-routing-system": true,
	"ingress-nginx":      true,
	"cert-manager":       true,
}

// IsSystemNamespace reports whether ns is infrastructural rather than applicative.
func IsSystemNamespace(ns string) bool {
	return systemNamespaces[ns] || strings.HasPrefix(ns, "kube-")
}

// ListAppNamespaces returns the cluster's namespaces, excluding system ones.
func ListAppNamespaces() ([]string, error) {
	out, err := exec.Command("kubectl", "get", "namespaces", "-o", "name").Output()
	if err != nil {
		return nil, fmt.Errorf("impossibile listare i namespace: %w", err)
	}
	var namespaces []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		ns := strings.TrimPrefix(strings.TrimSpace(line), "namespace/")
		if ns == "" || IsSystemNamespace(ns) {
			continue
		}
		namespaces = append(namespaces, ns)
	}
	return namespaces, nil
}

// DeploymentInfo describes a deployment and its first container's image.
type DeploymentInfo struct {
	Name       string
	Container  string // first container name (set image target)
	Image      string // full reference "registry/path:tag"
	Containers int    // total containers in the pod template
}

// ListDeployments returns the deployments in a namespace with their images.
func ListDeployments(namespace string) ([]DeploymentInfo, error) {
	out, err := exec.Command("kubectl", "get", "deployments", "-n", namespace, "-o", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("impossibile listare i deployment in %q: %w", namespace, err)
	}

	var parsed struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Name  string `json:"name"`
							Image string `json:"image"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parsing JSON deployment: %w", err)
	}

	var deployments []DeploymentInfo
	for _, item := range parsed.Items {
		containers := item.Spec.Template.Spec.Containers
		if len(containers) == 0 {
			continue
		}
		deployments = append(deployments, DeploymentInfo{
			Name:       item.Metadata.Name,
			Container:  containers[0].Name,
			Image:      containers[0].Image,
			Containers: len(containers),
		})
	}
	return deployments, nil
}

// KubectlSetImage updates a container image on a deployment (triggers a rollout).
func KubectlSetImage(namespace, deployment, container, image string, out io.Writer) error {
	cmd := exec.Command(
		"kubectl", "set", "image",
		"deployment/"+deployment,
		container+"="+image,
		"-n", namespace,
	)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl set image fallito: %w", err)
	}
	return nil
}

// KubectlRolloutStatus waits until the deployment rollout completes or times out.
func KubectlRolloutStatus(namespace, deployment string, timeout time.Duration, out io.Writer) error {
	cmd := exec.Command(
		"kubectl", "rollout", "status",
		"deployment/"+deployment,
		"-n", namespace,
		"--timeout", fmt.Sprintf("%ds", int(timeout.Seconds())),
	)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rollout non completato: %w", err)
	}
	return nil
}

// KubectlRolloutUndo reverts a deployment to its previous revision.
func KubectlRolloutUndo(namespace, deployment string, out io.Writer) error {
	cmd := exec.Command(
		"kubectl", "rollout", "undo",
		"deployment/"+deployment,
		"-n", namespace,
	)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl rollout undo fallito: %w", err)
	}
	return nil
}
