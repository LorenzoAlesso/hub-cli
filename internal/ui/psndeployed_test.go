package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"Hub-cli/internal/config"
	"Hub-cli/internal/logic"

	"charm.land/lipgloss/v2"
)

// Three images as a values file declares them, esb one tag behind the cluster.
func driftedModel() PSNWorkflowModel {
	key := func(k, tag string) logic.HelmImageKey {
		return logic.HelmImageKey{Key: k, SetKey: k + ".image.tag", Tag: tag}
	}
	return PSNWorkflowModel{
		testUI:  true,
		release: config.PSNReleaseConfig{Name: "app-coll", ChartsBranch: "dev-site"},
		values: logic.HelmValues{Services: []logic.HelmService{
			{Name: "app-be", Repository: "acr.azurecr.io/demo/app-be", Tag: "3.0.11-dev",
				Keys: []logic.HelmImageKey{key("appBe", "3.0.11-dev")}},
			{Name: "app-esb", Repository: "acr.azurecr.io/demo/app-esb", Tag: "3.0.9-dev",
				Keys: []logic.HelmImageKey{key("appEsb", "3.0.9-dev")}},
			{Name: "app-fe", Repository: "acr.azurecr.io/demo/app-fe", Tag: "3.0.11-dev",
				Keys: []logic.HelmImageKey{key("appFe", "3.0.11-dev")}},
		}},
	}
}

func deployedTags(tags map[string]string) map[string]any {
	out := map[string]any{}
	for k, tag := range tags {
		out[k] = map[string]any{"image": map[string]any{"tag": tag}}
	}
	return out
}

func readRelease(t *testing.T, m PSNWorkflowModel, msg psnReleaseReadMsg) PSNWorkflowModel {
	t.Helper()
	next, _ := m.handleReleaseRead(msg)
	return next.(PSNWorkflowModel)
}

