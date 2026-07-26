package diff

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Filter selects which changes are rendered or delivered.
type Filter struct {
	// MinSignificance drops anything below the given level.
	// "" means render everything.
	MinSignificance Significance
	// Countries, if non-empty, restricts output to these alpha-2 codes.
	Countries []string
}

var rank = map[Significance]int{Editorial: 0, Informational: 1, Material: 2}

// Apply returns the subset of changes matching the filter.
func (r Result) Apply(f Filter) Result {
	min := rank[f.MinSignificance]
	if f.MinSignificance == "" {
		min = 0
	}
	want := map[string]bool{}
	for _, c := range f.Countries {
		want[strings.ToUpper(strings.TrimSpace(c))] = true
	}

	out := Result{From: r.From, To: r.To}
	for _, c := range r.Changes {
		if rank[c.Significance] < min {
			continue
		}
		if len(want) > 0 && !want[c.Country] {
			continue
		}
		out.Changes = append(out.Changes, c)
	}
	return out
}

// RenderText writes the diff as a plain-text summary grouped by country. This
// is the format meant to be pasted into a changelog or a customer email.
func RenderText(w io.Writer, r Result) error {
	material, informational, editorial := r.Counts()

	fmt.Fprintf(w, "e-invoicing mandate changes: %s -> %s\n", r.From, r.To)
	fmt.Fprintf(w, "%d change(s): %d material, %d informational, %d editorial\n",
		len(r.Changes), material, informational, editorial)

	if len(r.Changes) == 0 {
		fmt.Fprintln(w, "\nNo changes.")
		return nil
	}

	byCountry := map[string][]Change{}
	var order []string
	for _, c := range r.Changes {
		if _, ok := byCountry[c.Country]; !ok {
			order = append(order, c.Country)
		}
		byCountry[c.Country] = append(byCountry[c.Country], c)
	}
	sort.Strings(order)

	for _, code := range order {
		changes := byCountry[code]
		fmt.Fprintf(w, "\n%s (%d)\n", code, len(changes))
		for _, c := range changes {
			fmt.Fprintf(w, "  %s %-13s %s\n", marker(c.Significance), c.Significance, c.Summary)
			if c.Field != "" && c.From != "" && c.To != "" {
				fmt.Fprintf(w, "      %s: %s -> %s\n", c.Field, c.From, c.To)
			}
		}
	}
	return nil
}

func marker(s Significance) string {
	switch s {
	case Material:
		return "!"
	case Informational:
		return "+"
	default:
		return "-"
	}
}
