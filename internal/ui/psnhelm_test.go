package ui

import (
	"strings"
	"testing"
	"time"

	"Hub-cli/internal/logic"
)

// One --set per values key, so an image referenced twice is bumped in both
// places: leaving one behind is how the two references drift apart.
func TestHelmSetArgsCoverEveryKey(t *testing.T) {
	m := PSNWorkflowModel{deployed: []psnDeployed{
		{
			svc: logic.HelmService{
				Name:       "jboss-be",
				Repository: "acr.azurecr.io/apps/jboss-be",
				Keys:       []logic.HelmImageKey{{Key: "jbossBe", SetKey: "jbossBe.image.tag"}},
			},
			oldTag: "3.0.9-dev", newTag: "3.0.10-dev",
		},
		{
			svc: logic.HelmService{
				Name:       "initc",
				Repository: "acr.azurecr.io/apps/initc",
				Keys: []logic.HelmImageKey{
					{Key: "initContainer", SetKey: "initContainer.image", ImagePath: "acr.azurecr.io/apps/initc"},
					{Key: "busybox", SetKey: "busybox.image.tag"},
				},
			},
			oldTag: "dev", newTag: "dev2",
		},
	}}

	got := m.helmSetArgs()
	want := []string{
		"jbossBe.image.tag=3.0.10-dev",
		"initContainer.image=acr.azurecr.io/apps/initc:dev2",
		"busybox.image.tag=dev2",
	}

	if len(got) != len(want) {
		t.Fatalf("--set = %v, attesi %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("--set[%d] = %q, atteso %q", i, got[i], want[i])
		}
	}
}

// The inline "repository:tag" form needs the whole reference, not just the tag:
// writing only the tag there would produce a bare "dev2" as the image.
func TestHelmSetArgsKeepTheInlineForm(t *testing.T) {
	m := PSNWorkflowModel{deployed: []psnDeployed{{
		svc: logic.HelmService{
			Name: "initc",
			Keys: []logic.HelmImageKey{
				{SetKey: "initContainer.image", ImagePath: "acr.azurecr.io/apps/initc"},
			},
		},
		newTag: "2.0.0",
	}}}

	got := m.helmSetArgs()
	if len(got) != 1 {
		t.Fatalf("--set = %v", got)
	}
	if !strings.HasSuffix(got[0], ":2.0.0") || !strings.Contains(got[0], "apps/initc") {
		t.Errorf("--set = %q: la forma inline deve portare repository e tag", got[0])
	}
}

func TestHelmSetArgsEmptyWhenNothingDeployed(t *testing.T) {
	var m PSNWorkflowModel
	if got := m.helmSetArgs(); len(got) != 0 {
		t.Errorf("--set = %v, atteso nessuno", got)
	}
}

// Only the services whose tag stayed the same need a restart: for the others
// helm changed the manifest and Kubernetes recreated the pod on its own.
func TestRestartTargetsOnlyCoverUnchangedTags(t *testing.T) {
	deployed := []psnDeployed{
		{
			svc: logic.HelmService{Name: "jboss-be", Keys: []logic.HelmImageKey{
				{Key: "jbossBe", Deployment: "jboss-be"},
			}},
			oldTag: "3.0.9-dev", newTag: "3.0.9-dev",
		},
		{
			svc: logic.HelmService{Name: "webapp", Keys: []logic.HelmImageKey{
				{Key: "webapp", Deployment: "webapp"},
			}},
			oldTag: "3.0.9-dev", newTag: "3.0.10-dev",
		},
	}

	targets, unnamed := psnRestartTargets(deployed)
	if len(targets) != 1 || targets[0] != "jboss-be" {
		t.Errorf("target = %v, atteso [jboss-be]", targets)
	}
	if len(unnamed) != 0 {
		t.Errorf("senza nome = %v, atteso vuoto", unnamed)
	}
}

