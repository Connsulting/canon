package canon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaultsWhenNoFiles(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.AI.Provider != "codex" {
		t.Fatalf("expected default provider codex, got %s", cfg.AI.Provider)
	}
}

func TestLoadConfigGlobalThenLocalOverride(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".canonconfig")
	if err := os.WriteFile(globalPath, []byte("[ai]\nprovider = claude\n"), 0o644); err != nil {
		t.Fatalf("failed writing global config: %v", err)
	}

	localPath := filepath.Join(root, ".canonconfig")
	if err := os.WriteFile(localPath, []byte("[ai]\nprovider = codex\n"), 0o644); err != nil {
		t.Fatalf("failed writing local config: %v", err)
	}

	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.AI.Provider != "codex" {
		t.Fatalf("expected local provider override codex, got %s", cfg.AI.Provider)
	}

	otherRoot := t.TempDir()
	cfg, err = LoadConfig(otherRoot)
	if err != nil {
		t.Fatalf("LoadConfig failed for other root: %v", err)
	}
	if cfg.AI.Provider != "claude" {
		t.Fatalf("expected global provider claude, got %s", cfg.AI.Provider)
	}
}
