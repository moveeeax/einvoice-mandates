// Package diff compares two published releases and describes what changed in
// terms an integration engineer cares about: which deadline moved, which
// population got pulled in, which format changed.
//
// Every change is classified by significance so that a customer webhook can be
// filtered: "wake me for material changes, not for re-verification stamps".
package diff

import (
	"sort"
	"strconv"

	"github.com/moveeeax/einvoice-mandates/internal/build"
	"github.com/moveeeax/einvoice-mandates/internal/model"
)

// Significance ranks a change for alerting.
type Significance string

const (
	// Material changes alter what a taxpayer or an integration must do.
	Material Significance = "material"
	// Informational changes add or refine context without changing an obligation.
	Informational Significance = "informational"
	// Editorial changes are curation bookkeeping: re-verification dates, notes.
	Editorial Significance = "editorial"
)

// Kind of change.
type Kind string

const (
	CountryAdded   Kind = "country_added"
	CountryRemoved Kind = "country_removed"
	MandateAdded   Kind = "mandate_added"
	MandateRemoved Kind = "mandate_removed"
	MandateChanged Kind = "mandate_changed"
	PhaseAdded     Kind = "phase_added"
	PhaseRemoved   Kind = "phase_removed"
	PhaseChanged   Kind = "phase_changed"
	SourceChanged  Kind = "source_changed"
)

// Change is one difference between two releases.
type Change struct {
	Kind         Kind         `json:"kind"`
	Significance Significance `json:"significance"`
	Country      string       `json:"country"`
	Key          string       `json:"key"`
	Field        string       `json:"field,omitempty"`
	From         string       `json:"from,omitempty"`
	To           string       `json:"to,omitempty"`
	Summary      string       `json:"summary"`
}

// Result is the full comparison of two releases.
type Result struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Changes []Change `json:"changes"`
}

// Counts returns the number of changes at each significance level.
func (r Result) Counts() (material, informational, editorial int) {
	for _, c := range r.Changes {
		switch c.Significance {
		case Material:
			material++
		case Informational:
			informational++
		default:
			editorial++
		}
	}
	return
}

