package ui

import (
	"fmt"
	"testing"

	"Hub-cli/internal/config"
)

// TestPreviewDockerfileMissing is a viewer, like TestPreviewRun: it prints the
// question a missing Dockerfile raises — through the real prompt and list — and
// the branch picker that follows "Cambia branch". It fails nothing.
func TestPreviewDockerfileMissing(t *testing.T) {
	if testing.Short() {
		t.Skip("anteprima: solo su richiesta")
	}

	fmt.Println()
	for _, l := range logWarn(
		"Dockerfile configurato non trovato",
		`~\.hub-cli\repos\acme-docker\portal\webapp-public\Dockerfile`,
		"branch del repo Docker: site-a-pre-prod",
		wfBranchHint([]string{"app-dev"})) {
		fmt.Println(l)
	}

	cfg := &config.Config{}
	cfg.Config.DockerRootPath = `C:\dev\ACME\docker`
	m := WorkflowModel{
		cfg:                 cfg,
		svcName:             "portal-webapp-public",
		width:               100,
		dockerRepoDir:       `~\.hub-cli\repos\acme-docker`,
		dockerURL:           "git@example:acme-docker.git",
		dockerBranchChoices: []string{"site-b-prod", "site-a-pre-prod", "app-dev", "app-prod"},
		dockerfileBranches:  []string{"app-dev"},
	}
	next, _ := m.enterDockerfileMissing()
	fmt.Println()
	fmt.Println(next.(WorkflowModel).list.View().Content)

	picker := listModel{
		title: "Branch del repo Docker per " + m.svcName,
		items: wfBranchChoices(m.dockerBranchChoices, m.dockerfileBranches, "site-a-pre-prod"),
		width: m.width,
	}
	fmt.Println()
	fmt.Println(picker.View().Content)
	fmt.Println()
}
