package ui

import (
	"reflect"
	"testing"

	"Hub-cli/internal/config"
)

// The branches that have the Dockerfile come first — the first is preselected —
// and the branch in use is left out: it is the one that just failed to have it.
func TestBranchChoicesPutContainingFirst(t *testing.T) {
	items := wfBranchChoices(
		[]string{"site-b-prod", "site-a-pre-prod", "app-dev", "app-prod"},
		[]string{"app-dev", "app-prod"},
		"site-a-pre-prod")

	var got []string
	for _, it := range items {
		got = append(got, it.Value)
	}
	if want := []string{"app-dev", "app-prod", "site-b-prod"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ordine = %v, atteso %v", got, want)
	}
	if items[0].Desc == "" || items[2].Desc != "" {
		t.Errorf("vanno segnati solo i branch con il Dockerfile: %+v", items)
	}
}

// The hint names where the Dockerfile is, and says so plainly when it is nowhere.
func TestBranchHint(t *testing.T) {
	cases := map[string][]string{
		"presente su: app-dev":                   {"app-dev"},
		"presente su: a, b, c e altri 2":          {"a", "b", "c", "d", "e"},
		"non presente in nessun branch di origin": nil,
	}
	for want, found := range cases {
		if got := wfBranchHint(found); got != want {
			t.Errorf("%v: %q, atteso %q", found, got, want)
		}
	}
}

// A service that moved the Docker clone mid-run writes its manifest back to the
// branch its image was built from; the others keep the run's branch.
func TestManifestSyncGroupsByBuildBranch(t *testing.T) {
	deployed := []deployedService{
		{name: "a", dockerBranch: "site-a-pre-prod"},
		{name: "b", dockerBranch: "app-dev"},
		{name: "c"},
		{name: "d", dockerBranch: "app-dev"},
	}

	var order []string
	got := map[string][]string{}
	for _, g := range wfByDockerBranch(deployed, "site-a-pre-prod") {
		order = append(order, g.branch)
		for _, d := range g.services {
			got[g.branch] = append(got[g.branch], d.name)
		}
	}

	if want := []string{"site-a-pre-prod", "app-dev"}; !reflect.DeepEqual(order, want) {
		t.Errorf("branch = %v, attesi %v", order, want)
	}
	if want := []string{"a", "c"}; !reflect.DeepEqual(got["site-a-pre-prod"], want) {
		t.Errorf("site-a-pre-prod = %v, attesi %v", got["site-a-pre-prod"], want)
	}
	if want := []string{"b", "d"}; !reflect.DeepEqual(got["app-dev"], want) {
		t.Errorf("app-dev = %v, attesi %v", got["app-dev"], want)
	}
}

// hub-cli moves its own clone between branches, never the working copy: with
// --local-sources the build reads the user's checkout, which may hold work in
// progress, so the switch is not offered.
func TestBranchSwitchIsNeverOfferedOnTheWorkingCopy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Config.DockerRootPath = "/repo/docker"

	base := WorkflowModel{
		cfg:                 cfg,
		svcName:             "portal-webapp-public",
		dockerRepoDir:       "/managed/docker",
		dockerURL:           "git@example:docker.git",
		dockerBranchChoices: []string{"site-a-pre-prod", "app-dev"},
	}
	if !offers(base, "branch") {
		t.Error("clone gestito: Cambia branch va offerto")
	}

	local := base
	local.localSources = true
	if offers(local, "branch") {
		t.Error("--local-sources: la copia di lavoro non si cambia di branch")
	}

	lonely := base
	lonely.dockerBranchChoices = []string{"site-a-pre-prod"}
	if offers(lonely, "branch") {
		t.Error("un solo branch: non c'è niente su cui spostarsi")
	}
}

func offers(m WorkflowModel, value string) bool {
	next, _ := m.enterDockerfileMissing()
	for _, it := range next.(WorkflowModel).list.items {
		if it.Value == value {
			return true
		}
	}
	return false
}
