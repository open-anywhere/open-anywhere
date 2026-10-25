package artifact

import (
	"strings"
	"testing"
)

func TestParseLaunchRejectsEntryThatCleansToAnotherName(t *testing.T) {
	// A backslash is a valid file-name character, so the archive can contain
	// "app/dir\main.js"; cleanName normalizes it to app/dir/main.js and the
	// runtime argument no longer resolves to an archived entry.
	data := []byte(`{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/dir\\main.js"]}`)
	files := map[string]bool{"app/dir\\main.js": true}
	_, err := parseLaunch(data, files)
	if err == nil || !strings.Contains(err.Error(), "must reference an entry file inside app/") {
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
