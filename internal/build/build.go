// Package build turns the YAML source tree into the published JSON artefact.
//
// The artefact is the product: it must be byte-for-byte reproducible from a
// given commit so that a customer diffing two releases sees only real changes.
package build

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/moveeeax/einvoice-mandates/internal/dataset"
	"github.com/moveeeax/einvoice-mandates/internal/model"
)

// Release is the top-level published document.
type Release struct {
	SchemaVersion int             `json:"schema_version"`
	Release       string          `json:"release"`
	GeneratedOn   string          `json:"generated_on"`
	Counts        Counts          `json:"counts"`
	Countries     []model.Country `json:"countries"`
}

// Counts is the at-a-glance summary printed by the CLI and rendered on the
// static page.
type Counts struct {
	Countries int `json:"countries"`
	Mandates  int `json:"mandates"`
	Phases    int `json:"phases"`
	Sources   int `json:"sources"`
	Draft     int `json:"draft_countries"`
}

// Options controls artefact generation.
type Options struct {
	// Release is the version tag of this artefact, e.g. "v4".
	Release string
	// GeneratedOn stamps the artefact. Zero means time.Now in UTC.
	GeneratedOn time.Time
	// AllowDraft publishes countries whose review_status is still draft.
	AllowDraft bool
}

// Build assembles a Release from loaded files. Countries and their nested
// collections are ordered deterministically; the YAML author's phase order is
// preserved because it is chronological and meaningful.
func Build(files []dataset.File, opts Options) (Release, error) {
	if opts.Release == "" {
		return Release{}, fmt.Errorf("release tag is required")
	}
	if opts.GeneratedOn.IsZero() {
		opts.GeneratedOn = time.Now().UTC()
	}

	rel := Release{
		SchemaVersion: model.SchemaVersion,
		Release:       opts.Release,
		GeneratedOn:   opts.GeneratedOn.UTC().Format(time.RFC3339),
	}

	var skipped []string
	for _, f := range files {
		c := f.Country
		if c.ReviewStatus == model.ReviewDraft {
			rel.Counts.Draft++
			if !opts.AllowDraft {
				skipped = append(skipped, c.Code)
				continue
			}
		}
		sort.SliceStable(c.Mandates, func(i, j int) bool { return c.Mandates[i].ID < c.Mandates[j].ID })
		for _, m := range c.Mandates {
			rel.Counts.Mandates++
			rel.Counts.Phases += len(m.Phases)
			rel.Counts.Sources += len(m.Sources)
		}
		rel.Countries = append(rel.Countries, c)
	}

	if len(skipped) > 0 {
		return Release{}, fmt.Errorf("refusing to publish: %d country file(s) still review_status=draft (%v); verify them or pass --allow-draft", len(skipped), skipped)
	}
	if len(rel.Countries) == 0 {
		return Release{}, fmt.Errorf("no countries to publish")
	}
	rel.Counts.Countries = len(rel.Countries)
	return rel, nil
}

// Marshal renders the release as indented JSON with a trailing newline.
func Marshal(rel Release) ([]byte, error) {
	b, err := json.MarshalIndent(rel, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Unmarshal parses a previously published artefact.
func Unmarshal(b []byte) (Release, error) {
	var rel Release
	if err := json.Unmarshal(b, &rel); err != nil {
		return rel, fmt.Errorf("parse release artefact: %w", err)
	}
	if rel.SchemaVersion != model.SchemaVersion {
		return rel, fmt.Errorf("artefact schema_version %d is not supported by this build (%d)", rel.SchemaVersion, model.SchemaVersion)
	}
	return rel, nil
}
