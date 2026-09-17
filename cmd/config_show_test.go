package cmd

import (
	"regexp"
	"strings"
	"testing"

	"Hub-cli/internal/config"

	"charm.land/lipgloss/v2"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(lines []string) string {
	return ansi.ReplaceAllString(joinLines(lines), "")
}

// inOrder fails unless every item appears in out, each after the previous one.
func inOrder(t *testing.T, out string, items ...string) {
	t.Helper()
	at := 0
	for _, want := range items {
		idx := strings.Index(out[at:], want)
		if idx < 0 {
			t.Fatalf("manca %q (o è fuori ordine) in:\n%s", want, out)
		}
		at += idx + len(want)
	}
}

func samplePSN() config.PSNConfig {
	return config.PSNConfig{
		TenantID: "00000000-0000-0000-0000-000000000000",
		Clusters: []config.PSNClusterConfig{
			{
				Name: "Cluster A — Collaudo", Env: "coll",
				SubscriptionID: "sub-a", ResourceGroup: "rg-a", AKSName: "aks-a", ACRName: "acra",
				Releases: []config.PSNReleaseConfig{
					{Name: "app-site-a-coll", Namespace: "app-site-a-col", Chart: "app",
						Values: "app/values-site-a-coll.yaml", ChartsBranch: "dev-site-a"},
					{Name: "vault", Namespace: "vault-col", Chart: "vault",
						Values: "vault/values-coll.yaml", ChartsBranch: "dev-site-a"},
				},
			},
			{
				Name: "Cluster B — Produzione", Env: "prod",
				SubscriptionID: "sub-b", ResourceGroup: "rg-b", AKSName: "aks-b", ACRName: "acrb",
			},
		},
		Projects: []config.PSNProjectConfig{
			{Namespace: "app-site-b-*", DockerRoot: `C:\repos\docker-b`},
			{Namespace: "app-site-a-*", DockerRoot: `C:\repos\docker`,
				BranchColl: "site-a-pre-prod", BranchProd: "site-a-prod"},
		},
		Deployments: map[string]string{"webapp": "app-webapp", "api-be": "app-api-be"},
	}
}

// Each release says where its chart comes from and which project and branch it
// builds from: the two branches a run depends on, side by side. Projects keep
// the configured order, which is the order a namespace is matched in.
func TestConfigShowPSNPerRelease(t *testing.T) {
	inOrder(t, plain(psnLines(samplePSN())),
		"PSN",
		"Tenant:",
		"[Cluster A — Collaudo]  collaudo",
		"Release:         app-site-a-coll",
		"Branch chart:  dev-site-a",
		`Progetto:      C:\repos\docker`,
		"Branch build:  site-a-pre-prod",
		"Release:         vault",
		"Progetto:      nessuno",
		"[Cluster B — Produzione]  PRODUZIONE",
		"Release:         nessuno",
		"PSN · Progetti",
		"[app-site-b-*]",
		"[app-site-a-*]",
		"Branch prod:     site-a-prod",
		"PSN · Mapping",
		"api-be",
		"webapp",
	)
}

func TestConfigShowWithoutPSN(t *testing.T) {
	if out := plain(psnLines(config.PSNConfig{})); !strings.Contains(out, "Blocco psn non configurato") {
		t.Errorf("senza blocco psn va detto:\n%s", out)
	}
}

// The same configuration prints the same way every time.
func TestConfigShowSortsServices(t *testing.T) {
	cfg := &config.Config{Services: map[string]config.ServiceConfig{
		"app-webapp": {}, "app-gateway": {}, "app-jboss-be": {}, "app-consul": {},
	}}
	inOrder(t, plain(localLines(cfg)),
		"Locale", "Branch chart (locale):",
		"[app-consul]", "[app-gateway]", "[app-jboss-be]", "[app-webapp]")
}

// Wide enough, the two environments sit side by side: every row carries the
// rule in the same column, and the right block starts where the left one does.
func TestShowColumnsSideBySide(t *testing.T) {
	left := []string{"Locale", "", "  ECR Region:  eu-west-1", "  a long local line, the widest of the block"}
	right := []string{"PSN", "", "  Tenant:  00000000"}

	out := ansi.ReplaceAllString(showColumns(left, right, 120), "")
	rows := strings.Split(out, "\n")
	if len(rows) != len(left) {
		t.Fatalf("righe = %d, attese %d:\n%s", len(rows), len(left), out)
	}
	if !strings.HasPrefix(rows[0], "Locale") || !strings.HasSuffix(rows[0], "PSN") {
		t.Errorf("le intestazioni devono stare sulla stessa riga: %q", rows[0])
	}

	col := -1
	for i, row := range rows {
		idx := strings.Index(row, "│")
		if idx < 0 {
			t.Fatalf("riga %d senza separatore: %q", i, row)
		}
		if w := lipgloss.Width(row[:idx]); col < 0 {
			col = w
		} else if w != col {
			t.Errorf("riga %d: separatore in colonna %d invece di %d", i, w, col)
		}
		if row != strings.TrimRight(row, " ") {
			t.Errorf("riga %d con spazi in coda: %q", i, row)
		}
	}
	if want := blockWidth(left) + columnGap; col != want {
		t.Errorf("separatore in colonna %d, atteso %d", col, want)
	}
}

// Too narrow for both, or not a terminal at all: one block under the other,
// with no rule.
func TestShowColumnsFallsBackToAList(t *testing.T) {
	left := []string{"Locale", "  a local line that is fairly long"}
	right := []string{"PSN", "  a psn line"}
	need := blockWidth(left) + 2*columnGap + 1 + blockWidth(right)

	for _, width := range []int{0, 40, need} {
		out := ansi.ReplaceAllString(showColumns(left, right, width), "")
		if strings.Contains(out, "│") {
			t.Errorf("larghezza %d: niente colonne, ma c'è il separatore:\n%s", width, out)
		}
		if want := joinLines(left) + "\n\n" + joinLines(right); out != want {
			t.Errorf("larghezza %d: elenco = %q, atteso %q", width, out, want)
		}
	}
	if out := showColumns(left, right, need+1); !strings.Contains(out, "│") {
		t.Errorf("con una colonna in più le due parti stanno affiancate:\n%s", out)
	}
}
