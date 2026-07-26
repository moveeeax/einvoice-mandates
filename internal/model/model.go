// Package model defines the on-disk (YAML) and published (JSON) shape of the
// e-invoicing mandate dataset.
//
// One YAML file describes one country. A country holds one or more mandates
// (B2G, B2B, e-reporting ...), and each mandate holds an ordered list of
// phases. A phase is the unit an ERP vendor actually cares about: "from this
// date, this population of taxpayers must do this thing".
package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SchemaVersion is the version of the dataset schema this binary reads and
// writes. Bumped only on a breaking change to the published JSON.
const SchemaVersion = 1

// DateLayout is the only date format accepted anywhere in the dataset.
const DateLayout = "2006-01-02"

// Date is a calendar date without a time zone. It is stored and published as
// a plain "YYYY-MM-DD" string so that a legal effective date never shifts
// because of a reader's locale.
type Date string

// Parse returns the date as a time.Time in UTC.
func (d Date) Parse() (time.Time, error) {
	return time.Parse(DateLayout, string(d))
}

// Valid reports whether the date is a well-formed calendar date.
func (d Date) Valid() bool {
	_, err := d.Parse()
	return err == nil
}

// Empty reports whether the date is unset.
func (d Date) Empty() bool { return strings.TrimSpace(string(d)) == "" }

