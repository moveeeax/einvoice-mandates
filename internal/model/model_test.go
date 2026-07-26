package model_test

import (
	"testing"

	"github.com/moveeeax/einvoice-mandates/internal/model"
)

func TestThresholdDescribe(t *testing.T) {
	cases := []struct {
		name string
		in   *model.Threshold
		want string
	}{
		{"nil threshold", nil, ""},
		{
			"greater than",
			&model.Threshold{Metric: "annual_turnover", Operator: "gt", Value: 800000, Currency: "EUR"},
			"annual_turnover > EUR 800000",
		},
		{
			"band",
			&model.Threshold{Metric: "annual_turnover", Operator: "between", Value: 1000000, ValueMax: 5000000, Currency: "MYR"},
			"annual_turnover between MYR 1000000 and MYR 5000000",
		},
		{
			"no currency",
			&model.Threshold{Metric: "headcount", Operator: "gte", Value: 250},
			"headcount >= 250",
		},
		{
			"fractional value keeps its decimals",
			&model.Threshold{Metric: "invoice_amount", Operator: "lte", Value: 12.5, Currency: "PLN"},
			"invoice_amount <= PLN 12.5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Describe(); got != tc.want {
				t.Errorf("Describe() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPhaseWhen(t *testing.T) {
	dated := model.Phase{StartsOn: "2027-01-01"}
	if got := dated.When(); got != "2027-01-01" {
		t.Errorf("dated phase When() = %q", got)
	}

	pegged := model.Phase{StartsOnRule: "Twelve months after publication"}
	if got := pegged.When(); got != "Twelve months after publication" {
		t.Errorf("pegged phase When() = %q", got)
	}

	if got := (model.Phase{}).When(); got != "unknown" {
		t.Errorf("empty phase When() = %q, want unknown", got)
	}
}

func TestDateParsing(t *testing.T) {
	if !model.Date("2026-02-01").Valid() {
		t.Error("a well-formed date should be valid")
	}
	for _, bad := range []string{"01/02/2026", "2026-2-1", "2026-13-01", "next Tuesday", ""} {
		if model.Date(bad).Valid() {
			t.Errorf("%q should not parse as a date", bad)
		}
	}
	if !model.Date("  ").Empty() {
		t.Error("whitespace should count as empty")
	}
}

func TestKeysAreStable(t *testing.T) {
	m := model.Mandate{ID: "b2b-ksef"}
	p := model.Phase{ID: "large-taxpayers"}

	mk := m.Key("PL")
	if mk != "PL.b2b-ksef" {
		t.Errorf("mandate key = %q", mk)
	}
	if pk := p.Key(mk); pk != "PL.b2b-ksef.large-taxpayers" {
		t.Errorf("phase key = %q", pk)
	}
}

func TestEnumMembership(t *testing.T) {
	if ok, _ := model.Allowed("scope", "b2b"); !ok {
		t.Error("b2b should be a valid scope")
	}
	ok, allowed := model.Allowed("scope", "b2x")
	if ok {
		t.Error("b2x should not be a valid scope")
	}
	if allowed == "" {
		t.Error("a rejection should list the permitted values")
	}
	if ok, _ := model.Allowed("no-such-enum", "anything"); ok {
		t.Error("an unknown enumeration should never validate")
	}
}