// Compare diffs two releases. The result is ordered by country, then by key,
// then by field, so the output is stable across runs.
func Compare(from, to build.Release) Result {
	res := Result{From: from.Release, To: to.Release}

	oldC := index(from.Countries)
	newC := index(to.Countries)

	for _, code := range union(oldC, newC) {
		o, inOld := oldC[code]
		n, inNew := newC[code]

		switch {
		case !inOld:
			res.Changes = append(res.Changes, Change{
				Kind: CountryAdded, Significance: Material, Country: code, Key: code,
				Summary: n.Name + " added to the dataset with " + plural(len(n.Mandates), "mandate", "mandates"),
			})
			continue
		case !inNew:
			res.Changes = append(res.Changes, Change{
				Kind: CountryRemoved, Significance: Material, Country: code, Key: code,
				Summary: o.Name + " removed from the dataset",
			})
			continue
		}
		res.Changes = append(res.Changes, compareCountry(o, n)...)
	}

	sort.SliceStable(res.Changes, func(i, j int) bool {
		a, b := res.Changes[i], res.Changes[j]
		if a.Country != b.Country {
			return a.Country < b.Country
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.Field < b.Field
	})
	return res
}

func compareCountry(o, n model.Country) []Change {
	var out []Change
	code := n.Code

	oldM := indexMandates(o)
	newM := indexMandates(n)

	for _, key := range unionM(oldM, newM) {
		om, inOld := oldM[key]
		nm, inNew := newM[key]
		switch {
		case !inOld:
			out = append(out, Change{
				Kind: MandateAdded, Significance: Material, Country: code, Key: key,
				Summary: n.Name + ": new mandate " + nm.Title + " (" + string(nm.Scope) + ", " + string(nm.Status) + ")",
			})
			continue
		case !inNew:
			out = append(out, Change{
				Kind: MandateRemoved, Significance: Material, Country: code, Key: key,
				Summary: n.Name + ": mandate " + om.Title + " removed",
			})
			continue
		}
		out = append(out, compareMandate(n.Name, code, key, om, nm)...)
	}
	return out
}

func compareMandate(countryName, code, key string, o, n model.Mandate) []Change {
	var out []Change

	field := func(name, from, to string, sig Significance, summary string) {
		if from == to {
			return
		}
		out = append(out, Change{
			Kind: MandateChanged, Significance: sig, Country: code, Key: key,
			Field: name, From: from, To: to, Summary: summary,
		})
	}

	field("status", string(o.Status), string(n.Status), Material,
		countryName+": "+n.Title+" status "+string(o.Status)+" -> "+string(n.Status))
	field("model", string(o.Model), string(n.Model), Material,
		countryName+": "+n.Title+" clearance model "+string(o.Model)+" -> "+string(n.Model))
	field("network", o.Network, n.Network, Material,
		countryName+": "+n.Title+" network "+orNone(o.Network)+" -> "+orNone(n.Network))
	field("formats", joinSorted(o.Formats), joinSorted(n.Formats), Material,
		countryName+": "+n.Title+" formats "+joinSorted(o.Formats)+" -> "+joinSorted(n.Formats))
	field("penalty", o.Penalty, n.Penalty, Informational,
		countryName+": "+n.Title+" penalty text updated")
	field("title", o.Title, n.Title, Editorial,
		countryName+": mandate title "+o.Title+" -> "+n.Title)

	out = append(out, comparePhases(countryName, code, key, o, n)...)
	out = append(out, compareSources(countryName, code, key, o, n)...)
	return out
}

func comparePhases(countryName, code, mandateKey string, o, n model.Mandate) []Change {
	var out []Change
	oldP := map[string]model.Phase{}
	for _, p := range o.Phases {
		oldP[p.ID] = p
	}
	newP := map[string]model.Phase{}
	for _, p := range n.Phases {
		newP[p.ID] = p
	}

	var ids []string
	seen := map[string]bool{}
	for _, p := range o.Phases {
		ids, seen[p.ID] = append(ids, p.ID), true
	}
	for _, p := range n.Phases {
		if !seen[p.ID] {
			ids = append(ids, p.ID)
		}
	}
	sort.Strings(ids)

	for _, id := range ids {
		key := mandateKey + "." + id
		op, inOld := oldP[id]
		np, inNew := newP[id]

		switch {
		case !inOld:
			out = append(out, Change{
				Kind: PhaseAdded, Significance: Material, Country: code, Key: key,
				To:      np.When(),
				Summary: countryName + ": new phase " + id + " from " + np.When() + " — " + np.AppliesTo,
			})
			continue
		case !inNew:
			out = append(out, Change{
				Kind: PhaseRemoved, Significance: Material, Country: code, Key: key,
				From:    op.When(),
				Summary: countryName + ": phase " + id + " (" + op.When() + ") removed",
			})
			continue
		}

		if op.StartsOn != np.StartsOn || op.StartsOnRule != np.StartsOnRule {
			out = append(out, Change{
				Kind: PhaseChanged, Significance: Material, Country: code, Key: key,
				Field: "starts_on", From: op.When(), To: np.When(),
				Summary: countryName + ": phase " + id + " " + moveVerb(op, np) + " " + op.When() + " -> " + np.When(),
			})
		}
		if op.Status != np.Status {
			out = append(out, Change{
				Kind: PhaseChanged, Significance: Material, Country: code, Key: key,
				Field: "status", From: string(op.Status), To: string(np.Status),
				Summary: countryName + ": phase " + id + " status " + string(op.Status) + " -> " + string(np.Status),
			})
		}
		if op.Obligation != np.Obligation {
			out = append(out, Change{
				Kind: PhaseChanged, Significance: Material, Country: code, Key: key,
				Field: "obligation", From: string(op.Obligation), To: string(np.Obligation),
				Summary: countryName + ": phase " + id + " obligation " + string(op.Obligation) + " -> " + string(np.Obligation),
			})
		}
		if ot, nt := op.Threshold.Describe(), np.Threshold.Describe(); ot != nt {
			out = append(out, Change{
				Kind: PhaseChanged, Significance: Material, Country: code, Key: key,
				Field: "threshold", From: ot, To: nt,
				Summary: countryName + ": phase " + id + " threshold " + orNone(ot) + " -> " + orNone(nt),
			})
		}
		if op.AppliesTo != np.AppliesTo {
			out = append(out, Change{
				Kind: PhaseChanged, Significance: Informational, Country: code, Key: key,
				Field: "applies_to", From: op.AppliesTo, To: np.AppliesTo,
				Summary: countryName + ": phase " + id + " covered population reworded",
			})
		}
	}
	return out
}

func compareSources(countryName, code, mandateKey string, o, n model.Mandate) []Change {
	var out []Change
	oldS := map[string]model.Source{}
	for _, s := range o.Sources {
		oldS[s.ID] = s
	}
	var ids []string
	for _, s := range n.Sources {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)

	newS := map[string]model.Source{}
	for _, s := range n.Sources {
		newS[s.ID] = s
	}

	for _, id := range ids {
		ns := newS[id]
		os, ok := oldS[id]
		key := mandateKey + ".source:" + id
		if !ok {
			out = append(out, Change{
				Kind: SourceChanged, Significance: Informational, Country: code, Key: key,
				Field: "url", To: ns.URL,
				Summary: countryName + ": new citation " + id + " (" + ns.Publisher + ")",
			})
			continue
		}
		if os.URL != ns.URL {
			out = append(out, Change{
				Kind: SourceChanged, Significance: Informational, Country: code, Key: key,
				Field: "url", From: os.URL, To: ns.URL,
				Summary: countryName + ": citation " + id + " now points at a different page",
			})
		}
		if os.VerifiedOn != ns.VerifiedOn {
			out = append(out, Change{
				Kind: SourceChanged, Significance: Editorial, Country: code, Key: key,
				Field: "verified_on", From: string(os.VerifiedOn), To: string(ns.VerifiedOn),
				Summary: countryName + ": citation " + id + " re-verified on " + string(ns.VerifiedOn),
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------

func index(cs []model.Country) map[string]model.Country {
	m := make(map[string]model.Country, len(cs))
	for _, c := range cs {
		m[c.Code] = c
	}
	return m
}

func indexMandates(c model.Country) map[string]model.Mandate {
	m := make(map[string]model.Mandate, len(c.Mandates))
	for _, x := range c.Mandates {
		m[x.Key(c.Code)] = x
	}
	return m
}

func union(a, b map[string]model.Country) []string {
	seen := map[string]bool{}
	var out []string
	for k := range a {
		seen[k] = true
		out = append(out, k)
	}
	for k := range b {
		if !seen[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func unionM(a, b map[string]model.Mandate) []string {
	seen := map[string]bool{}
	var out []string
	for k := range a {
		seen[k] = true
		out = append(out, k)
	}
	for k := range b {
		if !seen[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// moveVerb names what happened to a phase's effective date. A phase that
// gains a real date after only having a trigger rule is the single most
// valuable event in this dataset, so it gets its own wording.
func moveVerb(o, n model.Phase) string {
	switch {
	case o.StartsOn.Empty() && !n.StartsOn.Empty():
		return "date fixed,"
	case !o.StartsOn.Empty() && n.StartsOn.Empty():
		return "date withdrawn,"
	case o.StartsOn.Empty() && n.StartsOn.Empty():
		return "trigger reworded,"
	case later(n.StartsOn, o.StartsOn):
		return "postponed"
	default:
		return "brought forward"
	}
}

func later(a, b model.Date) bool {
	ta, erra := a.Parse()
	tb, errb := b.Parse()
	if erra != nil || errb != nil {
		return string(a) > string(b)
	}
	return ta.After(tb)
}

func joinSorted(in []string) string {
	cp := append([]string(nil), in...)
	sort.Strings(cp)
	out := ""
	for i, s := range cp {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func plural(n int, one, many string) string {
	word := many
	if n == 1 {
		word = one
	}
	return strconv.Itoa(n) + " " + word
}
