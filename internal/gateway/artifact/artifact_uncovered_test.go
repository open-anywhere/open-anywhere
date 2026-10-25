package artifact

import (
	"strings"
	"testing"
)

func TestParseLaunchRequiresExactEntryPath(t *testing.T) {
	data := []byte(`{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/dir\\main.js"]}`)
	files := map[string]bool{"app/dir\\main.js": true, "app/app.js": true}
	_, err := parseLaunch(data, files)
	if err == nil || !strings.Contains(err.Error(), "app/app.js") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadEntryRejectsReaderBeyondTheLimit(t *testing.T) {
	data, err := readEntry(strings.NewReader(strings.Repeat("x", 5)), 1, 4)
	if err == nil || data != nil {
		t.Fatalf("readEntry = %q, %v", data, err)
	}
	if !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("err = %v", err)
	}
}