// The same Deployment reached through two values keys is restarted once, and a
// key that names no Deployment is reported rather than skipped in silence.
func TestRestartTargetsDeduplicateAndReportUnnamed(t *testing.T) {
	deployed := []psnDeployed{{
		svc: logic.HelmService{Name: "initc", Keys: []logic.HelmImageKey{
			{Key: "jbossBe", Deployment: "jboss-be"},
			{Key: "jbossBeAlias", Deployment: "jboss-be"},
			{Key: "initContainer"},
		}},
		oldTag: "dev", newTag: "dev",
	}}

	targets, unnamed := psnRestartTargets(deployed)
	if len(targets) != 1 || targets[0] != "jboss-be" {
		t.Errorf("target = %v, atteso [jboss-be]", targets)
	}
	if len(unnamed) != 1 || unnamed[0] != "initContainer" {
		t.Errorf("senza nome = %v, atteso [initContainer]", unnamed)
	}
}

// psnRestartModel is a model parked right after the helm upgrade, with one
// service deployed under the given tags.
func psnRestartModel(oldTag, newTag string) PSNWorkflowModel {
	return PSNWorkflowModel{
		state:  psnHelmDeploy,
		testUI: true, // keeps the sync from touching git when no restart is needed
		deployed: []psnDeployed{{
			svc: logic.HelmService{Name: "jboss-be", Keys: []logic.HelmImageKey{
				{Key: "jbossBe", SetKey: "jbossBe.image.tag", Deployment: "jboss-be"},
			}},
			oldTag: oldTag, newTag: newTag,
		}},
	}
}

// A successful upgrade of an unchanged tag has left the old pod running, so the
// workflow has to stop and ask instead of walking on to the sync.
func TestUnchangedTagAsksForRestartAfterUpgrade(t *testing.T) {
	next, _ := psnRestartModel("3.0.9-dev", "3.0.9-dev").handleOpDone(psnOpDoneMsg{})

	m := next.(PSNWorkflowModel)
	if m.state != psnRestartConfirm {
		t.Fatalf("stato = %v, atteso psnRestartConfirm", m.state)
	}
	if len(m.restartTargets) != 1 || m.restartTargets[0] != "jboss-be" {
		t.Errorf("target = %v, atteso [jboss-be]", m.restartTargets)
	}
}

// A new tag changed the manifest, so Kubernetes already recreated the pod: asking
// would be a question with only one sensible answer.
func TestNewTagSkipsTheRestartPrompt(t *testing.T) {
	next, _ := psnRestartModel("3.0.9-dev", "3.0.10-dev").handleOpDone(psnOpDoneMsg{})

	if m := next.(PSNWorkflowModel); m.state == psnRestartConfirm {
		t.Error("tag nuovo: il riavvio non va proposto")
	}
}

// Declining the restart still has to reach the sync: the tags deployed by the
// upgrade belong in the values whether or not the pod was recreated.
func TestDecliningTheRestartStillSyncs(t *testing.T) {
	m := psnRestartModel("3.0.9-dev", "3.0.9-dev")
	m.restartTargets = []string{"jboss-be"}
	m.list = listModel{selected: "skip"}

	next, _ := m.finishRestartPrompt()
	if got := next.(PSNWorkflowModel); got.state == psnRestarting {
		t.Error("scelta \"salta\": nessun riavvio va eseguito")
	}
}

// The summary is assembled after the shared upgrade, so each service has to carry
// the time it took: reading the clock at that point gave every card the duration
// of the last service.
func TestSummaryKeepsPerServiceDuration(t *testing.T) {
	m := PSNWorkflowModel{
		state:  psnHelmDeploy,
		testUI: true,
		deployed: []psnDeployed{
			{svc: logic.HelmService{Name: "jboss-be"}, oldTag: "3.0.9-dev", newTag: "3.0.10-dev",
				elapsed: 3*time.Minute + 47*time.Second},
			{svc: logic.HelmService{Name: "webapp"}, oldTag: "3.0.9-dev", newTag: "3.0.10-dev",
				elapsed: 2*time.Minute + 19*time.Second},
		},
	}

	next, _ := m.handleOpDone(psnOpDoneMsg{})
	results := next.(PSNWorkflowModel).results

	if len(results) != 2 {
		t.Fatalf("risultati = %d, attesi 2", len(results))
	}
	for i, want := range []time.Duration{3*time.Minute + 47*time.Second, 2*time.Minute + 19*time.Second} {
		if results[i].Elapsed != want {
			t.Errorf("durata di %s = %v, attesa %v", results[i].Service, results[i].Elapsed, want)
		}
	}
}