// Country is one YAML file in data/countries.
type Country struct {
	SchemaVersion int    `yaml:"schema_version" json:"schema_version"`
	Code          string `yaml:"country" json:"country"`           // ISO 3166-1 alpha-2
	Name          string `yaml:"country_name" json:"country_name"` // English short name
	Region        string `yaml:"region" json:"region"`             // EU, EEA, LATAM, MEA, APAC, NA, UK
	TaxAuthority  string `yaml:"tax_authority" json:"tax_authority"`

	// ReviewStatus is the honesty flag of the dataset. Only "verified" files
	// may be published in a release without an explicit override.
	ReviewStatus ReviewStatus `yaml:"review_status" json:"review_status"`
	LastReviewed Date         `yaml:"last_reviewed" json:"last_reviewed"`

	Mandates []Mandate `yaml:"mandates" json:"mandates"`
	Notes    string    `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// Mandate is one legal obligation programme within a country.
type Mandate struct {
	ID      string         `yaml:"id" json:"id"`
	Title   string         `yaml:"title" json:"title"`
	Scope   Scope          `yaml:"scope" json:"scope"`
	Model   ClearanceModel `yaml:"model" json:"model"`
	Status  MandateStatus  `yaml:"status" json:"status"`
	Network string         `yaml:"network,omitempty" json:"network,omitempty"`
	Formats []string       `yaml:"formats" json:"formats"`
	Phases  []Phase        `yaml:"phases" json:"phases"`
	Penalty string         `yaml:"penalty,omitempty" json:"penalty,omitempty"`
	Sources []Source       `yaml:"sources" json:"sources"`
	Notes   string         `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// Phase is a dated step of a mandate: who is pulled in, when, to do what.
type Phase struct {
	ID string `yaml:"id" json:"id"`
	// StartsOn is the effective date. It is empty exactly when the law is on
	// the books but its start is pegged to a future event, in which case
	// StartsOnRule carries that trigger. Guessing a date instead is the single
	// worst thing this dataset could do, so the schema refuses to.
	StartsOn     Date   `yaml:"starts_on,omitempty" json:"starts_on,omitempty"`
	StartsOnRule string `yaml:"starts_on_rule,omitempty" json:"starts_on_rule,omitempty"`

	Obligation Obligation  `yaml:"obligation" json:"obligation"`
	AppliesTo  string      `yaml:"applies_to" json:"applies_to"`
	Threshold  *Threshold  `yaml:"threshold,omitempty" json:"threshold,omitempty"`
	Status     PhaseStatus `yaml:"status" json:"status"`
	// PreviousStartsOn records the date this phase was moved from, when a
	// mandate has been postponed. It is what makes a slipped deadline
	// auditable rather than silently rewritten.
	PreviousStartsOn Date   `yaml:"previous_starts_on,omitempty" json:"previous_starts_on,omitempty"`
	SourceRef        string `yaml:"source_ref,omitempty" json:"source_ref,omitempty"`
}

// Threshold is the size test that decides whether a taxpayer is in a phase.
type Threshold struct {
	Metric   string  `yaml:"metric" json:"metric"`     // annual_turnover, headcount, ...
	Operator string  `yaml:"operator" json:"operator"` // gt, gte, lt, lte, between
	Value    float64 `yaml:"value" json:"value"`
	ValueMax float64 `yaml:"value_max,omitempty" json:"value_max,omitempty"`
	Currency string  `yaml:"currency,omitempty" json:"currency,omitempty"` // ISO 4217
	Period   string  `yaml:"period,omitempty" json:"period,omitempty"`
}

// Source is an official citation. Every mandate needs at least one.
type Source struct {
	ID         string `yaml:"id" json:"id"`
	URL        string `yaml:"url" json:"url"`
	Title      string `yaml:"title" json:"title"`
	Publisher  string `yaml:"publisher" json:"publisher"`
	Language   string `yaml:"language" json:"language"` // ISO 639-1
	VerifiedOn Date   `yaml:"verified_on" json:"verified_on"`
	Official   bool   `yaml:"official" json:"official"`
}

// ---------------------------------------------------------------------------
// Enumerations
// ---------------------------------------------------------------------------

type ReviewStatus string

const (
	ReviewDraft    ReviewStatus = "draft"
	ReviewVerified ReviewStatus = "verified"
)

type Scope string

const (
	ScopeB2G        Scope = "b2g"
	ScopeB2B        Scope = "b2b"
	ScopeB2C        Scope = "b2c"
	ScopeEReporting Scope = "e-reporting"
)

type ClearanceModel string

const (
	ModelClearance        ClearanceModel = "clearance"
	ModelCentralised      ClearanceModel = "centralised-exchange"
	ModelInteroperability ClearanceModel = "interoperability"
	ModelPostAudit        ClearanceModel = "post-audit"
	ModelRealTimeReport   ClearanceModel = "real-time-reporting"
)

type MandateStatus string

const (
	StatusAnnounced MandateStatus = "announced"
	StatusDraftLaw  MandateStatus = "draft_law"
	StatusEnacted   MandateStatus = "enacted"
	StatusInForce   MandateStatus = "in_force"
	StatusPostponed MandateStatus = "postponed"
	StatusRepealed  MandateStatus = "repealed"
)

type Obligation string

const (
	ObligationIssue   Obligation = "issue"
	ObligationReceive Obligation = "receive"
	ObligationReport  Obligation = "report"
	ObligationStore   Obligation = "store"
)

type PhaseStatus string

const (
	PhaseInForce PhaseStatus = "in_force"
	PhasePlanned PhaseStatus = "planned"
	// PhaseAwaitingTrigger is a phase whose start is fixed by law but pegged
	// to an event that has not happened yet (typically the publication of an
	// implementing regulation).
	PhaseAwaitingTrigger PhaseStatus = "awaiting_trigger"
	PhasePostponed       PhaseStatus = "postponed"
	PhaseCancelled       PhaseStatus = "cancelled"
)

// When renders the effective date of a phase, or its trigger rule when the
// date is not yet fixed.
func (p Phase) When() string {
	if !p.StartsOn.Empty() {
		return string(p.StartsOn)
	}
	if p.StartsOnRule != "" {
		return p.StartsOnRule
	}
	return "unknown"
}

var (
	reviewStatuses  = set(string(ReviewDraft), string(ReviewVerified))
	scopes          = set(string(ScopeB2G), string(ScopeB2B), string(ScopeB2C), string(ScopeEReporting))
	clearanceModels = set(string(ModelClearance), string(ModelCentralised), string(ModelInteroperability), string(ModelPostAudit), string(ModelRealTimeReport))
	mandateStatuses = set(string(StatusAnnounced), string(StatusDraftLaw), string(StatusEnacted), string(StatusInForce), string(StatusPostponed), string(StatusRepealed))
	obligations     = set(string(ObligationIssue), string(ObligationReceive), string(ObligationReport), string(ObligationStore))
	phaseStatuses   = set(string(PhaseInForce), string(PhasePlanned), string(PhaseAwaitingTrigger), string(PhasePostponed), string(PhaseCancelled))
	regions         = set("EU", "EEA", "UK", "NA", "LATAM", "MEA", "APAC")
	operators       = set("gt", "gte", "lt", "lte", "between", "eq")
)

func set(vals ...string) map[string]bool {
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		m[v] = true
	}
	return m
}

