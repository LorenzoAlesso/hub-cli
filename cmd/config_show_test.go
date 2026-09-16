package cmd

import (
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"Hub-cli/internal/config"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = orig
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return ansi.ReplaceAllString(string(out), "")
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
			{Namespace: "app-site-a-*", DockerRoot: `C:\repos\docker`,
				BranchColl: "site-a-pre-prod", BranchProd: "site-a-prod"},
		},
		Deployments: map[string]string{"webapp": "app-webapp", "api-be": "app-api-be"},
	}
}

// Each release says where its chart comes from and which project and branch it
// builds from: the two branches a run depends on, side by side.
func TestConfigShowPSNPerRelease(t *testing.T) {
	out := captureStdout(t, func() { printPSNConfig(samplePSN()) })

	order := []string{
		"[Cluster A — Collaudo]  collaudo",
		"Release:         app-site-a-coll",
		"Branch chart:  dev-site-a",
		`Progetto:      C:\repos\docker`,
		"Branch build:  site-a-pre-prod",
		"Release:         vault",
		"Progetto:      nessuno",
		"[Cluster B — Produzione]  PRODUZIONE",
		"Release:         nessuno",
		"[app-site-a-*]",
		"Branch prod:     site-a-prod",
		"api-be",
		"webapp",
	}
	at := 0
	for _, want := range order {
		idx := strings.Index(out[at:], want)
		if idx < 0 {
			t.Fatalf("manca %q (o è fuori ordine) in:\n%s", want, out)
		}
		at += idx + len(want)
	}
}

func TestConfigShowWithoutPSN(t *testing.T) {
	out := captureStdout(t, func() { printPSNConfig(config.PSNConfig{}) })
	if !strings.Contains(out, "Blocco psn non configurato") {
		t.Errorf("senza blocco psn va detto:\n%s", out)
	}
}

// The same configuration prints the same way every time.
func TestConfigShowSortsServices(t *testing.T) {
	services := map[string]config.ServiceConfig{
		"app-webapp": {}, "app-gateway": {}, "app-jboss-be": {}, "app-consul": {},
	}
	out := captureStdout(t, func() { printLocalServices(services) })

	at := 0
	for _, name := range []string{"[app-consul]", "[app-gateway]", "[app-jboss-be]", "[app-webapp]"} {
		idx := strings.Index(out[at:], name)
		if idx < 0 {
			t.Fatalf("%s manca o è fuori ordine:\n%s", name, out)
		}
		at += idx + len(name)
	}
}
