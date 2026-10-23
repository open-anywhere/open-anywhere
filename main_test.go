package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	var buf bytes.Buffer

	run(&buf)

	if got, want := strings.TrimSpace(buf.String()), "hello world"; got != want {
		t.Errorf("run() 输出 = %q, want %q", got, want)
	}
}
