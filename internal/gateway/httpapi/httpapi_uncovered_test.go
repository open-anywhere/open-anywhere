package httpapi

import "testing"

func TestFromDocumentWithTimeoutClampsTheTimeout(t *testing.T) {
	for name, tc := range map[string]struct {
		timeoutMS int
		want      int
	}{
		"zero":      {timeoutMS: 0, want: 10000},
		"negative":  {timeoutMS: -5, want: 10000},
		"too large": {timeoutMS: 200000, want: 120000},
		"in range":  {timeoutMS: 2500, want: 2500},
	} {
		t.Run(name, func(t *testing.T) {
			controller, err := FromDocumentWithTimeout("app", "http://127.0.0.1:8090", []byte(edgeSpec), tc.timeoutMS)
			if err != nil {
				t.Fatalf("FromDocumentWithTimeout: %v", err)
			}
			entry, ok := controller.entries["app"]
			if !ok {
				t.Fatal("application entry was not compiled")
			}
			if entry.config.TimeoutMS != tc.want {
				t.Fatalf("TimeoutMS = %d, want %d", entry.config.TimeoutMS, tc.want)
			}
		})
	}
}
