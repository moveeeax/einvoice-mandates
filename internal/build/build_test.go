package build_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moveeeax/einvoice-mandates/internal/build"
	"github.com/moveeeax/einvoice-mandates/internal/dataset"
	"github.com/moveeeax/einvoice-mandates/internal/model"
)

var stamp = time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

func loadReal(t *testing.T) []dataset.File {
	t.Helper()
	files, err := dataset.Load(filepath.Join("..", "..", "data", "countries"))
	if err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	return files
}

func TestBuildRefusesDraftCountriesByDefault(t *testing.T) {
	// Every seed country is still review_status: draft, so an unguarded build
	// must refuse rather than publish unverified data.
	_, err := build.Build(loadReal(t), build.Options{Release: "test", GeneratedOn: stamp})
	if err == nil {
		t.Fatal("expected build to refuse a dataset of draft countries")
	}
	if !strings.Contains(err.Error(), "--allow-draft") {
		t.Errorf("error should tell the operator how to override, got: %v", err)
	}
}

func TestBuildCountsAndOrdering(t *testing.T) {
	files := loadReal(t)
	rel, err := build.Build(files, build.Options{Release: "test", GeneratedOn: stamp, AllowDraft: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if rel.SchemaVersion != model.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", rel.SchemaVersion, model.SchemaVersion)
	}
	if rel.Counts.Countries != len(files) {
		t.Errorf("counted %d countries, loaded %d files", rel.Counts.Countries, len(files))
	}
	if rel.Counts.Draft != len(files) {
		t.Errorf("draft_countries = %d, want %d", rel.Counts.Draft, len(files))
	}

	var mandates, phases, sources int
	for _, c := range rel.Countries {
		for _, m := range c.Mandates {
			mandates++
			phases += len(m.Phases)
			sources += len(m.Sources)
		}
	}
	if rel.Counts.Mandates != mandates || rel.Counts.Phases != phases || rel.Counts.Sources != sources {
		t.Errorf("counts %+v disagree with the tree (%d mandates, %d phases, %d sources)",
			rel.Counts, mandates, phases, sources)
	}

	for i := 1; i < len(rel.Countries); i++ {
		if rel.Countries[i-1].Code >= rel.Countries[i].Code {
			t.Fatalf("countries not sorted: %q before %q", rel.Countries[i-1].Code, rel.Countries[i].Code)
		}
	}
	for _, c := range rel.Countries {
		for i := 1; i < len(c.Mandates); i++ {
			if c.Mandates[i-1].ID >= c.Mandates[i].ID {
				t.Errorf("%s: mandates not sorted: %q before %q", c.Code, c.Mandates[i-1].ID, c.Mandates[i].ID)
			}
		}
	}
}

func TestBuildIsReproducible(t *testing.T) {
	// A customer diffing two releases must never see churn caused by the
	// builder itself, so identical inputs have to produce identical bytes.
	opts := build.Options{Release: "test", GeneratedOn: stamp, AllowDraft: true}

	first, err := build.Build(loadReal(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := build.Build(loadReal(t), opts)
	if err != nil {
		t.Fatal(err)
	}

	a, err := build.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := build.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two builds of the same input produced different bytes")
	}
	if !bytes.HasSuffix(a, []byte("\n")) {
		t.Error("artefact should end with a newline")
	}
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	rel, err := build.Build(loadReal(t), build.Options{Release: "2026.07", GeneratedOn: stamp, AllowDraft: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := build.Marshal(rel)
	if err != nil {
		t.Fatal(err)
	}
	back, err := build.Unmarshal(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	again, err := build.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatal("round trip through JSON is lossy")
	}
	if back.Release != "2026.07" || back.GeneratedOn != "2026-07-25T00:00:00Z" {
		t.Errorf("release metadata lost: %q %q", back.Release, back.GeneratedOn)
	}
}

func TestUnmarshalRejectsForeignSchemaVersion(t *testing.T) {
	_, err := build.Unmarshal([]byte(`{"schema_version": 99, "release": "v99", "countries": []}`))
	if err == nil {
		t.Fatal("expected a schema version mismatch error")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("error should name the unsupported version, got: %v", err)
	}
}

func TestBuildRequiresReleaseTag(t *testing.T) {
	if _, err := build.Build(loadReal(t), build.Options{AllowDraft: true}); err == nil {
		t.Fatal("expected an error when the release tag is missing")
	}
}
