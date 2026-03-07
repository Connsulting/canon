package canon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type aiResponseHelperFixture struct {
	Model   string `json:"model"`
	Summary string `json:"summary"`
	Nested  struct {
		Count int `json:"count"`
	} `json:"nested"`
}

func TestReadAIResponseFileRejectsEmptyPath(t *testing.T) {
	_, err := readAIResponseFile(t.TempDir(), "   ")
	if !errors.Is(err, errAIResponseFilePathRequired) {
		t.Fatalf("expected required-path sentinel error, got %v", err)
	}
}

func TestReadAIResponseFileSupportsRelativeAndAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	relPath := filepath.Join(root, "response.json")
	content := []byte("{\"model\":\"headless\"}")
	if err := os.WriteFile(relPath, content, 0o644); err != nil {
		t.Fatalf("failed writing fixture: %v", err)
	}

	relBytes, err := readAIResponseFile(root, "response.json")
	if err != nil {
		t.Fatalf("relative read failed: %v", err)
	}
	if string(relBytes) != string(content) {
		t.Fatalf("unexpected relative read bytes: %q", string(relBytes))
	}

	absBytes, err := readAIResponseFile(root, relPath)
	if err != nil {
		t.Fatalf("absolute read failed: %v", err)
	}
	if string(absBytes) != string(content) {
		t.Fatalf("unexpected absolute read bytes: %q", string(absBytes))
	}
}

func TestDecodeAIResponseJSONDirectPayload(t *testing.T) {
	payload := []byte(`{"model":"codex","summary":"ok","nested":{"count":2}}`)
	decoded, err := decodeAIResponseJSON[aiResponseHelperFixture](payload, "invalid fixture JSON")
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Model != "codex" || decoded.Summary != "ok" || decoded.Nested.Count != 2 {
		t.Fatalf("unexpected decoded payload: %+v", decoded)
	}
}

func TestDecodeAIResponseJSONExtractsWrappedPayload(t *testing.T) {
	payload := []byte("analysis:\n{\"model\":\"codex\",\"summary\":\"includes { brace text }\",\"nested\":{\"count\":9}}\nend")
	decoded, err := decodeAIResponseJSON[aiResponseHelperFixture](payload, "invalid fixture JSON")
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Model != "codex" || decoded.Nested.Count != 9 {
		t.Fatalf("unexpected decoded payload: %+v", decoded)
	}
}

func TestDecodeAIResponseJSONInvalidPayload(t *testing.T) {
	_, err := decodeAIResponseJSON[aiResponseHelperFixture]([]byte("not-json"), "invalid fixture JSON")
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
	if err.Error() != "invalid fixture JSON" {
		t.Fatalf("unexpected invalid JSON error text: %v", err)
	}

	_, err = decodeAIResponseJSON[aiResponseHelperFixture]([]byte("prefix {\"model\":} suffix"), "invalid fixture JSON")
	if err == nil {
		t.Fatalf("expected wrapped invalid JSON error")
	}
	if !strings.Contains(err.Error(), "invalid fixture JSON:") {
		t.Fatalf("expected wrapped parse error propagation, got %v", err)
	}
}
