// Package validate is the schema linter over the country YAML tree.
//
// It is deliberately strict: this dataset is sold on being correct, so a
// missing source URL or an unparseable date is an error, not a warning. The
// warning level is reserved for things a human should look at but which do not
// make the artefact wrong (a citation that has not been re-checked in a year,
// a mandate in force whose phases are all still "planned").
package validate

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/moveeeax/einvoice-mandates/internal/dataset"
	"github.com/moveeeax/einvoice-mandates/internal/model"
)

// Severity of a finding.
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

// Finding is one lint result, anchored at a file and a dotted field path.
type Finding struct {
	Severity Severity `json:"severity"`
	File     string   `json:"file"`
	Path     string   `json:"path"`
	Message  string   `json:"message"`
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s: %s: %s", f.Severity, f.File, f.Path, f.Message)
}

// Report is the outcome of a lint run.
type Report struct {
	Findings []Finding `json:"findings"`
}

// Errors reports whether any finding is fatal.
func (r Report) Errors() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == Error {
			n++
		}
	}
	return n
}

// Warnings counts non-fatal findings.
func (r Report) Warnings() int { return len(r.Findings) - r.Errors() }

// Options tunes the lint run.
type Options struct {
	// Today is the reference date for staleness checks. Zero means time.Now.
	Today time.Time
	// StaleAfter is how old a source verification may be before it warns.
	StaleAfter time.Duration
}

// DefaultStaleAfter is the maintenance promise: no citation older than a year.
const DefaultStaleAfter = 365 * 24 * time.Hour

type linter struct {
	opts     Options
	findings []Finding
	file     string
}

// Run lints a loaded dataset, including cross-file checks such as duplicate
// country codes.
func Run(files []dataset.File, opts Options) Report {
	if opts.Today.IsZero() {
		opts.Today = time.Now().UTC()
	}
	if opts.StaleAfter == 0 {
		opts.StaleAfter = DefaultStaleAfter
	}

	l := &linter{opts: opts}
	seenCountry := map[string]string{}

	for _, f := range files {
		l.file = f.Path
		c := f.Country
		if prev, dup := seenCountry[c.Code]; dup && c.Code != "" {
			l.errf("country", "duplicate country code %q, already defined in %s", c.Code, prev)
		}
		if c.Code != "" {
			seenCountry[c.Code] = f.Path
		}
		l.country(c)
	}

	sort.SliceStable(l.findings, func(i, j int) bool {
		if l.findings[i].File != l.findings[j].File {
			return l.findings[i].File < l.findings[j].File
		}
		return l.findings[i].Path < l.findings[j].Path
	})
	return Report{Findings: l.findings}
}

func (l *linter) errf(path, format string, args ...any) {
	l.findings = append(l.findings, Finding{Error, l.file, path, fmt.Sprintf(format, args...)})
}

func (l *linter) warnf(path, format string, args ...any) {
	l.findings = append(l.findings, Finding{Warning, l.file, path, fmt.Sprintf(format, args...)})
}

func (l *linter) enum(path, enum, value string) {
	if value == "" {
		l.errf(path, "is required")
		return
	}
	if ok, allowed := model.Allowed(enum, value); !ok {
		l.errf(path, "%q is not a valid %s (allowed: %s)", value, enum, allowed)
	}
}

func (l *linter) required(path, value string) {
	if strings.TrimSpace(value) == "" {
		l.errf(path, "is required")
	}
}

// date validates a required date field and returns it parsed.
func (l *linter) date(path string, d model.Date, required bool) (time.Time, bool) {
	if d.Empty() {
		if required {
			l.errf(path, "is required")
		}
		return time.Time{}, false
	}
	t, err := d.Parse()
	if err != nil {
		l.errf(path, "%q is not a valid date, expected YYYY-MM-DD", string(d))
		return time.Time{}, false
	}
	return t, true
}

