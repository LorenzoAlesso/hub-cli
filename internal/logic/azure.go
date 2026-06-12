package logic

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// AZIsLoggedIn reports whether an Azure CLI session is currently active.
func AZIsLoggedIn() bool {
	cmd := exec.Command("az", "account", "show", "--query", "id", "-o", "tsv")
	return cmd.Run() == nil
}

// AZLogin runs `az login` for the given tenant. It is interactive (browser /
// device code) so it writes straight to the terminal — run it outside the TUI.
// Stdin receives "\n" so the subscription prompt accepts the default — the
// correct subscription is then set explicitly via AZSetSubscription.
func AZLogin(tenantID string) error {
	cmd := exec.Command("az", "login", "--tenant", tenantID)
	cmd.Stdin = bytes.NewBufferString("\n")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("az login fallito: %w", err)
	}
	return nil
}

// AZSetSubscription sets the active Azure subscription.
func AZSetSubscription(subscriptionID string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("az", "account", "set", "--subscription", subscriptionID)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("az account set fallito: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

// AZGetAKSCredentials fetches kubectl credentials for the given AKS cluster
// and switches the current kube context to it.
func AZGetAKSCredentials(resourceGroup, clusterName string, out io.Writer) error {
	cmd := exec.Command(
		"az", "aks", "get-credentials",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--overwrite-existing",
	)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("az aks get-credentials fallito: %w", err)
	}
	return nil
}

// ACRLogin runs `az acr login` for the given registry.
func ACRLogin(acrName string, out io.Writer) error {
	cmd := exec.Command("az", "acr", "login", "--name", acrName)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("az acr login fallito: %w", err)
	}
	return nil
}

// ACRDeleteImage removes a tag from an Azure Container Registry repository.
// Used to roll back a pushed image. repository accepts either the bare path
// ("org/service") or the full reference ("registry.azurecr.io/org/service").
func ACRDeleteImage(acrName, repository, tag string) error {
	if idx := strings.Index(repository, "/"); idx != -1 && strings.Contains(repository[:idx], ".") {
		repository = repository[idx+1:]
	}

	var stderr bytes.Buffer
	cmd := exec.Command(
		"az", "acr", "repository", "delete",
		"--name", acrName,
		"--image", fmt.Sprintf("%s:%s", repository, tag),
		"--yes",
	)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rollback ACR fallito: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}
