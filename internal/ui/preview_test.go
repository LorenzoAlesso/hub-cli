package ui

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestPreviewRun is not an assertion but a viewer: `go test -run Preview -v`
// prints a whole run through the real renderers, which is the only way to look
// at a TUI without driving a terminal. It fails nothing.
func TestPreviewRun(t *testing.T) {
	if testing.Short() {
		t.Skip("anteprima: solo su richiesta")
	}
	frame := "⠋"

	fmt.Println()
	fmt.Println(trackerRow([]string{
		trackerTab(trackDone, "Azure", frame),
		trackerTab(trackDone, "Chart", frame),
		trackerTab(trackDone, "Servizi", frame),
		trackerTab(trackInteractive, "Pipeline [3/3]", frame),
		trackerTab(trackPending, "Rilascio", frame),
		trackerTab(trackPending, "Sync", frame),
	}))
	fmt.Println(trackerRule(80))
	fmt.Println(stageRow("webapp", []string{
		trackerTab(trackDone, "Config", frame),
		trackerTab(trackDone, "Build", frame),
		trackerTab(trackSpinning, "Push", frame),
	}))

	fmt.Println()
	PrintRunHeader("Ambiente PSN: ", "Cluster A — Collaudo", "app-coll",
		[]string{"app-site-a-coll", "chart app", "dev-site-a", "site-a-pre-prod"})
	fmt.Println(logDone("Sessione Azure", "", "attiva"))
	fmt.Println(logDone("Credenziali del cluster", "", "3.6s"))
	fmt.Println(logDone("Chart", "origin/dev-site-a · 0.7.0", "1.3s"))
	fmt.Println(logDone("Progetto Docker", "origin/site-a-pre-prod", "1.3s"))

	fmt.Println(logServiceHeader(1, 3, "jboss-be", "3.0.10-dev", "3.0.10-dev"))
	for _, l := range logWarn(
		"Il manifest non cambia: il pod non riparte da solo.",
		"A fine deploy hub-cli propone il riavvio.") {
		fmt.Println(l)
	}
	fmt.Println(logInfo("Immagine", "exampleacr.azurecr.io/apps/jboss-be:3.0.10-dev"))
	fmt.Println(logInfo("Dockerfile", "~/.hub-cli/repos/acme-docker/jboss-be/Dockerfile"))
	fmt.Println()
	fmt.Println(logDone("Build", "", "2m 43s"))
	fmt.Println(logDone("Push", "", "1m 04s"))

	fmt.Println(logSection("Rilascio", "app-site-a-coll"))
	fmt.Println(logDone("helm upgrade", "3 servizi · 1 revisione", "7.4s"))
	fmt.Println(logStep("↻", CursorStyle, "Riavvio jboss-be", "tag invariato", "18.4s"))
	fmt.Println(logDone("Sync del values", "dev-site-a", "1.9s"))

	fmt.Println()
	fmt.Println(questionBox(
		SelectedItemStyle.Render("Tag invariato per jboss-be — riavviare?"), CursorStyle,
		itemLines([]Item{
			{Label: "Riavvia", Desc: "rollout restart; per qualche decina di secondi girano due istanze"},
			{Label: "Salta", Desc: "il pod continua a girare l'immagine precedente"},
		}, 0, 0, 100)))

	fmt.Println()
	for _, l := range logFailure("helm upgrade", errors.New("helm upgrade fallito: exit status 1"), "4.1s") {
		fmt.Println(l)
	}
	fmt.Println(logOutput([]byte("Error: UPGRADE FAILED: timed out waiting for the condition")))
	fmt.Println(questionBox(ErrStyle.Render("Cosa vuoi fare?"), ErrStyle,
		itemLines([]Item{
			{Label: "Riprova", Desc: "riesegue helm upgrade"},
			{Label: "Annulla", Desc: "il release resta invariato, le immagini restano su ACR"},
		}, 0, 0, 100)))

	SetSummaryContext("Cluster A — Collaudo  ·  app-coll",
		"3 servizi  ·  1 revisione helm  ·  values su dev-site-a")
	PrintDeploySummary([]DeployResult{
		{Service: "jboss-be", OldTag: "3.0.10-dev", NewTag: "3.0.10-dev",
			Elapsed: 3*time.Minute + 47*time.Second, Restarted: true},
		{Service: "jboss-fe", OldTag: "3.0.9-dev", NewTag: "3.0.10-dev",
			Elapsed: 4*time.Minute + 55*time.Second},
		{Service: "webapp", OldTag: "3.0.9-dev", NewTag: "3.0.10-dev",
			Elapsed: 2*time.Minute + 19*time.Second},
	})
	fmt.Println()
}
