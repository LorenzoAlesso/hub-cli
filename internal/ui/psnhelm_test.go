package ui

import (
	"strings"
	"testing"

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
