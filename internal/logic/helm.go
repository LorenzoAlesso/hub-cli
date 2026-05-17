package logic

import (
	"fmt"
	"io"
	"os/exec"
)

// HelmDeploy runs `helm install` or `helm upgrade` depending on whether the release already exists.
func HelmDeploy(releaseName, chartName, valuesPath, namespace, setArg, chartVersion string, out io.Writer) error {
	statusCmd := exec.Command("helm", "status", releaseName, "--namespace", namespace)
	if statusCmd.Run() == nil {
		return HelmUpgrade(releaseName, chartName, valuesPath, namespace, setArg, chartVersion, out)
	}
	return HelmInstall(releaseName, chartName, valuesPath, namespace, setArg, chartVersion, out)
}

func HelmInstall(releaseName, chartName, valuesPath, namespace, setArg, chartVersion string, out io.Writer) error {
	cmd := exec.Command(
		"helm", "install", releaseName, chartName,
		"-f", valuesPath,
		"--namespace", namespace,
		"--set", setArg,
		"--version", chartVersion,
	)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm install fallito: %w", err)
	}
	return nil
}

func HelmUpgrade(releaseName, chartName, valuesPath, namespace, setArg, chartVersion string, out io.Writer) error {
	cmd := exec.Command(
		"helm", "upgrade", releaseName, chartName,
		"-f", valuesPath,
		"--namespace", namespace,
		"--set", setArg,
		"--version", chartVersion,
	)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm upgrade fallito: %w", err)
	}
	return nil
}