func (l *linter) country(c model.Country) {
	if c.SchemaVersion != model.SchemaVersion {
		l.errf("schema_version", "is %d, this build supports %d", c.SchemaVersion, model.SchemaVersion)
	}
	if !model.IsAlpha2(c.Code) {
		l.errf("country", "%q must be an ISO 3166-1 alpha-2 code in upper case", c.Code)
	}
	l.required("country_name", c.Name)
	l.required("tax_authority", c.TaxAuthority)
	l.enum("region", "region", c.Region)
	l.enum("review_status", "review_status", string(c.ReviewStatus))

	if reviewed, ok := l.date("last_reviewed", c.LastReviewed, true); ok {
		if reviewed.After(l.opts.Today) {
			l.errf("last_reviewed", "%s is in the future", c.LastReviewed)
		} else if l.opts.Today.Sub(reviewed) > l.opts.StaleAfter {
			l.warnf("last_reviewed", "country not reviewed since %s", c.LastReviewed)
		}
	}

	if len(c.Mandates) == 0 {
		l.errf("mandates", "at least one mandate is required")
	}

	seen := map[string]bool{}
	for i, m := range c.Mandates {
		path := fmt.Sprintf("mandates[%d]", i)
		if seen[m.ID] {
			l.errf(path+".id", "duplicate mandate id %q within country", m.ID)
		}
		seen[m.ID] = true
		l.mandate(path, m)
	}
}

func (l *linter) mandate(path string, m model.Mandate) {
	if !model.IsSlug(m.ID) {
		l.errf(path+".id", "%q must be lower-kebab-case", m.ID)
	}
	l.required(path+".title", m.Title)
	l.enum(path+".scope", "scope", string(m.Scope))
	l.enum(path+".model", "model", string(m.Model))
	l.enum(path+".status", "status", string(m.Status))

	if len(m.Formats) == 0 {
		l.errf(path+".formats", "at least one format is required")
	}

	sourceIDs := l.sources(path, m.Sources)

	if len(m.Phases) == 0 {
		l.errf(path+".phases", "at least one phase is required")
	}

	var (
		prev       time.Time
		havePrev   bool
		seenPhase  = map[string]bool{}
		anyInForce bool
	)
	for i, p := range m.Phases {
		ppath := fmt.Sprintf("%s.phases[%d]", path, i)
		if !model.IsSlug(p.ID) {
			l.errf(ppath+".id", "%q must be lower-kebab-case", p.ID)
		}
		if seenPhase[p.ID] {
			l.errf(ppath+".id", "duplicate phase id %q within mandate", p.ID)
		}
		seenPhase[p.ID] = true

		l.required(ppath+".applies_to", p.AppliesTo)
		l.enum(ppath+".obligation", "obligation", string(p.Obligation))
		l.enum(ppath+".status", "phase_status", string(p.Status))

		// A phase either has a date, or a rule that says what fixes the date.
		// Never both, never neither.
		var (
			start time.Time
			ok    bool
		)
		switch {
		case p.StartsOn.Empty() && p.StartsOnRule == "":
			l.errf(ppath+".starts_on", "is required, or set starts_on_rule when the date is pegged to a future event")
		case !p.StartsOn.Empty() && p.StartsOnRule != "":
			l.errf(ppath+".starts_on_rule", "must not be set together with starts_on; a known date wins")
		case p.StartsOn.Empty():
			if p.Status != model.PhaseAwaitingTrigger {
				l.errf(ppath+".status", "must be awaiting_trigger when the phase has no starts_on, got %q", p.Status)
			}
		default:
			start, ok = l.date(ppath+".starts_on", p.StartsOn, true)
		}

		if ok {
			if havePrev && start.Before(prev) {
				l.errf(ppath+".starts_on", "phases must be listed in chronological order (%s precedes the previous phase)", p.StartsOn)
			}
			prev, havePrev = start, true

			switch p.Status {
			case model.PhaseInForce:
				anyInForce = true
				if start.After(l.opts.Today) {
					l.errf(ppath+".status", "marked in_force but starts_on %s is in the future", p.StartsOn)
				}
			case model.PhasePlanned:
				if !start.After(l.opts.Today) {
					l.warnf(ppath+".status", "still marked planned but starts_on %s has passed", p.StartsOn)
				}
			case model.PhaseAwaitingTrigger:
				l.errf(ppath+".status", "awaiting_trigger is only valid for a phase without starts_on")
			}
		}

		if !p.PreviousStartsOn.Empty() {
			if old, ok := l.date(ppath+".previous_starts_on", p.PreviousStartsOn, false); ok && ok2(start, ok) && !old.Before(start) {
				l.errf(ppath+".previous_starts_on", "%s is not earlier than starts_on %s; a postponement moves a date forward", p.PreviousStartsOn, p.StartsOn)
			}
		}

		l.threshold(ppath+".threshold", p.Threshold)

		if p.SourceRef != "" && !sourceIDs[p.SourceRef] {
			l.errf(ppath+".source_ref", "%q does not match any source id on this mandate", p.SourceRef)
		}
	}

	if m.Status == model.StatusInForce && !anyInForce {
		l.warnf(path+".status", "mandate is in_force but no phase is in_force")
	}
}

