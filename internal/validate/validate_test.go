package validate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moveeeax/einvoice-mandates/internal/dataset"
	"github.com/moveeeax/einvoice-mandates/internal/validate"
)

// refToday pins "now" so that in_force/planned checks and staleness warnings
// do not start failing as the calendar moves.
var refToday = time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)

func load(t *testing.T, dir string) []dataset.File {
	t.Helper()
	files, err := dataset.Load(dir)
	if err != nil {
		t.Fatalf("load %s: %v", dir, err)
	}
	return files
}

func runLint(t *testing.T, dir string) validate.Report {
	t.Helper()
	return validate.Run(load(t, dir), validate.Options{Today: refToday})
}

func TestGoodFixtureIsClean(t *testing.T) {
	report := runLint(t, "testdata/good")
	if report.Errors() != 0 || report.Warnings() != 0 {
		for _, f := range report.Findings {
			t.Logf("unexpected: %s", f)
		}
		t.Fatalf("good fixture should lint clean, got %d error(s) and %d warning(s)",
			report.Errors(), report.Warnings())
	}
}

func TestRealDatasetLintsClean(t *testing.T) {
	// The shipped dataset is part of the product, so it is part of the suite.
	report := validate.Run(load(t, filepath.Join("..", "..", "data", "countries")), validate.Options{})
	if report.Errors() != 0 {
		for _, f := range report.Findings {
			if f.Severity == validate.Error {
				t.Errorf("%s", f)
			}
		}
		t.Fatalf("data/countries has %d lint error(s)", report.Errors())
	}
}

func TestBadFixtureFindings(t *testing.T) {
	report := runLint(t, "testdata/bad")

	// Each entry is a field path plus a distinctive fragment of the message.
	want := []struct{ path, fragment string }{
		{"country", "ISO 3166-1 alpha-2"},
		{"tax_authority", "is required"},
		{"region", "not a valid region"},
		{"review_status", "not a valid review_status"},
		{"last_reviewed", "is in the future"},
		{"mandates[0].id", "lower-kebab-case"},
		{"mandates[0].title", "is required"},
		{"mandates[0].scope", "not a valid scope"},
		{"mandates[0].model", "not a valid model"},
		{"mandates[0].formats", "at least one format"},
		{"mandates[0].sources[0].url", "absolute https URL"},
		{"mandates[0].sources[0].language", "ISO 639-1"},
		{"mandates[0].phases[0].status", "marked in_force but starts_on"},
		{"mandates[0].phases[0].threshold.operator", "not a valid operator"},
		{"mandates[0].phases[0].threshold.value", "must not be negative"},
		{"mandates[0].phases[0].threshold.currency", "ISO 4217"},
		{"mandates[0].phases[0].threshold.value_max", "only meaningful when operator is between"},
		{"mandates[0].phases[1].starts_on", "chronological order"},
		{"mandates[0].phases[1].obligation", "not a valid obligation"},
		{"mandates[0].phases[1].applies_to", "is required"},
		{"mandates[0].phases[1].status", "awaiting_trigger is only valid"},
		{"mandates[0].phases[2].starts_on", "or set starts_on_rule"},
		{"mandates[0].phases[3].starts_on_rule", "must not be set together"},
		{"mandates[0].phases[3].source_ref", "does not match any source id"},
		{"mandates[0].phases[4].id", "duplicate phase id"},
		{"mandates[0].phases[4].previous_starts_on", "not earlier than starts_on"},
	}

	for _, w := range want {
		if !hasFinding(report, validate.Error, w.path, w.fragment) {
			t.Errorf("missing error at %s containing %q", w.path, w.fragment)
		}
	}

	// The stale citation and the missing official flag are warnings, not errors:
	// they make the entry suspect, not wrong.
	if !hasFinding(report, validate.Warning, "mandates[0].sources[0].verified_on", "older than the freshness promise") {
		t.Error("expected a staleness warning on the 2019 citation")
	}
	if !hasFinding(report, validate.Warning, "mandates[0].sources", "no source is marked official") {
		t.Error("expected a warning about the missing official source")
	}
}

func TestDuplicateCountryCodeAcrossFiles(t *testing.T) {
	report := runLint(t, "testdata/dupes")
	if !hasFinding(report, validate.Error, "country", "duplicate country code") {
		t.Fatalf("expected a duplicate country code error, got %d finding(s)", len(report.Findings))
	}
}

func TestStalenessIsRelativeToToday(t *testing.T) {
	files := load(t, "testdata/good")

	fresh := validate.Run(files, validate.Options{Today: refToday})
	if fresh.Warnings() != 0 {
		t.Fatalf("expected no warnings at the reference date, got %d", fresh.Warnings())
	}

	// Two years on, the same file must start nagging about its citations.
	later := validate.Run(files, validate.Options{Today: refToday.AddDate(2, 0, 0)})
	if !hasFinding(later, validate.Warning, "last_reviewed", "not reviewed since") {
		t.Error("expected a country-level staleness warning two years later")
	}
	if !hasFinding(later, validate.Warning, "mandates[0].sources[0].verified_on", "older than the freshness promise") {
		t.Error("expected a source-level staleness warning two years later")
	}
}

func TestFindingsAreSortedAndStable(t *testing.T) {
	files := load(t, "testdata/bad")
	first := validate.Run(files, validate.Options{Today: refToday})
	second := validate.Run(files, validate.Options{Today: refToday})

	if len(first.Findings) != len(second.Findings) {
		t.Fatalf("lint is not deterministic: %d vs %d findings", len(first.Findings), len(second.Findings))
	}
	for i := range first.Findings {
		if first.Findings[i] != second.Findings[i] {
			t.Fatalf("finding %d differs between runs:\n %s\n %s", i, first.Findings[i], second.Findings[i])
		}
		if i > 0 && first.Findings[i-1].Path > first.Findings[i].Path {
			t.Errorf("findings are not sorted by path: %q before %q",
				first.Findings[i-1].Path, first.Findings[i].Path)
		}
	}
}

func TestUnknownFieldIsRejected(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile("testdata/good/xx.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// A typo in a key must fail loudly rather than being silently dropped.
	broken := strings.Replace(string(src), "review_status: verified", "reviewstatus: verified", 1)
	if err := os.WriteFile(filepath.Join(dir, "xx.yaml"), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := dataset.Load(dir); err == nil {
		t.Fatal("expected an error for an unknown YAML field")
	} else if !strings.Contains(err.Error(), "reviewstatus") {
		t.Fatalf("error should name the offending field, got: %v", err)
	}
}

func hasFinding(r validate.Report, sev validate.Severity, path, fragment string) bool {
	for _, f := range r.Findings {
		if f.Severity == sev && f.Path == path && strings.Contains(f.Message, fragment) {
			return true
		}
	}
	return false
}
