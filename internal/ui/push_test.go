package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"Hub-cli/internal/logic"
)

// psnPushModel is a model parked on the push of the last of two services, the
// first one already pushed: the run a refused push used to end with neither
// service deployed.
func psnPushModel(attempt int) PSNWorkflowModel {
	return PSNWorkflowModel{
		state:        psnPushing,
		testUI:       true, // keeps the steps after the push from touching docker, helm or git
		pushAttempt:  attempt,
		selectedDeps: []string{"jboss-be", "webapp"},
		depIdx:       1,
		svc: logic.HelmService{Name: "webapp", Keys: []logic.HelmImageKey{
			{Key: "webapp", SetKey: "webapp.image.tag"},
		}},
		oldTag: "1.0.0",
		newTag: "1.0.1",
		deployed: []psnDeployed{{
			svc: logic.HelmService{Name: "jboss-be", Keys: []logic.HelmImageKey{
				{Key: "jbossBe", SetKey: "jbossBe.image.tag"},
			}},
			oldTag: "2.0.0", newTag: "2.0.1",
		}},
	}
}

func itemValues(items []Item) []string {
	values := make([]string, len(items))
	for i, it := range items {
		values[i] = it.Value
	}
	return values
}

// A refused push is repeated on its own, before anything is asked: the registry
// usually takes it a moment later.
func TestFailedPushIsRetried(t *testing.T) {
	next, _ := psnPushModel(1).handleOpDone(psnOpDoneMsg{err: errSimulatedPush, output: []byte(simulatedACRRefusal)})

	m := next.(PSNWorkflowModel)
	if m.state != psnPushing || m.pushAttempt != 2 {
		t.Fatalf("stato = %v, tentativo = %d: atteso un nuovo push, tentativo 2", m.state, m.pushAttempt)
	}
	if len(m.deployed) != 1 {
		t.Errorf("pushati = %d, atteso 1: un push fallito non conta", len(m.deployed))
	}
}

// Once every attempt has failed the run stops and asks, instead of quitting and
// taking the images already pushed down with it.
func TestPushAsksAfterTheLastAttempt(t *testing.T) {
	next, _ := psnPushModel(pushAttempts).handleOpDone(psnOpDoneMsg{err: errSimulatedPush})

	m := next.(PSNWorkflowModel)
	if m.state != psnPushError {
		t.Fatalf("stato = %v, atteso psnPushError", m.state)
	}
	if got, want := itemValues(m.list.items), []string{"retry", "skip", "cancel"}; !reflect.DeepEqual(got, want) {
		t.Errorf("opzioni = %v, attese %v", got, want)
	}
}

// Skipping the service whose push failed still upgrades the release with the
// others — the case of a push refused on the last service of the run.
func TestSkippedPushStillDeploysTheOthers(t *testing.T) {
	m := psnPushModel(pushAttempts)
	m.state = psnPushError
	m.list = listModel{selected: "skip"}

	next, _ := m.finishPushError()
	got := next.(PSNWorkflowModel)
	if got.state != psnHelmDeploy {
		t.Fatalf("stato = %v, atteso psnHelmDeploy", got.state)
	}
	if args, want := got.helmSetArgs(), []string{"jbossBe.image.tag=2.0.1"}; !reflect.DeepEqual(args, want) {
		t.Errorf("--set = %v, attesi %v: nell'upgrade va solo il servizio pushato", args, want)
	}
}

// With nothing else to deploy, skipping would be cancelling under another name.
func TestSkipNeedsSomethingElseToDeploy(t *testing.T) {
	m := psnPushModel(pushAttempts)
	m.selectedDeps = []string{"webapp"}
	m.depIdx = 0
	m.deployed = nil

	next, _ := m.handleOpDone(psnOpDoneMsg{err: errSimulatedPush})
	if got, want := itemValues(next.(PSNWorkflowModel).list.items), []string{"retry", "cancel"}; !reflect.DeepEqual(got, want) {
		t.Errorf("opzioni = %v, attese %v", got, want)
	}
}

// Retrying from the question starts a new round of attempts, not a single one.
func TestRetryFromTheQuestionStartsOver(t *testing.T) {
	m := psnPushModel(pushAttempts)
	m.state = psnPushError
	m.list = listModel{selected: "retry"}

	next, _ := m.finishPushError()
	if got := next.(PSNWorkflowModel); got.state != psnPushing || got.pushAttempt != 1 {
		t.Errorf("stato = %v, tentativo = %d: atteso psnPushing dal tentativo 1", got.state, got.pushAttempt)
	}
}

// The local run retries the same way: a first failure is not asked about.
func TestLocalFailedPushIsRetried(t *testing.T) {
	m := WorkflowModel{state: wfSvcPushing, testUI: true, pushAttempt: 1, selectedServices: []string{"web"}}

	next, _ := m.handleOpDone(wfOpDoneMsg{err: errSimulatedPush, output: []byte(simulatedECRTimeout)})
	if got := next.(WorkflowModel); got.state != wfSvcPushing || got.pushAttempt != 2 {
		t.Errorf("stato = %v, tentativo = %d: atteso un nuovo push, tentativo 2", got.state, got.pushAttempt)
	}
}

// The local run upgrades one release per service, so a push that failed on the
// last one left the others deployed and never synced. Skipping it reaches the
// end of the run, where the sync is.
func TestLocalSkippedPushReachesTheEnd(t *testing.T) {
	m := WorkflowModel{
		state:            wfSvcPushing,
		testUI:           true,
		pushAttempt:      pushAttempts,
		selectedServices: []string{"api", "web"},
		svcIdx:           1,
		svcName:          "web",
		oldTag:           "1.0.0",
		newTag:           "1.0.1",
		deployed:         []deployedService{{name: "api", tag: "2.0.1"}},
	}

	next, _ := m.handleOpDone(wfOpDoneMsg{err: errSimulatedPush})
	m = next.(WorkflowModel)
	if m.state != wfSvcPushError {
		t.Fatalf("stato = %v, atteso wfSvcPushError", m.state)
	}

	m.list.selected = "skip"
	next, _ = m.finishPushError()
	if got := next.(WorkflowModel); got.state != wfSummary || len(got.deployed) != 1 {
		t.Errorf("stato = %v, deployati = %d: atteso wfSummary con il solo servizio riuscito",
			got.state, len(got.deployed))
	}
}

// The retry line carries the registry's reason, not the exit status, which reads
// the same whatever went wrong.
func TestPushRetryShowsTheRegistryReason(t *testing.T) {
	output := "The push refers to repository [demoacr.azurecr.io/demo/webapp]\n" +
		"5f70bf18a086: Preparing\n" + simulatedACRRefusal + "\r\n\n"

	text := strings.Join(logPushRetry(1, errSimulatedPush, []byte(output), "3.2s"), "\n")
	if !strings.Contains(text, "is not allowed access") || strings.Contains(text, "exit status") {
		t.Errorf("riga di retry senza il motivo del registry: %q", text)
	}
	if want := fmt.Sprintf("tentativo 1/%d", pushAttempts); !strings.Contains(text, want) {
		t.Errorf("riga di retry senza %q: %q", want, text)
	}
}

// With no output to read a reason from, the error itself is the reason.
func TestPushRetryFallsBackOnTheError(t *testing.T) {
	text := strings.Join(logPushRetry(2, errSimulatedPush, nil, "3.2s"), "\n")
	if !strings.Contains(text, errSimulatedPush.Error()) {
		t.Errorf("riga di retry senza errore: %q", text)
	}
}