// ok2 guards the previous_starts_on comparison when starts_on failed to parse.
func ok2(t time.Time, parsed bool) bool { return parsed && !t.IsZero() }

func (l *linter) sources(path string, sources []model.Source) map[string]bool {
	ids := map[string]bool{}
	if len(sources) == 0 {
		l.errf(path+".sources", "at least one source is required; an unsourced field is not shippable")
		return ids
	}
	official := false
	for i, s := range sources {
		spath := fmt.Sprintf("%s.sources[%d]", path, i)
		if !model.IsSlug(s.ID) {
			l.errf(spath+".id", "%q must be lower-kebab-case", s.ID)
		}
		if ids[s.ID] {
			l.errf(spath+".id", "duplicate source id %q within mandate", s.ID)
		}
		ids[s.ID] = true

		l.required(spath+".title", s.Title)
		l.required(spath+".publisher", s.Publisher)

		if u, err := url.Parse(s.URL); err != nil || u.Scheme != "https" || u.Host == "" {
			l.errf(spath+".url", "%q must be an absolute https URL", s.URL)
		}
		if !model.IsLanguage(s.Language) {
			l.errf(spath+".language", "%q must be a lower-case ISO 639-1 code", s.Language)
		}
		if v, ok := l.date(spath+".verified_on", s.VerifiedOn, true); ok {
			if v.After(l.opts.Today) {
				l.errf(spath+".verified_on", "%s is in the future", s.VerifiedOn)
			} else if l.opts.Today.Sub(v) > l.opts.StaleAfter {
				l.warnf(spath+".verified_on", "source last verified %s, older than the freshness promise", s.VerifiedOn)
			}
		}
		if s.Official {
			official = true
		}
	}
	if !official {
		l.warnf(path+".sources", "no source is marked official; a mandate should cite the tax authority itself")
	}
	return ids
}

func (l *linter) threshold(path string, t *model.Threshold) {
	if t == nil {
		return
	}
	l.required(path+".metric", t.Metric)
	l.enum(path+".operator", "operator", t.Operator)
	if t.Currency != "" && !model.IsCurrency(t.Currency) {
		l.errf(path+".currency", "%q must be an upper-case ISO 4217 code", t.Currency)
	}
	if t.Operator == "between" {
		if t.ValueMax <= t.Value {
			l.errf(path+".value_max", "must be greater than value for a between threshold")
		}
	} else if t.ValueMax != 0 {
		l.errf(path+".value_max", "is only meaningful when operator is between")
	}
	if t.Value < 0 {
		l.errf(path+".value", "must not be negative")
	}
}
