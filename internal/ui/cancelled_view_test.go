package ui

import (
	"strings"
	"testing"
)

// A run that fails while aligning a repo quits in that step's state: the last
// frame must show the error alone, not the step still spinning beneath it.
func TestCancelledRunDropsTheRunningLine(t *testing.T) {
	const label = "Allineamento repo chart (feature-x)"

	psn := PSNWorkflowModel{state: psnChartsPrep, spinner: newSpinnerModel(label)}
	if !strings.Contains(psn.View().Content, label) {
		t.Fatal("PSN: durante il passo la riga in corso deve esserci")
	}
	psn.cancelled = true
	if strings.Contains(psn.View().Content, label) {
		t.Error("PSN: a run annullato la riga in corso non va disegnata")
	}

	local := WorkflowModel{state: wfRepoPrep, spinner: newSpinnerModel(label)}
	if !strings.Contains(local.View().Content, label) {
		t.Fatal("locale: durante il passo la riga in corso deve esserci")
	}
	local.cancelled = true
	if strings.Contains(local.View().Content, label) {
		t.Error("locale: a run annullato la riga in corso non va disegnata")
	}
}
