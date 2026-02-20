package canon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAIProviderRuntimeReadyCodexWithoutAPIKeyEnv(t *testing.T) {
	binDir := t.TempDir()
	codexPath := filepath.Join(binDir, "codex")
	script := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(codexPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed writing fake codex binary: %v", err)
	}

	t.Setenv("PATH", binDir)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEX_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	if !aiProviderRuntimeReady("codex") {
		t.Fatalf("expected codex runtime to be ready when binary exists without API key env")
	}
}

func TestAIProviderRuntimeReadyRejectsUnsupportedProvider(t *testing.T) {
	if aiProviderRuntimeReady("unsupported-provider") {
		t.Fatalf("expected unsupported provider to be not ready")
	}
}

func TestAIRenderTimeoutRespectsEnvOverride(t *testing.T) {
	t.Setenv("CANON_AI_RENDER_TIMEOUT_SECONDS", "")
	if got := aiRenderTimeout(); got != 10*time.Minute {
		t.Fatalf("expected default 10m timeout, got %s", got)
	}

	t.Setenv("CANON_AI_RENDER_TIMEOUT_SECONDS", "0")
	if got := aiRenderTimeout(); got != 0 {
		t.Fatalf("expected zero timeout when explicitly disabled, got %s", got)
	}

	t.Setenv("CANON_AI_RENDER_TIMEOUT_SECONDS", "900")
	if got := aiRenderTimeout(); got != 15*time.Minute {
		t.Fatalf("expected configured timeout override, got %s", got)
	}

	t.Setenv("CANON_AI_RENDER_TIMEOUT_SECONDS", "-5")
	if got := aiRenderTimeout(); got != 10*time.Minute {
		t.Fatalf("expected default timeout for negative override, got %s", got)
	}
}
