package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moveeeax/einvoice-mandates/internal/build"
	"github.com/moveeeax/einvoice-mandates/internal/render"
)

func example(t *testing.T) build.Release {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "release-2026.07.json"))
	if err != nil {
		t.Fatal(err)
	}
	rel, err := build.Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestFlattenProducesOneRowPerPhase(t *testing.T) {
	rel := example(t)
	rows := render.Flatten(rel)
	if len(rows) != rel.Counts.Phases {
		t.Fatalf("flattened %d rows, release declares %d phases", len(rows), rel.Counts.Phases)
	}

	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.Anchor] {
			t.Errorf("duplicate permalink anchor %q", r.Anchor)
		}
		seen[r.Anchor] = true
		if r.StartsOn == "" {
			t.Errorf("%s has an empty date column; undated phases must fall back to their trigger rule", r.Anchor)
		}
		if r.SourceURL == "" {
			t.Errorf("%s has no source link", r.Anchor)
		}
	}
}

func TestUndatedPhaseShowsItsTriggerRule(t *testing.T) {
	// Spain's Crea y Crece phases have no date yet, only a rule.
	for _, r := range render.Flatten(example(t)) {
		if r.Anchor == "ES-b2b-crea-y-crece-issue-large" {
			if !strings.Contains(r.StartsOn, "implementing regulation") {
				t.Fatalf("expected the trigger rule in the date column, got %q", r.StartsOn)
			}
			if r.PhaseStatus != "awaiting_trigger" {
				t.Fatalf("phase status = %q, want awaiting_trigger", r.PhaseStatus)
			}
			return
		}
	}
	t.Fatal("fixture no longer contains the undated Spanish phase")
}

func TestHTMLIsSelfContained(t *testing.T) {
	rel := example(t)

	var sb strings.Builder
	if err := render.HTML(&sb, rel); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	if !strings.HasPrefix(out, "<!DOCTYPE html>") {
		t.Error("output should be a complete HTML document")
	}
	if n := strings.Count(out, "<tr id="); n != rel.Counts.Phases {
		t.Errorf("rendered %d table rows, want %d", n, rel.Counts.Phases)
	}
	if !strings.Contains(out, rel.Release) {
		t.Error("page should state which release it was built from")
	}

	// GitHub Pages serves this as a single file: no external assets allowed.
	for _, forbidden := range []string{"<script src=", "<link rel=\"stylesheet\"", "@import"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("page pulls in an external asset via %q", forbidden)
		}
	}
}

func TestHTMLEscapesUntrustedText(t *testing.T) {
	rel := example(t)
	rel.Countries[0].Mandates[0].Title = `Nasty <script>alert(1)</script> title`

	var sb strings.Builder
	if err := render.HTML(&sb, rel); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sb.String(), "<script>alert(1)</script>") {
		t.Fatal("mandate text was not HTML-escaped")
	}
}
