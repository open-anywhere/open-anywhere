// Package artifact validates and stores application packages produced by
// AgentAPI and served to Render.
package artifact

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/open-anywhere/open-anywhere/internal/gateway/openapi"
)

const (
	// MaxSize is the compressed artifact limit from the artifact contract.
	MaxSize int64 = 100 << 20
	// MaxReadyTimeoutMS is the upper bound accepted for launch.json.
	MaxReadyTimeoutMS = 60_000

	// maxArtifactSize is the expanded artifact limit. It matches Render's
	// maxArtifactBytes so an artifact accepted here cannot fail there.
	maxArtifactSize int64 = 100 << 20
	// maxFileSize is the per-file limit. Render does not impose a tighter
	// per-file bound, so keep it equal to the expanded total.
	maxFileSize = maxArtifactSize
	maxTotal    = maxArtifactSize
	// maxEntries matches Render's archive entry cap.
	maxEntries = 4096
)

// requiredFiles must exist as regular files for the archive to be runnable.
var requiredFiles = []string{
	"launch.json",
	"openapi.yaml",
	"app/app.js",
	"app/package.json",
	"app/public/index.html",
}

// Error is a validation failure with a stable code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func invalid(format string, args ...any) error {
	return &Error{Code: "INVALID_ARTIFACT", Message: fmt.Sprintf(format, args...)}
}

// IsCode reports whether err is a validation error with code.
func IsCode(err error, code string) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}

// Launch is the validated launch.json contract.
type Launch struct {
	Name              string
	RuntimeExecutable string
	RuntimeArgs       []string
	HealthPath        string
	ReadyTimeoutMS    int
}

// Result is the outcome of validating one artifact.
type Result struct {
	Launch  Launch
	OpenAPI *openapi.Spec
	Size    int64
	SHA256  string
}

type launchFile struct {
	Name              string          `json:"name"`
	RuntimeExecutable string          `json:"runtimeExecutable"`
	RuntimeArgs       []string        `json:"runtimeArgs"`
	HealthPath        string          `json:"healthPath"`
	ReadyTimeoutMS    *int            `json:"readyTimeoutMs"`
	Port              json.RawMessage `json:"port"`
}

// Validate checks the archive at filename and returns its parsed contract.
func Validate(filename string) (Result, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return Result{}, err
	}
	if info.Size() > MaxSize {
		return Result{}, &Error{Code: "ARTIFACT_TOO_LARGE", Message: fmt.Sprintf("artifact is %d bytes; limit is %d", info.Size(), MaxSize)}
	}
	digest, err := fileSHA256(filename)
	if err != nil {
		return Result{}, err
	}
	result, err := validateArchive(filename)
	if err != nil {
		return Result{}, err
	}
	result.Size = info.Size()
	result.SHA256 = digest
	return result, nil
}

func validateArchive(filename string) (Result, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Result{}, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return Result{}, &Error{Code: "ARTIFACT_CORRUPT", Message: "not a gzip archive: " + err.Error()}
	}
	defer gz.Close()
	expanded := &io.LimitedReader{R: gz, N: maxTotal + 1}
	reader := tar.NewReader(expanded)

	files := make(map[string]bool)
	var (
		launchData  []byte
		openapiData []byte
		packageJSON []byte
		total       int64
		entryCount  int
	)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Result{}, &Error{Code: "ARTIFACT_CORRUPT", Message: "cannot read tar archive: " + err.Error()}
		}
		entryCount++
		if entryCount > maxEntries {
			return Result{}, invalid("archive has more than %d entries", maxEntries)
		}
		name, err := cleanName(header.Name)
		if err != nil {
			return Result{}, err
		}
		if name == "" {
			continue
		}
		switch header.Typeflag {
		case tar.TypeReg:
		case tar.TypeDir:
			continue
		default:
			return Result{}, invalid("archive entry %q is not a regular file or directory", header.Name)
		}
		if strings.HasPrefix(name, "outputs/") || name == "outputs" {
			return Result{}, invalid("archive must not contain an outputs/ directory")
		}
		total += header.Size
		if header.Size < 0 || header.Size > maxFileSize || total > maxTotal {
			return Result{}, &Error{Code: "ARTIFACT_TOO_LARGE", Message: "archive contents exceed the size limit"}
		}
		if files[name] {
			return Result{}, invalid("duplicate archive path %q", name)
		}
		files[name] = true
		switch name {
		case "launch.json":
			launchData, err = readEntry(reader, header.Size, 1<<20)
		case "openapi.yaml":
			openapiData, err = readEntry(reader, header.Size, 8<<20)
		case "app/package.json":
			packageJSON, err = readEntry(reader, header.Size, 1<<20)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if _, err := io.Copy(io.Discard, expanded); err != nil {
		return Result{}, invalid("gzip integrity check failed")
	}
	if expanded.N <= 0 {
		return Result{}, invalid("expanded archive too large")
	}
	for _, name := range requiredFiles {
		if !files[name] {
			return Result{}, invalid("required file %q is missing", name)
		}
	}
	if packageJSON == nil {
		return Result{}, invalid("required file %q is missing", "app/package.json")
	}
	if _, err := decodePackageJSON(packageJSON); err != nil {
		return Result{}, err
	}
	launch, err := parseLaunch(launchData, files)
	if err != nil {
		return Result{}, err
	}
	spec, err := openapi.Parse(openapiData)
	if err != nil {
		return Result{}, invalid("%s", err)
	}
	return Result{Launch: launch, OpenAPI: spec}, nil
}

