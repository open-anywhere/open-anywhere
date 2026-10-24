package artifact

import (
	"bytes"
	"github.com/open-anywhere/open-anywhere/gateway/internal/testfixture"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestStoreAndReject(t *testing.T) {
	store := Store{Dir: t.TempDir(), PublicURL: "http://gateway.test"}
	b := testfixture.Artifact(nil)
	product, err := store.Save(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if len(product.Digest) != 64 || product.Name != "test-app" {
		t.Fatal(product)
	}
	if _, err = os.Stat(product.Path); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	store.Handler().ServeHTTP(w, httptest.NewRequest("GET", product.URL, nil))
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), b) {
		t.Fatal(w.Code)
	}
	for _, extra := range []map[string]string{{"../escape": "x"}, {"launch.json": `{"runtimeExecutable":"sh","runtimeArgs":["app/app.js"]}`}, {"launch.json": `{"runtimeExecutable":"node","port":1234,"runtimeArgs":["app/app.js"]}`}, {"openapi.yaml": `{"openapi":"3.1.0","paths":{},"servers":[{"url":"https://evil.test"}]}`}} {
		b = testfixture.Artifact(extra)
		if _, err = store.Save(bytes.NewReader(b), int64(len(b))); err == nil {
			t.Fatal(extra)
		}
	}
	if _, err = store.Save(strings.NewReader("bad"), 4); err == nil {
		t.Fatal("size mismatch accepted")
	}
}

func TestNodeEntryMustBeFirstArgument(t *testing.T) {
	for _, args := range []string{`["-e","process.exit(0)","app/app.js"]`, `["--require","/tmp/other.js","app/app.js"]`, `["/tmp/outside.js","app/app.js"]`, `["app/../app/app.js"]`} {
		b := testfixture.Artifact(map[string]string{"launch.json": `{"name":"bad","runtimeExecutable":"node","runtimeArgs":` + args + `}`})
		if _, err := (Store{Dir: t.TempDir(), PublicURL: "http://gateway.test"}).Save(bytes.NewReader(b), int64(len(b))); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
}
