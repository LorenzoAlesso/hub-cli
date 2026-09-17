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

// HelmDeployChart installs or upgrades a release from a chart directory, with one
// --set per image being bumped: a single upgrade means one revision instead of
// one per service. No --version is passed, the chart comes from the managed clone
// and its version is whatever the branch declares.
func HelmDeployChart(releaseName, chartDir, valuesPath, namespace string, setArgs []string, out io.Writer) error {
	action := "upgrade"
	if exec.Command("helm", "status", releaseName, "--namespace", namespace).Run() != nil {
		action = "install"
	}

	args := []string{action, releaseName, chartDir, "-f", valuesPath, "--namespace", namespace}
	for _, set := range setArgs {
		args = append(args, "--set", set)
	}

	cmd := exec.Command("helm", args...)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm %s fallito: %w", action, err)
	}
	return nil
}

// HelmCommandLine renders the command HelmDeployChart would run, for --dry-run.
func HelmCommandLine(releaseName, chartDir, valuesPath, namespace string, setArgs []string) string {
	line := fmt.Sprintf("helm upgrade %s %s -f %s --namespace %s",
		releaseName, chartDir, valuesPath, namespace)
	for _, set := range setArgs {
		line += " --set " + set
	}
	return line
}
