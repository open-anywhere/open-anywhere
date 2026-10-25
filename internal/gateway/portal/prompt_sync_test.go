package portal

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestEmbeddedPromptMatchesDocs keeps the embedded host prompt and its
// documented source from drifting apart. The docs file is the source of truth:
// edit it, then copy it to internal/gateway/portal/agent-prompt.txt.
func TestEmbeddedPromptMatchesDocs(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test file")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "agent-system-prompts", "open-anywhere-host.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read docs prompt %s: %v", path, err)
	}
	want := string(data)
	if defaultSystemPrompt == want {
		return
	}
	t.Fatalf("embedded agent-prompt.txt differs from %s:\n%s", path, promptDiff(want, defaultSystemPrompt))
}

func promptDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	for i := 0; i < len(wantLines) && i < len(gotLines); i++ {
		if wantLines[i] != gotLines[i] {
			return fmt.Sprintf("first difference at line %d:\n  docs:     %q\n  embedded: %q\n(docs has %d lines, embedded has %d)",
				i+1, wantLines[i], gotLines[i], len(wantLines), len(gotLines))
		}
	}
	return fmt.Sprintf("content matches for %d lines but lengths differ: docs has %d lines, embedded has %d",
		min(len(wantLines), len(gotLines)), len(wantLines), len(gotLines))
}