// The service list shows the tag that runs, and says where the values differ.
func TestReleaseReadStartsFromTheDeployedTags(t *testing.T) {
	m := readRelease(t, driftedModel(), psnReleaseReadMsg{deployed: deployedTags(map[string]string{
		"appBe": "3.0.11-dev", "appEsb": "3.0.11-dev", "appFe": "3.0.11-dev",
	})})

	if m.state != psnDepSelect {
		t.Fatalf("stato = %v, atteso psnDepSelect", m.state)
	}
	if got := m.svcByName["app-esb"].Tag; got != "3.0.11-dev" {
		t.Errorf("app-esb parte da %q, atteso il tag deployato 3.0.11-dev", got)
	}

	var esb Item
	for _, it := range m.multisel.items {
		if it.Value == "app-esb" {
			esb = it
		}
	}
	if !strings.Contains(esb.Desc, "3.0.11-dev") || !strings.Contains(esb.Desc, "nel values 3.0.9-dev") {
		t.Errorf("descrizione di app-esb = %q", esb.Desc)
	}

	log := stripANSI(strings.Join(m.log, "\n"))
	for _, want := range []string{"values non allineato", "app-esb", "values 3.0.9-dev", "deployato 3.0.11-dev"} {
		if !strings.Contains(log, want) {
			t.Errorf("il log non riporta %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "app-be ") {
		t.Errorf("app-be è allineato e non va segnalato:\n%s", log)
	}
}

// A release never installed, or one that cannot be read, does not stop the run:
// it starts from the values and the log says which case it is.
func TestReleaseReadWithoutDeployedTags(t *testing.T) {
	cases := map[string]error{
		"non ancora installato":       logic.ErrReleaseNotFound,
		"tag deployati non leggibili": errors.New("helm get values fallito: timeout"),
	}
	for want, err := range cases {
		m := readRelease(t, driftedModel(), psnReleaseReadMsg{err: err})
		if m.state != psnDepSelect || len(m.drift) != 0 {
			t.Errorf("%s: stato %v, scostamenti %d", want, m.state, len(m.drift))
		}
		if got := m.svcByName["app-esb"].Tag; got != "3.0.9-dev" {
			t.Errorf("%s: app-esb parte da %q, atteso il tag del values", want, got)
		}
		if log := stripANSI(strings.Join(m.log, "\n")); !strings.Contains(log, want) {
			t.Errorf("il log non dice %q:\n%s", want, log)
		}
	}
}

// Deploying be alone must not roll esb back: the upgrade keeps it as it runs,
// the sync writes it back, and the run says so. A service redeployed now needs
// neither — its new tag covers it.
func TestUpgradeKeepsWhatTheValuesMissed(t *testing.T) {
	m := readRelease(t, driftedModel(), psnReleaseReadMsg{deployed: deployedTags(map[string]string{
		"appBe": "3.0.11-dev", "appEsb": "3.0.11-dev", "appFe": "3.0.12-dev",
	})})
	m.deployed = []psnDeployed{
		{svc: m.svcByName["app-be"], oldTag: "3.0.11-dev", newTag: "3.0.12-dev"},
		{svc: m.svcByName["app-fe"], oldTag: "3.0.12-dev", newTag: "3.0.13-dev"},
	}

	got := m.helmSetArgs()
	want := []string{"appBe.image.tag=3.0.12-dev", "appFe.image.tag=3.0.13-dev", "appEsb.image.tag=3.0.11-dev"}
	if !slices.Equal(got, want) {
		t.Errorf("--set = %v\natteso  %v", got, want)
	}

	// The footer names the sync only for a real run.
	m.testUI = false
	if footer := m.summaryFooter(); !strings.Contains(footer, "1 tag riallineato") {
		t.Errorf("il riepilogo non dice del riallineamento: %q", footer)
	}
	if kept := psnKeptNote(m.pendingDrift()); kept != "app-esb 3.0.11-dev" {
		t.Errorf("tag mantenuti = %q", kept)
	}
}

func startService(t *testing.T, m PSNWorkflowModel, name string) PSNWorkflowModel {
	t.Helper()
	m.selectedDeps = []string{name}
	next, _ := m.startNextDeployment()
	return next.(PSNWorkflowModel)
}

// The proposal skips a tag ACR already holds, and says why it jumps.
func TestProposalStepsPastACR(t *testing.T) {
	m := readRelease(t, driftedModel(), psnReleaseReadMsg{deployed: deployedTags(map[string]string{
		"appEsb": "3.0.11-dev",
	})})
	m.acrTags = map[string][]string{"acr.azurecr.io/demo/app-esb": {"3.0.10-dev", "3.0.11-dev", "3.0.12-dev"}}

	m = startService(t, m, "app-esb")
	if m.suggestedTag != "3.0.13-dev" {
		t.Errorf("proposto %q, atteso 3.0.13-dev", m.suggestedTag)
	}
	if got := m.input.textInput.Value(); got != "3.0.13-dev" {
		t.Errorf("valore nel campo = %q", got)
	}
	log := stripANSI(strings.Join(m.log, "\n"))
	if !strings.Contains(log, "presente fino a 3.0.12-dev") {
		t.Errorf("il salto della proposta non è spiegato:\n%s", log)
	}

	// Nothing on ACR past the running tag: a plain increment, and no note.
	m = readRelease(t, driftedModel(), psnReleaseReadMsg{})
	m.acrTags = map[string][]string{"acr.azurecr.io/demo/app-be": {"3.0.11-dev"}}
	m = startService(t, m, "app-be")
	if m.suggestedTag != "3.0.12-dev" || strings.Contains(stripANSI(strings.Join(m.log, "\n")), "Su ACR") {
		t.Errorf("proposta %q, log:\n%s", m.suggestedTag, stripANSI(strings.Join(m.log, "\n")))
	}
}

// A chosen tag that exists on ACR is asked about; changing it goes back to the
// question with the header pending again, overwriting goes on and says so.
func TestExistingTagIsAskedAbout(t *testing.T) {
	m := readRelease(t, driftedModel(), psnReleaseReadMsg{})
	m.acrTags = map[string][]string{"acr.azurecr.io/demo/app-be": {"3.0.11-dev", "3.0.12-dev"}}
	m = startService(t, m, "app-be")

	m.input.textInput.SetValue("3.0.12-dev")
	next, _ := m.finishTagInput()
	m = next.(PSNWorkflowModel)
	if m.state != psnTagExists {
		t.Fatalf("stato = %v, atteso psnTagExists", m.state)
	}
	if m.list.items[m.list.cursor].Value != "change" {
		t.Error("la scelta preselezionata deve essere cambiare tag, non sovrascrivere")
	}
	if header := stripANSI(m.log[m.svcLogStart]); !strings.Contains(header, "3.0.11-dev → …") {
		t.Errorf("finché la domanda è aperta la testata resta in sospeso: %q", header)
	}

	m.list.selected = "change"
	next, _ = m.finishTagExists()
	changed := next.(PSNWorkflowModel)
	if changed.state != psnTagInput || changed.input.textInput.Value() != "3.0.13-dev" {
		t.Errorf("cambia tag: stato %v, campo %q", changed.state, changed.input.textInput.Value())
	}
	if !strings.Contains(stripANSI(changed.log[changed.svcLogStart]), "→ …") {
		t.Errorf("la testata deve tornare in sospeso: %q", stripANSI(changed.log[changed.svcLogStart]))
	}

	m.list.selected = "overwrite"
	next, _ = m.finishTagExists()
	kept := next.(PSNWorkflowModel)
	if kept.newTag != "3.0.12-dev" || kept.state == psnTagExists || kept.state == psnTagInput {
		t.Errorf("sovrascrivi: tag %q, stato %v", kept.newTag, kept.state)
	}
	if log := stripANSI(strings.Join(kept.log, "\n")); !strings.Contains(log, "il push sostituisce") {
		t.Errorf("la sovrascrittura non è dichiarata:\n%s", log)
	}
	if header := stripANSI(kept.log[kept.svcLogStart]); !strings.Contains(header, "3.0.11-dev → 3.0.12-dev") {
		t.Errorf("confermato il tag, la testata va completata: %q", header)
	}
}

// The running tag is on ACR by definition: redeploying it is the unchanged-tag
// path, not a collision, and tags that could not be read are not checked.
func TestRunningTagAndUnreadTagsAreNotAsked(t *testing.T) {
	m := readRelease(t, driftedModel(), psnReleaseReadMsg{})
	m.acrTags = map[string][]string{"acr.azurecr.io/demo/app-be": {"3.0.11-dev"}}
	m = startService(t, m, "app-be")
	m.input.textInput.SetValue("3.0.11-dev")
	if next, _ := m.finishTagInput(); next.(PSNWorkflowModel).state == psnTagExists {
		t.Error("il tag in esecuzione non deve chiedere conferma di sovrascrittura")
	}

	m = readRelease(t, driftedModel(), psnReleaseReadMsg{})
	m = startService(t, m, "app-be")
	m.input.textInput.SetValue("3.0.1-dev")
	if next, _ := m.finishTagInput(); next.(PSNWorkflowModel).state == psnTagExists {
		t.Error("senza tag letti da ACR non c'è niente da confrontare")
	}
}

// Unreadable tags leave the run going, name the service, and skip its check.
func TestTagsLoadedWithErrors(t *testing.T) {
	m := readRelease(t, driftedModel(), psnReleaseReadMsg{})
	m.selectedDeps = []string{"app-be", "app-fe"}
	next, _ := m.handleTagsLoaded(psnTagsLoadedMsg{
		tags: map[string][]string{"acr.azurecr.io/demo/app-be": {"3.0.11-dev"}},
		errs: map[string]error{"acr.azurecr.io/demo/app-fe": errors.New("az: timeout")},
	})
	m = next.(PSNWorkflowModel)

	if m.state != psnTagInput {
		t.Fatalf("stato = %v, atteso psnTagInput", m.state)
	}
	log := stripANSI(strings.Join(m.log, "\n"))
	if !strings.Contains(log, "non verificabili") || !strings.Contains(log, "app-fe: az: timeout") {
		t.Errorf("il log non riporta l'errore di app-fe:\n%s", log)
	}
	if _, ok := m.acrTags["acr.azurecr.io/demo/app-fe"]; ok {
		t.Error("un repository non letto non deve risultare senza tag")
	}
}

// The drift report keeps its tags in one column whatever the service names.
func TestDriftLinesAlignTheirTags(t *testing.T) {
	lines := logDrift([]logic.TagDrift{
		{Service: "app-be", Values: "3.0.9-dev", Deployed: "3.0.11-dev"},
		{Service: "app-webapp-public", Values: "1.0.0", Deployed: "1.0.2"},
		{Service: "app-be", Values: "3.0.9-dev", Deployed: "3.0.11-dev"},
	}, "0.9s")

	if len(lines) != 4 {
		t.Fatalf("righe = %d, attese 4 (passo, due servizi, nota):\n%s", len(lines), stripANSI(strings.Join(lines, "\n")))
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > logWidth {
			t.Errorf("riga larga %d, oltre la griglia (%d): %s", w, logWidth, stripANSI(l))
		}
	}
	if strings.Index(stripANSI(lines[1]), "values") != strings.Index(stripANSI(lines[2]), "values") {
		t.Errorf("colonne disallineate:\n%s\n%s", stripANSI(lines[1]), stripANSI(lines[2]))
	}
}

// The simulated run shows both new paths: jboss-fe realigned to the release,
// and webapp's proposal stepping past the image ACR already holds.
func TestTestUIShowsAlignmentAndACRSkip(t *testing.T) {
	m := PSNWorkflowModel{testUI: true, cfg: &config.Config{}}
	next, _ := m.start()
	m = next.(PSNWorkflowModel)
	if m.state != psnReleaseRead {
		t.Fatalf("stato = %v, atteso psnReleaseRead", m.state)
	}

	m = readRelease(t, m, psnReleaseReadMsg{deployed: psnFakeDeployed()})
	if len(m.drift) != 1 || m.drift[0].Service != "jboss-fe" {
		t.Fatalf("scostamenti simulati = %+v, atteso solo jboss-fe", m.drift)
	}

	m.selectedDeps = []string{"webapp", "jboss-fe"}
	next, _ = m.enterTagsLoading()
	m = next.(PSNWorkflowModel)
	repos := []string{m.svcByName["webapp"].Repository, m.svcByName["jboss-fe"].Repository}
	next, _ = m.handleTagsLoaded(psnTagsLoadedMsg{tags: psnFakeACRTags(repos)})
	m = next.(PSNWorkflowModel)

	if m.svc.Name != "webapp" || m.suggestedTag != "1.0.2" {
		t.Errorf("webapp: proposto %q, atteso 1.0.2 (1.0.1 è già su ACR)", m.suggestedTag)
	}
	if !strings.Contains(stripANSI(strings.Join(m.log, "\n")), "presente fino a 1.0.1") {
		t.Errorf("il salto della proposta non si vede:\n%s", stripANSI(strings.Join(m.log, "\n")))
	}
}
