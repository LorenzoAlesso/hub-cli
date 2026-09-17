package config

import (
	"os"
	"path/filepath"
	"testing"
)

// withFakeHome points the home-directory lookup at a temporary directory and
// resets the package state the migration records into.
func withFakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home) // Windows
	t.Setenv("HOME", home)        // everywhere else
	migratedFiles = nil
	t.Cleanup(func() { migratedFiles = nil })
	return home
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLegacyFiles(t *testing.T) {
	home := withFakeHome(t)
	write(t, filepath.Join(home, ".hub-cli.yaml"), "config:\n  theme: dracula\n")
	write(t, filepath.Join(home, ".hub-cli.seed.yaml"), "services: {}\n")

	if err := migrateLegacyFiles(); err != nil {
		t.Fatalf("migrazione: %v", err)
	}

	cfg, err := os.ReadFile(filepath.Join(home, ".hub-cli", "config.yaml"))
	if err != nil {
		t.Fatalf("config non spostata: %v", err)
	}
	if string(cfg) != "config:\n  theme: dracula\n" {
		t.Errorf("contenuto alterato dalla migrazione: %q", cfg)
	}
	if _, err := os.Stat(filepath.Join(home, ".hub-cli", "seed.yaml")); err != nil {
		t.Errorf("seed non spostato: %v", err)
	}

	// The old paths must be gone, or an older build would keep using them.
	if _, err := os.Stat(filepath.Join(home, ".hub-cli.yaml")); err == nil {
		t.Error("il vecchio file di config è ancora al suo posto")
	}
	if len(MigratedFiles()) != 2 {
		t.Errorf("spostamenti riportati = %d, attesi 2", len(MigratedFiles()))
	}
}

// A config already living in the state directory is the current one: a stale
// dotfile left behind must never overwrite it.
func TestMigrateLegacyFilesNeverOverwrites(t *testing.T) {
	home := withFakeHome(t)
	dir := filepath.Join(home, ".hub-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "config.yaml"), "config:\n  theme: nuovo\n")
	write(t, filepath.Join(home, ".hub-cli.yaml"), "config:\n  theme: vecchio\n")

	if err := migrateLegacyFiles(); err != nil {
		t.Fatalf("migrazione: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if string(got) != "config:\n  theme: nuovo\n" {
		t.Errorf("la config corrente è stata sovrascritta: %q", got)
	}
	if len(MigratedFiles()) != 0 {
		t.Errorf("nessuno spostamento atteso, riportati %v", MigratedFiles())
	}
}

func TestMigrateLegacyFilesIsIdempotent(t *testing.T) {
	home := withFakeHome(t)
	write(t, filepath.Join(home, ".hub-cli.yaml"), "config: {}\n")

	for i := range 2 {
		if err := migrateLegacyFiles(); err != nil {
			t.Fatalf("migrazione %d: %v", i+1, err)
		}
	}
	if len(MigratedFiles()) != 1 {
		t.Errorf("spostamenti = %d, atteso 1: la seconda passata non deve fare nulla", len(MigratedFiles()))
	}
}

func TestStatePathsShareOneDirectory(t *testing.T) {
	home := withFakeHome(t)

	dir, err := StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(home, ".hub-cli") {
		t.Errorf("StateDir = %q", dir)
	}

	cfgPath, err := ConfigFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(cfgPath) != dir {
		t.Errorf("config fuori dalla state dir: %q", cfgPath)
	}
	if filepath.Dir(SeedFilePath()) != dir {
		t.Errorf("seed fuori dalla state dir: %q", SeedFilePath())
	}
}
