package ui

import (
	"errors"
	"os"
	"path/filepath"
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
	for _, want := range []string{"1 tag diverso dal values", "app-esb  3.0.9-dev nel values, 3.0.11-dev sul cluster"} {
		if !strings.Contains(log, want) {
			t.Errorf("il log non riporta %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "app-be ") {
		t.Errorf("app-be è allineato e non va segnalato:\n%s", log)
	}
}

// A release never installed is not a failure: the run starts from the values.
func TestReleaseReadNotInstalled(t *testing.T) {
	m := readRelease(t, driftedModel(), psnReleaseReadMsg{err: logic.ErrReleaseNotFound})
	if m.state != psnDepSelect || len(m.drift) != 0 {
		t.Errorf("stato %v, scostamenti %d", m.state, len(m.drift))
	}
	if got := m.svcByName["app-esb"].Tag; got != "3.0.9-dev" {
		t.Errorf("app-esb parte da %q, atteso il tag del values", got)
	}
	if log := stripANSI(strings.Join(m.log, "\n")); !strings.Contains(log, "non ancora installato") {
		t.Errorf("il log non lo dice:\n%s", log)
	}
}

var errUnreachable = errors.New(`helm get values fallito: Error: kubernetes cluster unreachable: ` +
	`Get "https://aks-a.privatelink.example.com:443/version": dial tcp: lookup aks-a.privatelink.example.com: no such host`)

// The first failed read is repeated on its own, with the reason cut down to
// what explains it.
func TestReleaseReadRetriesOnce(t *testing.T) {
	m := driftedModel()
	m.releaseAttempt = 1
	m = readRelease(t, m, psnReleaseReadMsg{err: errUnreachable})

	if m.state != psnReleaseRead || m.releaseAttempt != 2 {
		t.Fatalf("stato %v, tentativo %d: atteso il secondo tentativo", m.state, m.releaseAttempt)
	}
	if !strings.Contains(m.spinner.label, "tentativo 2/2") {
		t.Errorf("etichetta dello spinner = %q", m.spinner.label)
	}
	log := stripANSI(strings.Join(m.log, "\n"))
	for _, want := range []string{
		"tentativo 1/2 non riuscito",
		"cluster non raggiungibile: lookup aks-a.privatelink.example.com: no such host",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("il log non riporta %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "helm get values fallito") {
		t.Errorf("la catena di Helm non va mostrata per un cluster irraggiungibile:\n%s", log)
	}
}

// Past the last attempt the run stops and asks, retry first.
func TestReleaseReadAsksAfterTheLastAttempt(t *testing.T) {
	m := driftedModel()
	m.releaseAttempt = releaseReadAttempts
	m = readRelease(t, m, psnReleaseReadMsg{err: errUnreachable})

	if m.state != psnReleaseError {
		t.Fatalf("stato = %v, atteso psnReleaseError", m.state)
	}
	if m.list.title != "Il cluster non risponde" || !m.list.errorTone {
		t.Errorf("domanda: titolo %q, tono d'errore %v", m.list.title, m.list.errorTone)
	}
	if m.list.items[m.list.cursor].Value != "retry" {
		t.Error("la scelta preselezionata deve essere Riprova")
	}
	log := stripANSI(strings.Join(m.log, "\n"))
	if !strings.Contains(log, "✗  Release sul cluster      cluster non raggiungibile") ||
		!strings.Contains(log, "     lookup aks-a.privatelink.example.com: no such host") {
		t.Errorf("riga di errore:\n%s", log)
	}
	if !strings.Contains(m.renderTracker(), "✗ Chart") {
		t.Errorf("la fase Chart va segnata come fallita:\n%s", stripANSI(m.renderTracker()))
	}

	retry := m
	retry.list.selected = "retry"
	next, _ := retry.finishReleaseError()
	if got := next.(PSNWorkflowModel); got.state != psnReleaseRead || got.releaseAttempt != 1 {
		t.Errorf("Riprova: stato %v, tentativo %d", got.state, got.releaseAttempt)
	}

	proceed := m
	proceed.list.selected = "continue"
	next, _ = proceed.finishReleaseError()
	got := next.(PSNWorkflowModel)
	if got.state != psnDepSelect || got.svcByName["app-esb"].Tag != "3.0.9-dev" {
		t.Errorf("Prosegui: stato %v, app-esb %q", got.state, got.svcByName["app-esb"].Tag)
	}
	if log := stripANSI(strings.Join(got.log, "\n")); !strings.Contains(log, "senza confronto con il cluster") {
		t.Errorf("Prosegui va dichiarato:\n%s", log)
	}

	cancel := m
	cancel.list.selected = "cancel"
	next, _ = cancel.finishReleaseError()
	if !next.(PSNWorkflowModel).cancelled {
		t.Error("Annulla deve chiudere il run")
	}
}

// Any other failure asks the same, but says what it was in full: there is no
// telling which part of it matters.
func TestReleaseReadOtherErrors(t *testing.T) {
	err := errors.New(`helm get values fallito: Error: query: failed to query with labels: secrets is forbidden`)
	m := driftedModel()
	m.releaseAttempt = releaseReadAttempts
	m = readRelease(t, m, psnReleaseReadMsg{err: err})

	if m.state != psnReleaseError || m.list.title != "Release non leggibile" {
		t.Fatalf("stato %v, titolo %q", m.state, m.list.title)
	}
	if strings.Contains(m.list.items[0].Desc, "VPN") {
		t.Errorf("il suggerimento sulla VPN vale solo per un cluster irraggiungibile: %q", m.list.items[0].Desc)
	}
	log := stripANSI(strings.Join(m.log, "\n"))
	if !strings.Contains(log, "tag deployati non leggibili") || !strings.Contains(log, err.Error()) {
		t.Errorf("riga di errore:\n%s", log)
	}
}

func TestReleaseReadDetail(t *testing.T) {
	cases := map[string]string{
		`Error: Kubernetes cluster unreachable: Get "https://10.0.0.4:443/version": dial tcp 10.0.0.4:443: i/o timeout`: "dial tcp 10.0.0.4:443: i/o timeout",
		errUnreachable.Error(): "lookup aks-a.privatelink.example.com: no such host",
		`Error: kubernetes cluster unreachable: the server has asked for the client to provide credentials`: "the server has asked for the client to provide credentials",
	}
	for msg, want := range cases {
		if got := releaseReadDetail(errors.New(msg)); got != want {
			t.Errorf("releaseReadDetail(%q) = %q, atteso %q", msg, got, want)
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
	kept := logKept(m.pendingDrift())
	if len(kept) != 1 || stripANSI(kept[0]) != "     app-esb resta a 3.0.11-dev, come sul cluster" {
		t.Errorf("tag mantenuti = %q", kept)
	}
}

// tagFormFor takes a model from the ACR read into the tag form.
func tagFormFor(t *testing.T, m PSNWorkflowModel, acr map[string][]string, names ...string) PSNWorkflowModel {
	t.Helper()
	m.selectedDeps = names
	next, _ := m.handleTagsLoaded(psnTagsLoadedMsg{tags: acr})
	m = next.(PSNWorkflowModel)
	if m.state != psnTagForm {
		t.Fatalf("stato = %v, atteso psnTagForm", m.state)
	}
	return m
}

func confirmTags(t *testing.T, m PSNWorkflowModel, tags map[int]string) PSNWorkflowModel {
	t.Helper()
	for row, tag := range tags {
		m.tagForm.rows[row].input.SetValue(tag)
	}
	next, _ := m.finishTagForm()
	return next.(PSNWorkflowModel)
}

func plainLog(lines []string) string {
	return stripANSI(strings.Join(lines, "\n"))
}

// Every selected service is asked in one frame, each proposal stepping past
// what ACR holds, and nothing is logged until the tags are confirmed.
func TestTagFormAsksEveryServiceAtOnce(t *testing.T) {
	m := readRelease(t, driftedModel(), psnReleaseReadMsg{deployed: deployedTags(map[string]string{
		"appEsb": "3.0.11-dev",
	})})
	before := len(m.log)
	m = tagFormFor(t, m, map[string][]string{
		"acr.azurecr.io/demo/app-esb": {"3.0.10-dev", "3.0.11-dev", "3.0.12-dev"},
	}, "app-be", "app-esb")

	rows := m.tagForm.rows
	if len(rows) != 2 {
		t.Fatalf("righe = %d, attese 2", len(rows))
	}
	if rows[0].current != "3.0.11-dev" || rows[0].suggested != "3.0.12-dev" || rows[0].skipped != "" {
		t.Errorf("app-be: %+v", rows[0])
	}
	if rows[1].current != "3.0.11-dev" || rows[1].suggested != "3.0.13-dev" || rows[1].skipped != "3.0.12-dev" {
		t.Errorf("app-esb: parte dal tag deployato e salta quello su ACR: %+v", rows[1])
	}
	if len(m.log) != before {
		t.Errorf("prima della conferma non va scritto nulla:\n%s", plainLog(m.log[before:]))
	}

	view := stripANSI(m.View().Content)
	for _, want := range []string{"Tag immagine  2 servizi", "su ACR: tag immagine 3.0.12-dev già presente", "▸ Servizi", "· Pipeline"} {
		if !strings.Contains(view, want) {
			t.Errorf("la vista non mostra %q:\n%s", want, view)
		}
	}
}

// Confirmed with nothing to ask, the builds start straight away: each header is
// complete from the start, and the notes about a tag sit under its service.
func TestConfirmedTagsStartTheBuilds(t *testing.T) {
	m := tagFormFor(t, readRelease(t, driftedModel(), psnReleaseReadMsg{}), nil, "app-be", "app-fe")
	before := len(m.log)
	m = confirmTags(t, m, map[int]string{1: "3.0.11-dev"})

	if m.state != psnBuilding || m.depIdx != 0 {
		t.Fatalf("stato %v, servizio %d: attesa la build del primo", m.state, m.depIdx)
	}
	section := plainLog(m.log[before:])
	if !strings.Contains(section, "1/2  app-be") || !strings.Contains(section, "3.0.11-dev → 3.0.12-dev") {
		t.Errorf("testata del primo servizio:\n%s", section)
	}
	if strings.Contains(section, "→ …") || strings.Contains(section, "app-fe") {
		t.Errorf("solo il primo servizio, con la testata completa:\n%s", section)
	}
	if !strings.Contains(section, "Dockerfile (simulato)") {
		t.Errorf("le righe del Dockerfile stanno nella sezione del servizio:\n%s", section)
	}
	if !strings.Contains(stripANSI(m.renderTracker()), "APP-BE      ⣾") && !strings.Contains(stripANSI(m.renderTracker()), "Build") {
		t.Errorf("riga del servizio:\n%s", stripANSI(m.renderTracker()))
	}
	if strings.Contains(stripANSI(m.renderTracker()), "Config") {
		t.Errorf("la fase Config non c'è più:\n%s", stripANSI(m.renderTracker()))
	}

	// The second service opens when the first is pushed, still without asking.
	m.depIdx = 1
	at := len(m.log)
	next, _ := m.startNextDeployment()
	m = next.(PSNWorkflowModel)
	section = plainLog(m.log[at:])
	if m.state != psnBuilding || !strings.Contains(section, "2/2  app-fe") ||
		!strings.Contains(section, "invariato") || !strings.Contains(section, "Il manifest non cambia") {
		t.Errorf("secondo servizio, stato %v:\n%s", m.state, section)
	}
}

// Tags already on ACR are asked about together. Changing goes back to the form
// on the first of them, with what was typed; overwriting marks them all.
func TestTakenTagsAreAskedTogether(t *testing.T) {
	acr := map[string][]string{
		"acr.azurecr.io/demo/app-be": {"3.0.11-dev", "3.0.12-dev", "3.0.13-dev"},
		"acr.azurecr.io/demo/app-fe": {"3.0.11-dev", "3.0.12-dev"},
	}
	m := tagFormFor(t, readRelease(t, driftedModel(), psnReleaseReadMsg{}), acr, "app-be", "app-esb", "app-fe")
	m = confirmTags(t, m, map[int]string{0: "3.0.12-dev", 2: "3.0.12-dev"})

	if m.state != psnTagExists || !slices.Equal(m.taken, []int{0, 2}) {
		t.Fatalf("stato %v, in conflitto %v", m.state, m.taken)
	}
	view := stripANSI(m.View().Content)
	for _, want := range []string{"Tag già presenti su ACR", "app-be   3.0.12-dev", "app-fe   3.0.12-dev", "le immagini esistenti"} {
		if !strings.Contains(view, want) {
			t.Errorf("la domanda non mostra %q:\n%s", want, view)
		}
	}
	if m.list.items[m.list.cursor].Value != "change" {
		t.Error("la scelta preselezionata deve essere cambiare tag")
	}

	change := m
	change.list.selected = "change"
	next, _ := change.finishTagExists()
	back := next.(PSNWorkflowModel)
	if back.state != psnTagForm || back.tagForm.cursor != 0 || back.tagForm.done {
		t.Errorf("Cambia tag: stato %v, cursore %d, done %v", back.state, back.tagForm.cursor, back.tagForm.done)
	}
	if got := back.tagForm.values(); got[0] != "3.0.12-dev" || got[2] != "3.0.12-dev" {
		t.Errorf("i tag digitati vanno conservati: %v", got)
	}

	over := m
	over.list.selected = "overwrite"
	before := len(over.log)
	next, _ = over.finishTagExists()
	over = next.(PSNWorkflowModel)
	if over.state != psnBuilding {
		t.Fatalf("Sovrascrivi: stato %v", over.state)
	}
	if !over.plan[0].overwrite || over.plan[1].overwrite || !over.plan[2].overwrite {
		t.Errorf("sovrascritture = %v %v %v", over.plan[0].overwrite, over.plan[1].overwrite, over.plan[2].overwrite)
	}
	if section := plainLog(over.log[before:]); !strings.Contains(section, "Il push sostituisce 3.0.12-dev su ACR.") {
		t.Errorf("la sovrascrittura va dichiarata sotto il servizio:\n%s", section)
	}
}

// The running tag is on ACR by definition: redeploying it is the unchanged-tag
// path, not a collision; tags that could not be read are not checked.
func TestRunningTagAndUnreadTagsAreNotAsked(t *testing.T) {
	m := tagFormFor(t, readRelease(t, driftedModel(), psnReleaseReadMsg{}),
		map[string][]string{"acr.azurecr.io/demo/app-be": {"3.0.11-dev"}}, "app-be")
	if got := confirmTags(t, m, map[int]string{0: "3.0.11-dev"}); got.state == psnTagExists {
		t.Error("il tag in esecuzione non deve chiedere conferma di sovrascrittura")
	}

	m = tagFormFor(t, readRelease(t, driftedModel(), psnReleaseReadMsg{}), nil, "app-be")
	if got := confirmTags(t, m, map[int]string{0: "3.0.1-dev"}); got.state == psnTagExists {
		t.Error("senza tag letti da ACR non c'è niente da confrontare")
	}
}

// A Dockerfile question is asked before any build, under the section of its
// service; once answered the section leaves the log, and comes back when the
// builds reach that service.
func TestDockerfileQuestionsComeBeforeTheBuilds(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"app-be", "other"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := readRelease(t, driftedModel(), psnReleaseReadMsg{})
	m.testUI = false
	m.project = &config.PSNProjectConfig{Namespace: "*", DockerRoot: root}
	m = tagFormFor(t, m, nil, "app-be", "app-fe")
	before := len(m.log)
	m = confirmTags(t, m, nil)

	// app-be has <name>/Dockerfile; app-fe has none, and the scan is a question.
	if m.state != psnDockerfileList || m.prepIdx != 1 {
		t.Fatalf("stato %v, servizio in preparazione %d", m.state, m.prepIdx)
	}
	asking := plainLog(m.log[before:])
	if !strings.Contains(asking, "2/2  app-fe") || !strings.Contains(asking, "Scansione") || strings.Contains(asking, "app-be") {
		t.Errorf("durante la domanda si vede solo la sezione di app-fe:\n%s", asking)
	}
	if m.plan[0].dockerfile != filepath.Join(root, "app-be", "Dockerfile") ||
		!strings.Contains(plainLog(m.plan[0].lines), "Dockerfile") {
		t.Errorf("app-be già risolto: %q %q", m.plan[0].dockerfile, m.plan[0].lines)
	}
	if strings.Contains(stripANSI(m.renderTracker()), "Pipeline [") {
		t.Errorf("la pipeline non è ancora partita:\n%s", stripANSI(m.renderTracker()))
	}

	chosen := filepath.Join(root, "other", "Dockerfile")
	m.list.selected = chosen
	next, _ := m.finishDockerfile()
	m = next.(PSNWorkflowModel)
	if m.state != psnBuilding || m.depIdx != 0 {
		t.Fatalf("dopo la risposta: stato %v, servizio %d", m.state, m.depIdx)
	}
	building := plainLog(m.log[before:])
	if !strings.HasPrefix(strings.TrimLeft(building, "\n"), "──  1/2  app-be") || strings.Contains(building, "app-fe") {
		t.Errorf("la build parte dal primo servizio, la sezione di app-fe aspetta il suo turno:\n%s", building)
	}
	if m.plan[1].dockerfile != chosen || !strings.Contains(plainLog(m.plan[1].lines), "Scansione") {
		t.Errorf("app-fe: %q %q", m.plan[1].dockerfile, m.plan[1].lines)
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

	if m.state != psnTagForm {
		t.Fatalf("stato = %v, atteso psnTagForm", m.state)
	}
	log := plainLog(m.log)
	if !strings.Contains(log, "non verificabili") || !strings.Contains(log, "app-fe: az: timeout") {
		t.Errorf("il log non riporta l'errore di app-fe:\n%s", log)
	}
	if _, ok := m.acrTags["acr.azurecr.io/demo/app-fe"]; ok {
		t.Error("un repository non letto non deve risultare senza tag")
	}
}

// A read that went through has nothing to report.
func TestTagsLoadedQuietly(t *testing.T) {
	m := readRelease(t, driftedModel(), psnReleaseReadMsg{})
	before := len(m.log)
	m = tagFormFor(t, m, map[string][]string{"acr.azurecr.io/demo/app-be": {"3.0.11-dev"}}, "app-be")
	if len(m.log) != before {
		t.Errorf("righe aggiunte:\n%s", plainLog(m.log[before:]))
	}
}

// The drift report keeps its tags in one column whatever the service names.
func TestDriftLinesAlignTheirTags(t *testing.T) {
	lines := logDrift([]logic.TagDrift{
		{Service: "app-be", Values: "3.0.9-dev", Deployed: "3.0.11-dev"},
		{Service: "app-webapp-public", Values: "1.0.0", Deployed: "1.0.2"},
		{Service: "app-be", Values: "3.0.9-dev", Deployed: "3.0.11-dev"},
	}, "0.9s")

	if len(lines) != 3 {
		t.Fatalf("righe = %d, attese 3 (passo e due servizi):\n%s", len(lines), stripANSI(strings.Join(lines, "\n")))
	}
	if !strings.Contains(stripANSI(lines[0]), "2 tag diversi dal values") {
		t.Errorf("passo = %q", stripANSI(lines[0]))
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > logWidth {
			t.Errorf("riga larga %d, oltre la griglia (%d): %s", w, logWidth, stripANSI(l))
		}
	}
	if strings.Index(stripANSI(lines[1]), "3.0.9-dev") != strings.Index(stripANSI(lines[2]), "1.0.0") {
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

	if m.state != psnTagForm || m.tagForm.rows[0].name != "webapp" {
		t.Fatalf("stato %v: atteso il riquadro dei tag", m.state)
	}
	if r := m.tagForm.rows[0]; r.suggested != "1.0.2" || r.skipped != "1.0.1" {
		t.Errorf("webapp: proposto %q (saltato %q), atteso 1.0.2 perché 1.0.1 è già su ACR", r.suggested, r.skipped)
	}
	if view := stripANSI(m.View().Content); !strings.Contains(view, "su ACR: tag immagine 1.0.1 già presente") {
		t.Errorf("il salto della proposta non si vede:\n%s", view)
	}
}
