package testfixture

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
)

func Artifact(extra map[string]string) []byte {
	files := map[string]string{"app/app.js": "console.log('test')", "app/public/index.html": "<!doctype html><title>test</title>", "launch.json": `{"name":"test-app","runtimeExecutable":"node","runtimeArgs":["app/app.js"]}`, "openapi.yaml": `{"openapi":"3.1.0","paths":{"/api/state":{"get":{"operationId":"readState","responses":{"200":{"description":"ok"}}}}}}`}
	for k, v := range extra {
		files[k] = v
	}
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for name, value := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(value)), Mode: 0600})
		_, _ = tw.Write([]byte(value))
	}
	_ = tw.Close()
	_ = gz.Close()
	return b.Bytes()
}
