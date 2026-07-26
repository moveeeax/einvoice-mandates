package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	dataDir     = "../../data/countries"
	exampleOld  = "../../examples/release-2026.06.json"
	exampleNew  = "../../examples/release-2026.07.json"
	exampleTail = "release-2026.07.json"
)

// exec runs the CLI in-process and returns stdout, stderr and the exit status.
func exec(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)

	code := 0
	if err != nil {
		var ec exitCode
		if errors.As(err, &ec) {
			code = int(ec)
		} else {
			code = 1
			stderr.WriteString("error: " + err.Error() + "\n")
		}
	}
	return stdout.String(), stderr.String(), code
}

func TestValidateShippedDataset(t *testing.T) {
	stdout, stderr, code := exec(t, "validate", "-data", dataDir, "-strict")
	if code != 0 {
		t.Fatalf("validate -strict failed (exit %d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "0 error(s), 0 warning(s)") {
		t.Errorf("unexpected summary line:\n%s", stdout)
	}
}

func TestValidateJSONOutput(t *testing.T) {
	stdout, _, code := exec(t, "validate", "-data", dataDir, "-json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var report struct {
		Findings []struct {
			Severity string `json:"severity"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(report.Findings) != 0 {
		t.Errorf("expected no findings, got %d", len(report.Findings))
	}
}

func TestBuildRenderDiffPipeline(t *testing.T) {
	dir := t.TempDir()
	artefact := filepath.Join(dir, "release.json")

	// build
	if _, stderr, code := exec(t, "build", "-data", dataDir, "-release", "test", "-allow-draft", "-o", artefact); code != 0 {
		t.Fatalf("build failed (exit %d): %s", code, stderr)
	}
	raw, err := os.ReadFile(artefact)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatal("build produced invalid JSON")
	}

	// render
	page := filepath.Join(dir, "index.html")
	if _, stderr, code := exec(t, "render", artefact, "-o", page); code != 0 {
		t.Fatalf("render failed (exit %d): %s", code, stderr)
	}
	html, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(html, []byte("<table")) {
		t.Fatal("rendered page has no table")
	}

	// diff against itself: no changes, exit 0 even with -exit-code
	stdout, _, code := exec(t, "diff", artefact, artefact, "-exit-code")
	if code != 0 {
		t.Errorf("self-diff should exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "No changes.") {
		t.Errorf("self-diff should report no changes:\n%s", stdout)
	}
}

func TestBuildRefusesDraftWithoutOverride(t *testing.T) {
	_, stderr, code := exec(t, "build", "-data", dataDir, "-release", "test")
	if code == 0 {
		t.Fatal("expected a non-zero exit when publishing draft countries")
	}
	if !strings.Contains(stderr, "allow-draft") {
		t.Errorf("stderr should explain the override:\n%s", stderr)
	}
}

func TestDiffExitCodeAndFlagPermutation(t *testing.T) {
	// Flags after the positional arguments must still be honoured.
	stdout, _, code := exec(t, "diff", exampleOld, exampleNew, "-min", "material", "-exit-code")
	if code != 1 {
		t.Fatalf("expected exit 1 when material changes exist, got %d", code)
	}
	if !strings.Contains(stdout, "0 editorial") {
		t.Errorf("-min material should have suppressed every editorial change:\n%s", stdout)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- editorial") {
			t.Errorf("-min material leaked an editorial change: %s", line)
		}
	}

	// Restricting to a country with no changes must exit 0 again.
	if _, _, code := exec(t, "diff", exampleOld, exampleNew, "-country", "IT", "-exit-code"); code != 0 {
		t.Errorf("expected exit 0 for an unchanged country, got %d", code)
	}
}

func TestDiffJSONPayloadShape(t *testing.T) {
	stdout, _, code := exec(t, "diff", exampleOld, exampleNew, "-json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var payload struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Changes []struct {
			Kind         string `json:"kind"`
			Significance string `json:"significance"`
			Country      string `json:"country"`
			Key          string `json:"key"`
			Summary      string `json:"summary"`
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if payload.From != "2026.06" || payload.To != "2026.07" {
		t.Errorf("unexpected release tags: %q -> %q", payload.From, payload.To)
	}
	if len(payload.Changes) == 0 {
		t.Fatal("expected changes between the two example releases")
	}
	for i, c := range payload.Changes {
		if c.Kind == "" || c.Significance == "" || c.Country == "" || c.Key == "" || c.Summary == "" {
			t.Errorf("change %d has an empty required field: %+v", i, c)
		}
	}
}

func TestUsageAndErrorPaths(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no arguments", nil, 2, "Usage:"},
		{"unknown command", []string{"frobnicate"}, 2, "unknown command"},
		{"diff with one file", []string{"diff", exampleNew}, 2, "exactly two"},
		{"render with no file", []string{"render"}, 2, "exactly one"},
		{"build without release", []string{"build", "-data", dataDir}, 2, "-release is required"},
		{"missing data dir", []string{"validate", "-data", "does/not/exist"}, 1, "read data dir"},
		{"bad significance", []string{"diff", exampleOld, exampleNew, "-min", "urgent"}, 1, "invalid -min"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := exec(t, tc.args...)
			if code != tc.code {
				t.Errorf("exit = %d, want %d (stderr: %s)", code, tc.code, stderr)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr should mention %q, got:\n%s", tc.want, stderr)
			}
		})
	}
}

func TestVersionCommand(t *testing.T) {
	stdout, _, code := exec(t, "version")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "einvoice-mandates") || !strings.Contains(stdout, "schema v1") {
		t.Errorf("unexpected version output: %q", stdout)
	}
}

func TestExamplesAreRebuildableFromSource(t *testing.T) {
	// The committed example release must stay in step with data/countries,
	// otherwise the README example documents a dataset that no longer exists.
	dir := t.TempDir()
	fresh := filepath.Join(dir, exampleTail)
	if _, stderr, code := exec(t, "build", "-data", dataDir, "-release", "2026.07", "-allow-draft", "-o", fresh); code != 0 {
		t.Fatalf("build failed: %s", stderr)
	}

	got := mustParse(t, fresh)
	want := mustParse(t, exampleNew)
	// generated_on is a timestamp, so compare everything else.
	delete(got, "generated_on")
	delete(want, "generated_on")

	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Error("examples/" + exampleTail + " is stale; regenerate it with `make examples`")
	}
}

func mustParse(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
