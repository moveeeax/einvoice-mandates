// Package render produces the free public HTML table from a release artefact.
// The page is a single self-contained file with no external assets, so it can
// be served from GitHub Pages or opened from disk.
package render

import (
	"embed"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/moveeeax/einvoice-mandates/internal/build"
	"github.com/moveeeax/einvoice-mandates/internal/model"
)

//go:embed templates/*.gohtml
var templates embed.FS

// Row is one flattened phase, which is what the table lists.
type Row struct {
	Country      string
	CountryName  string
	Region       string
	MandateID    string
	MandateTitle string
	Scope        string
	Model        string
	Status       string
	Network      string
	Formats      string
	PhaseID      string
	StartsOn     string
	Obligation   string
	AppliesTo    string
	Threshold    string
	PhaseStatus  string
	Postponed    bool
	SourceURL    string
	VerifiedOn   string
	Anchor       string
}

type page struct {
	Release build.Release
	Rows    []Row
	Regions []string
}

// Flatten turns the nested release into one row per phase.
func Flatten(rel build.Release) []Row {
	var rows []Row
	for _, c := range rel.Countries {
		for _, m := range c.Mandates {
			src, verified := primarySource(m)
			for _, p := range m.Phases {
				if p.SourceRef != "" {
					if s, ok := sourceByID(m, p.SourceRef); ok {
						src, verified = s.URL, string(s.VerifiedOn)
					}
				}
				rows = append(rows, Row{
					Country:      c.Code,
					CountryName:  c.Name,
					Region:       c.Region,
					MandateID:    m.ID,
					MandateTitle: m.Title,
					Scope:        strings.ToUpper(string(m.Scope)),
					Model:        string(m.Model),
					Status:       string(m.Status),
					Network:      m.Network,
					Formats:      strings.Join(m.Formats, ", "),
					PhaseID:      p.ID,
					StartsOn:     p.When(),
					Obligation:   string(p.Obligation),
					AppliesTo:    p.AppliesTo,
					Threshold:    p.Threshold.Describe(),
					PhaseStatus:  string(p.Status),
					Postponed:    !p.PreviousStartsOn.Empty(),
					SourceURL:    src,
					VerifiedOn:   verified,
					Anchor:       c.Code + "-" + m.ID + "-" + p.ID,
				})
			}
		}
	}
	return rows
}

func primarySource(m model.Mandate) (url, verified string) {
	for _, s := range m.Sources {
		if s.Official {
			return s.URL, string(s.VerifiedOn)
		}
	}
	if len(m.Sources) > 0 {
		return m.Sources[0].URL, string(m.Sources[0].VerifiedOn)
	}
	return "", ""
}

func sourceByID(m model.Mandate, id string) (model.Source, bool) {
	for _, s := range m.Sources {
		if s.ID == id {
			return s, true
		}
	}
	return model.Source{}, false
}

// HTML writes the static page for a release.
func HTML(w io.Writer, rel build.Release) error {
	rows := Flatten(rel)

	seen := map[string]bool{}
	var regions []string
	for _, r := range rows {
		if !seen[r.Region] {
			seen[r.Region] = true
			regions = append(regions, r.Region)
		}
	}

	tmpl, err := template.New("index.gohtml").Funcs(template.FuncMap{
		"year": func() int { return time.Now().UTC().Year() },
	}).ParseFS(templates, "templates/*.gohtml")
	if err != nil {
		return err
	}
	return tmpl.Execute(w, page{Release: rel, Rows: rows, Regions: regions})
}
