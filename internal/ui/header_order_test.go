package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Hub-cli/internal/config"
	"Hub-cli/internal/logic"

	"charm.land/lipgloss/v2"
)

// Before the tag is chosen the header already stands, with the tag half
// pending — and it is not mistaken for an unchanged tag.
func TestServiceHeaderWithPendingTag(t *testing.T) {
	line := strings.Trim(logServiceHeader(1, 1, "webapp", "1.0.0", ""), "\n")

	if w := lipgloss.Width(line); w != logWidth {
		t.Errorf("testata larga %d invece di %d: %s", w, logWidth, stripANSI(line))
	}
	plain := stripANSI(line)
	if !strings.Contains(plain, "1.0.0 → …") {
		t.Errorf("tag in sospeso non mostrato: %q", plain)
	}
	if strings.Contains(plain, "invariato") {
		t.Errorf("un tag non ancora scelto non è un tag invariato: %q", plain)
	}
}

// In the local workflow the Dockerfile is resolved before the tag is asked, so
// its lines used to be logged above the header of their own service. The header
// now opens the service, and the tag is filled in where the header stands.
func TestLocalServiceLinesLandUnderTheirHeader(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "portal", "webapp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "portal", "webapp", "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Services: map[string]config.ServiceConfig{
		"portal-webapp": {
			DockerfileSubpath: "portal/webapp/Dockerfile",
			ECRRepository:     "123.dkr.ecr.eu-west-1.amazonaws.com/portal/webapp",
			LastTag:           "1.0.0",
		},
	}}
	cfg.Config.DockerRootPath = root

	m := WorkflowModel{cfg: cfg, testUI: true, selectedServices: []string{"portal-webapp"}}
	next, _ := m.startNextService()
	m = next.(WorkflowModel)

	header := indexOf(m.log, "portal-webapp")
	dockerfile := indexOf(m.log, "Dockerfile")
	if header < 0 || dockerfile < 0 {
		t.Fatalf("testata o Dockerfile mancanti nel log:\n%s", stripANSI(strings.Join(m.log, "\n")))
	}
	if dockerfile < header {
		t.Errorf("la riga del Dockerfile (%d) sta sopra la testata (%d)", dockerfile, header)
	}
	if !strings.Contains(stripANSI(m.log[header]), "→ …") {
		t.Errorf("prima della scelta il tag deve essere in sospeso: %q", stripANSI(m.log[header]))
	}

	next, _ = m.finishTagInput()
	m = next.(WorkflowModel)
	if got := stripANSI(m.log[header]); !strings.Contains(got, "1.0.0 → 1.0.1") {
		t.Errorf("la testata non è stata completata sul posto: %q", got)
	}
	if indexOf(m.log, "portal-webapp") != header {
		t.Error("la testata è stata aggiunta di nuovo invece di essere completata")
	}
}

// PSN asks the tag first: with the header already standing, the question is
// asked under the section it belongs to.
func TestPSNHeaderStandsWhileTheTagIsAsked(t *testing.T) {
	m := PSNWorkflowModel{
		testUI:       true,
		selectedDeps: []string{"webapp"},
		svcByName: map[string]logic.HelmService{
			"webapp": {Name: "webapp", Repository: "acr.azurecr.io/demo/webapp", Tag: "1.0.0"},
		},
	}
	next, _ := m.startNextDeployment()
	m = next.(PSNWorkflowModel)

	if m.state != psnTagInput {
		t.Fatalf("stato = %v, atteso psnTagInput", m.state)
	}
	if len(m.log) == 0 || !strings.Contains(stripANSI(m.log[m.svcLogStart]), "1.0.0 → …") {
		t.Fatalf("durante la domanda del tag la testata deve già esserci:\n%s", stripANSI(strings.Join(m.log, "\n")))
	}

	start := m.svcLogStart
	next, _ = m.finishTagInput()
	m = next.(PSNWorkflowModel)
	if got := stripANSI(m.log[start]); !strings.Contains(got, "1.0.0 → 1.0.1") {
		t.Errorf("la testata non è stata completata sul posto: %q", got)
	}
}

// A long service name pushes the tag column right instead of running into it,
// and every service's tags start in the same column.
func TestSummaryColumnsFollowTheLongestName(t *testing.T) {
	SetSummaryContext("Locale  ·  eu-west-1", "2 servizi  ·  2 revisioni helm")
	out := capture(t, func() {
		PrintDeploySummary([]DeployResult{
			{Service: "jboss-be", OldTag: "3.0.9-dev", NewTag: "3.0.10-dev", Elapsed: 4 * 60e9},
			{Service: "portal-webapp-private", OldTag: "1.0.0-staging", NewTag: "1.0.1-staging", Elapsed: 12.9e9},
		})
	})

	var rows []string
	width := -1
	for _, line := range strings.Split(strings.Trim(out, "\n"), "\n") {
		if w := lipgloss.Width(line); width < 0 {
			width = w
		} else if w != width {
			t.Errorf("bordo non allineato: riga larga %d, la prima %d", w, width)
		}
		plain := stripANSI(line)
		if strings.Contains(plain, "jboss-be") || strings.Contains(plain, "portal-webapp-private") {
			rows = append(rows, plain)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("righe dei servizi = %d, attese 2:\n%s", len(rows), stripANSI(out))
	}
	if !strings.Contains(rows[1], "portal-webapp-private  1.0.0-staging") {
		t.Errorf("il tag non parte due spazi dopo il nome più lungo: %q", rows[1])
	}
	if strings.Index(rows[0], "3.0.9-dev") != strings.Index(rows[1], "1.0.0-staging") {
		t.Errorf("i tag non partono dalla stessa colonna:\n%s\n%s", rows[0], rows[1])
	}
}

// Short names keep the table exactly as it was: the adaptive columns only move
// when something does not fit.
func TestSummaryKeepsItsGeometryForShortNames(t *testing.T) {
	SetSummaryContext("Cluster A — Collaudo  ·  app-coll", "1 servizio  ·  1 revisione helm")
	out := capture(t, func() {
		PrintDeploySummary([]DeployResult{
			{Service: "webapp", OldTag: "3.0.9-dev", NewTag: "3.0.10-dev", Elapsed: 139e9},
		})
	})
	top := strings.Split(strings.Trim(out, "\n"), "\n")[0]
	if w := lipgloss.Width(top); w != 2+1+sumContent+4+1 {
		t.Errorf("riquadro largo %d, atteso %d", w, 2+1+sumContent+4+1)
	}
}

func indexOf(log []string, needle string) int {
	for i, line := range log {
		if strings.Contains(stripANSI(line), needle) {
			return i
		}
	}
	return -1
}