// decodePackageJSON requires the same shape Render accepts: a JSON object.
func decodePackageJSON(data []byte) (map[string]any, error) {
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil || metadata == nil {
		return nil, invalid("app/package.json is not a valid JSON object")
	}
	return metadata, nil
}

func parseLaunch(data []byte, files map[string]bool) (Launch, error) {
	var value launchFile
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Launch{}, invalid("launch.json is invalid: %v", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Launch{}, err
	}
	if len(value.Port) != 0 {
		return Launch{}, invalid("launch.json must not contain a port field")
	}
	name := strings.TrimSpace(value.Name)
	if name == "" {
		return Launch{}, invalid("launch.json name is required")
	}
	if value.RuntimeExecutable != "node" {
		return Launch{}, invalid("runtimeExecutable must be node")
	}
	if len(value.RuntimeArgs) == 0 {
		return Launch{}, invalid("runtimeArgs is required")
	}
	if value.RuntimeArgs[0] != "app/app.js" {
		return Launch{}, invalid(`runtimeArgs[0] must be exactly "app/app.js", got %q`, value.RuntimeArgs[0])
	}
	if !files["app/app.js"] {
		return Launch{}, invalid(`runtimeArgs[0] must reference the archived file "app/app.js"`)
	}
	for _, arg := range value.RuntimeArgs[1:] {
		if arg == "--port" || strings.HasPrefix(arg, "--port=") {
			return Launch{}, invalid("runtimeArgs must not fix a port")
		}
	}
	healthPath := value.HealthPath
	if healthPath == "" {
		healthPath = "/health"
	}
	if healthPath != "/health" {
		return Launch{}, invalid("healthPath must be /health in this version, got %q", healthPath)
	}
	ready := 10000
	if value.ReadyTimeoutMS != nil {
		ready = *value.ReadyTimeoutMS
		if ready <= 0 || ready > MaxReadyTimeoutMS {
			return Launch{}, invalid("readyTimeoutMs must be between 1 and %d", MaxReadyTimeoutMS)
		}
	}
	return Launch{
		Name:              name,
		RuntimeExecutable: value.RuntimeExecutable,
		RuntimeArgs:       append([]string(nil), value.RuntimeArgs...),
		HealthPath:        healthPath,
		ReadyTimeoutMS:    ready,
	}, nil
}

func cleanName(raw string) (string, error) {
	name := strings.ReplaceAll(raw, "\\", "/")
	if strings.HasPrefix(name, "/") {
		return "", invalid("archive entry %q is absolute", raw)
	}
	for segment := range strings.SplitSeq(name, "/") {
		if segment == ".." {
			return "", invalid("archive entry %q escapes the archive root", raw)
		}
	}
	name = strings.TrimPrefix(name, "./")
	cleaned := path.Clean(name)
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", invalid("archive entry %q escapes the archive root", raw)
	}
	return cleaned, nil
}

func readEntry(reader io.Reader, size, limit int64) ([]byte, error) {
	if size > limit {
		return nil, invalid("archive entry exceeds %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, &Error{Code: "ARTIFACT_CORRUPT", Message: err.Error()}
	}
	if int64(len(data)) > limit {
		return nil, invalid("archive entry exceeds %d bytes", limit)
	}
	return data, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return invalid("launch.json must contain exactly one JSON object")
	}
	return nil
}

func fileSHA256(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