func sorted(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// small sets: insertion sort keeps this dependency-free and deterministic
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return strings.Join(out, ", ")
}

// Allowed reports whether v is a member of the named enumeration, and returns
// a human-readable list of the permitted values for the error message.
func Allowed(enum, v string) (bool, string) {
	m, ok := enums[enum]
	if !ok {
		return false, ""
	}
	return m[v], sorted(m)
}

var enums = map[string]map[string]bool{
	"review_status": reviewStatuses,
	"scope":         scopes,
	"model":         clearanceModels,
	"status":        mandateStatuses,
	"obligation":    obligations,
	"phase_status":  phaseStatuses,
	"region":        regions,
	"operator":      operators,
}

var (
	alpha2Re = regexp.MustCompile(`^[A-Z]{2}$`)
	slugRe   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	langRe   = regexp.MustCompile(`^[a-z]{2}$`)
	currRe   = regexp.MustCompile(`^[A-Z]{3}$`)
)

// IsAlpha2 reports whether s looks like an ISO 3166-1 alpha-2 code.
func IsAlpha2(s string) bool { return alpha2Re.MatchString(s) }

// IsSlug reports whether s is a lower-kebab-case identifier.
func IsSlug(s string) bool { return slugRe.MatchString(s) }

// IsLanguage reports whether s looks like an ISO 639-1 code.
func IsLanguage(s string) bool { return langRe.MatchString(s) }

// IsCurrency reports whether s looks like an ISO 4217 code.
func IsCurrency(s string) bool { return currRe.MatchString(s) }

// Key returns the stable dotted identity of a mandate, used as the join key
// when diffing two releases.
func (m Mandate) Key(country string) string {
	return fmt.Sprintf("%s.%s", country, m.ID)
}

// Key returns the stable dotted identity of a phase.
func (p Phase) Key(mandateKey string) string {
	return fmt.Sprintf("%s.%s", mandateKey, p.ID)
}

// Describe renders a threshold as a single readable clause.
func (t *Threshold) Describe() string {
	if t == nil {
		return ""
	}
	amount := func(v float64) string {
		s := fmt.Sprintf("%.2f", v)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		if t.Currency != "" {
			return t.Currency + " " + s
		}
		return s
	}
	switch t.Operator {
	case "between":
		return fmt.Sprintf("%s between %s and %s", t.Metric, amount(t.Value), amount(t.ValueMax))
	case "gt":
		return fmt.Sprintf("%s > %s", t.Metric, amount(t.Value))
	case "gte":
		return fmt.Sprintf("%s >= %s", t.Metric, amount(t.Value))
	case "lt":
		return fmt.Sprintf("%s < %s", t.Metric, amount(t.Value))
	case "lte":
		return fmt.Sprintf("%s <= %s", t.Metric, amount(t.Value))
	default:
		return fmt.Sprintf("%s %s %s", t.Metric, t.Operator, amount(t.Value))
	}
}
