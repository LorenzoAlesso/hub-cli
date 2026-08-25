package ui

import (
	"strings"
	"testing"

	"Hub-cli/internal/config"
)

// The safe answer must be the one already under the cursor: the list opens on
// index 0, so cancelling has to be the first entry.
func TestDockerfileMissingDefaultsToCancel(t *testing.T) {
	m := WorkflowModel{
		svcName: "portal-gateway",
		svc:     config.ServiceConfig{ECRRepository: "123.dkr.ecr.eu-west-1.amazonaws.com/portal/gateway"},
	}
	next, _ := m.enterDockerfileMissing()
	got := next.(WorkflowModel)

	if got.state != wfSvcDockerfileMissing {
		t.Fatalf("stato = %v, atteso wfSvcDockerfileMissing", got.state)
	}
	if got.list.cursor != 0 {
		t.Errorf("cursore su %d: la scelta sicura deve essere preselezionata", got.list.cursor)
	}
	if got.list.items[0].Value != "cancel" {
		t.Errorf("prima voce = %q, atteso \"cancel\"", got.list.items[0].Value)
	}
	if len(got.list.items) != 2 || got.list.items[1].Value != "scan" {
		t.Errorf("la scansione deve restare disponibile come seconda scelta: %+v", got.list.items)
	}
}

func TestDockerfileMissingCancelStopsTheDeploy(t *testing.T) {
	m := WorkflowModel{svcName: "portal-gateway"}
	m.list = listModel{selected: "cancel"}

	next, _ := m.finishDockerfileMissing()
	got := next.(WorkflowModel)

	if !got.cancelled {
		t.Error("scegliendo Annulla il deploy deve fermarsi")
	}
	if got.dockerfilePath != "" {
		t.Errorf("nessun Dockerfile deve essere scelto: %q", got.dockerfilePath)
	}
}

// The destination comes from the config whatever file is picked, so it belongs
// in the title: that is what makes a mismatch visible.
func TestDockerfileListTitleNamesTheDestination(t *testing.T) {
	title := wfDockerfileListTitle("portal-gateway", "123.dkr.ecr.eu-west-1.amazonaws.com/portal/gateway")

	if !strings.Contains(title, "portal-gateway") {
		t.Errorf("il titolo deve dire come verrà buildato: %q", title)
	}
	if !strings.Contains(title, "portal/gateway") {
		t.Errorf("il titolo deve dire dove finisce l'immagine: %q", title)
	}
	if strings.Contains(title, "amazonaws.com") {
		t.Errorf("l'host del registry è rumore, va tolto: %q", title)
	}

	if bare := wfDockerfileListTitle("svc", ""); !strings.Contains(bare, "svc") {
		t.Errorf("senza repository il titolo resta sensato: %q", bare)
	}
}

// A fallback pick must never overwrite a path the config already declares:
// on the wrong branch that would replace the correct value with a wrong one.
func TestDiscoveredPathIsPersistedOnlyWhenNothingWasConfigured(t *testing.T) {
	cases := []struct {
		name    string
		subpath string
		want    bool
	}{
		{"servizio senza path configurato", "", true},
		{"servizio con path configurato", "portal/gateway/Dockerfile", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := WorkflowModel{
				discovered: true,
				testUI:     false,
				svc:        config.ServiceConfig{DockerfileSubpath: tc.subpath},
			}
			if got := m.shouldPersistDockerfilePath(); got != tc.want {
				t.Errorf("shouldPersistDockerfilePath() = %v, atteso %v", got, tc.want)
			}
		})
	}
}
