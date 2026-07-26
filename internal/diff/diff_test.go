package diff_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moveeeax/einvoice-mandates/internal/build"
	"github.com/moveeeax/einvoice-mandates/internal/diff"
)

// The fixtures are the two example releases shipped in examples/, so the
// documented usage example and the test suite can never drift apart.
const (
	oldRelease = "release-2026.06.json"
	newRelease = "release-2026.07.json"
)

func readExample(t *testing.T, name string) build.Release {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	rel, err := build.Unmarshal(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return rel
}

func compareExamples(t *testing.T) diff.Result {
	t.Helper()
	return diff.Compare(readExample(t, oldRelease), readExample(t, newRelease))
}

func TestComparePicksUpEachChangeShape(t *testing.T) {
	res := compareExamples(t)

	if res.From != "2026.06" || res.To != "2026.07" {
		t.Fatalf("release tags not carried through: %q -> %q", res.From, res.To)
	}

	cases := []struct {
		name string
		want diff.Change
	}{
		{
			name: "a postponed deadline",
			want: diff.Change{
				Kind: diff.PhaseChanged, Significance: diff.Material,
				Country: "MY", Key: "MY.b2b-myinvois.band-1m-5m", Field: "starts_on",
				From: "2026-01-01", To: "2026-07-01",
			},
		},
		{
			name: "a phase that came into force",
			want: diff.Change{
				Kind: diff.PhaseChanged, Significance: diff.Material,
				Country: "PL", Key: "PL.b2b-ksef.large-taxpayers", Field: "status",
				From: "postponed", To: "in_force",
			},
		},
		{
			name: "a mandate that came into force",
			want: diff.Change{
				Kind: diff.MandateChanged, Significance: diff.Material,
				Country: "DE", Key: "DE.b2b-erechnung", Field: "status",
				From: "enacted", To: "in_force",
			},
		},
		{
			name: "a re-verification, which must not be material",
			want: diff.Change{
				Kind: diff.SourceChanged, Significance: diff.Editorial,
				Country: "DE", Key: "DE.b2b-erechnung.source:bmf-erechnung", Field: "verified_on",
				From: "2026-01-12", To: "2026-07-25",
			},
		},
	}

	for _, tc := range cases {
		if !hasChange(res, tc.want) {
			t.Errorf("did not detect %s: %+v", tc.name, tc.want)
		}
	}

	if !hasKind(res, diff.MandateAdded, "ES.verifactu") {
		t.Error("did not detect the newly tracked Spanish Verifactu mandate")
	}
	if !hasKind(res, diff.PhaseAdded, "MY.b2b-myinvois.band-500k-1m") {
		t.Error("did not detect the newly added Malaysian phase")
	}
}

func TestQuietCountriesProduceNoChanges(t *testing.T) {
	// Italy is identical in both releases. Any change reported for it means
	// the differ is inventing noise, which would poison a customer webhook.
	for _, c := range compareExamples(t).Changes {
		if c.Country == "IT" {
			t.Errorf("unexpected change for an unmodified country: %+v", c)
		}
	}
}

func TestCompareIsSymmetricallyEmpty(t *testing.T) {
	rel := readExample(t, newRelease)
	if res := diff.Compare(rel, rel); len(res.Changes) != 0 {
		t.Fatalf("a release compared with itself produced %d change(s): %+v", len(res.Changes), res.Changes)
	}
}

func TestCountsAndFilters(t *testing.T) {
	res := compareExamples(t)
	material, informational, editorial := res.Counts()
	if material+informational+editorial != len(res.Changes) {
		t.Fatalf("counts %d/%d/%d do not add up to %d changes",
			material, informational, editorial, len(res.Changes))
	}
	if material == 0 || editorial == 0 {
		t.Fatalf("fixture should exercise both material and editorial changes, got %d/%d", material, editorial)
	}

	onlyMaterial := res.Apply(diff.Filter{MinSignificance: diff.Material})
	if len(onlyMaterial.Changes) != material {
		t.Errorf("material filter kept %d changes, want %d", len(onlyMaterial.Changes), material)
	}
	for _, c := range onlyMaterial.Changes {
		if c.Significance != diff.Material {
			t.Errorf("material filter leaked a %s change", c.Significance)
		}
	}

	byCountry := res.Apply(diff.Filter{Countries: []string{"pl"}})
	if len(byCountry.Changes) == 0 {
		t.Fatal("country filter should be case-insensitive and match PL")
	}
	for _, c := range byCountry.Changes {
		if c.Country != "PL" {
			t.Errorf("country filter leaked %s", c.Country)
		}
	}

	if none := res.Apply(diff.Filter{Countries: []string{"ZZ"}}); len(none.Changes) != 0 {
		t.Errorf("filtering on an absent country should yield nothing, got %d", len(none.Changes))
	}
}

func TestChangesAreOrderedDeterministically(t *testing.T) {
	first := compareExamples(t)
	second := compareExamples(t)
	if len(first.Changes) != len(second.Changes) {
		t.Fatalf("non-deterministic change count: %d vs %d", len(first.Changes), len(second.Changes))
	}
	for i := range first.Changes {
		if first.Changes[i] != second.Changes[i] {
			t.Fatalf("change %d differs between runs:\n%+v\n%+v", i, first.Changes[i], second.Changes[i])
		}
		if i > 0 && first.Changes[i-1].Country > first.Changes[i].Country {
			t.Fatalf("changes are not grouped by country: %s before %s",
				first.Changes[i-1].Country, first.Changes[i].Country)
		}
	}
}

func TestRenderText(t *testing.T) {
	var sb strings.Builder
	res := compareExamples(t)
	if err := diff.RenderText(&sb, res); err != nil {
		t.Fatal(err)
	}
	out := sb.String()

	for _, want := range []string{
		"e-invoicing mandate changes: 2026.06 -> 2026.07",
		"postponed 2026-01-01 -> 2026-07-01",
		"status enacted -> in_force",
		"material",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output is missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "IT (") {
		t.Error("unchanged Italy should not get a section in the report")
	}
}

func TestRenderTextOnEmptyDiff(t *testing.T) {
	var sb strings.Builder
	rel := readExample(t, newRelease)
	if err := diff.RenderText(&sb, diff.Compare(rel, rel)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "No changes.") {
		t.Errorf("expected an explicit no-changes line, got:\n%s", sb.String())
	}
}

func hasChange(r diff.Result, want diff.Change) bool {
	for _, c := range r.Changes {
		if c.Kind == want.Kind && c.Significance == want.Significance && c.Country == want.Country &&
			c.Key == want.Key && c.Field == want.Field && c.From == want.From && c.To == want.To {
			return true
		}
	}
	return false
}

func hasKind(r diff.Result, kind diff.Kind, key string) bool {
	for _, c := range r.Changes {
		if c.Kind == kind && c.Key == key {
			return true
		}
	}
	return false
}
