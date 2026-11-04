# einvoice-mandates

A versioned, machine-readable dataset of e-invoicing mandates — country, model,
network, formats, phase dates, scope thresholds, penalties — with a CLI that
tells you exactly what changed between two releases.

Most ERP and AP-automation teams keep this matrix in a spreadsheet that one
engineer updates when someone notices a deadline moved. This repository is the
same matrix as YAML under version control, with a linter that refuses
unsourced or self-contradictory entries, a reproducible JSON build artefact,
and a diff that separates "Poland's mandate went live" from "we re-checked the
citation".

**Status:** Phase 0. Eight seed countries (IT, PL, DE, FR, ES, SA, MX, MY), all
still `review_status: draft` — see [`data/README.md`](data/README.md). The
schema, the linter, the build, the diff and the static page are done and
tested. Source-change watching and webhook delivery are not built yet.

## What is in the box

| | |
| --- | --- |
| `data/countries/*.yaml` | The hand-curated source of truth, one file per country. |
| `validate` | Schema linter. Cross-checks dates against phase status, rejects unsourced mandates, warns on citations older than a year. |
| `build` | Compiles the YAML tree into one reproducible JSON artefact with a schema version and a release tag. |
| `diff` | Compares two release artefacts and classifies every change as material, informational or editorial. |
| `render` | Writes a single self-contained HTML table with a permalink per phase. |

## Install

```sh
git clone https://github.com/moveeeax/einvoice-mandates.git
cd einvoice-mandates
make build          # -> bin/einvoice-mandates
```

Requires Go 1.26 or newer. There are no runtime dependencies beyond the Go
standard library and `go.yaml.in/yaml/v3`.

## Usage

Everything below runs against the files in this repository.

### Lint the dataset

```sh
$ ./bin/einvoice-mandates validate
8 file(s) checked: 0 error(s), 0 warning(s)
```

`-strict` promotes warnings to failures, which is what CI runs: a citation that
has not been re-verified within a year is treated as a defect, not a nag.

### Build a release artefact

```sh
$ mkdir -p dist
$ ./bin/einvoice-mandates build -release 2026.07 -allow-draft -o dist/release.json
wrote dist/release.json: 8 countries, 15 mandates, 40 phases
```

Without `-allow-draft` the build refuses, because no country has been
field-by-field verified yet. That refusal is the point: the artefact is only
worth what its review status says it is.

### Diff two releases

Two example artefacts are committed so this runs with no setup. They differ by
a realistic quarter of mandate news: Poland's KSeF going live on its re-cut
timetable, Malaysia's fourth band slipping six months, Spain's Verifactu being
tracked as its own mandate, Germany's B2B obligation moving from enacted to in
force.

```sh
$ ./bin/einvoice-mandates diff examples/release-2026.06.json examples/release-2026.07.json -min material -country PL
e-invoicing mandate changes: 2026.06 -> 2026.07
4 change(s): 4 material, 0 informational, 0 editorial

PL (4)
  ! material      Poland: phase all-other-taxpayers postponed 2024-07-01 -> 2026-04-01
      starts_on: 2024-07-01 -> 2026-04-01
  ! material      Poland: phase all-other-taxpayers status postponed -> in_force
      status: postponed -> in_force
  ! material      Poland: phase large-taxpayers postponed 2024-07-01 -> 2026-02-01
      starts_on: 2024-07-01 -> 2026-02-01
  ! material      Poland: phase large-taxpayers status postponed -> in_force
      status: postponed -> in_force
```

Drop the filters to see the whole quarter, or add `-json` for the structured
payload — the same shape a change webhook will carry:

```json
{
  "kind": "phase_changed",
  "significance": "material",
  "country": "MY",
  "key": "MY.b2b-myinvois.band-1m-5m",
  "field": "starts_on",
  "from": "2026-01-01",
  "to": "2026-07-01",
  "summary": "Malaysia: phase band-1m-5m postponed 2026-01-01 -> 2026-07-01"
}
```

`-exit-code` makes the command exit 1 when anything survives the filter, so it
drops straight into a scheduled job.

### Render the public table

```sh
$ ./bin/einvoice-mandates render examples/release-2026.07.json -o dist/index.html
wrote dist/index.html: 40 rows
```

One file, no external assets, filterable by text, region and phase state, with
`#COUNTRY-mandate-phase` permalinks.

`make dist` does the validate, build and render in one step.

## Design decisions worth knowing

**A phase is the unit, not a country.** "Germany, 2028" is useless to an
integration engineer. `DE.b2b-erechnung.issue-all` — issue obligation, all
domestic businesses, no turnover floor, from 2028-01-01, citing the BMF
circular — is something you can hold a roadmap against. Those dotted keys are
stable across releases and are what the diff joins on.

**Undated law is modelled, not guessed.** Spain's Crea y Crece B2B mandate is
enacted but starts twelve months after an implementing regulation that has not
been published. The schema has `starts_on_rule` and the phase status
`awaiting_trigger` for exactly this, and the linter rejects a phase that tries
to carry both a rule and a date. Inventing a plausible date would be the single
most damaging thing this dataset could do.

**Postponements keep their history.** `previous_starts_on` records where a date
moved from. Poland's KSeF phases still carry 2024-07-01; Malaysia's fourth band
still carries 2026-01-01. A silently rewritten deadline is indistinguishable
from a deadline that never moved.

**Not every change deserves a page.** Re-verifying a URL is editorial; a
threshold changing is material. Grading them is what makes a change feed usable
rather than something customers mute after a fortnight.

**Builds are reproducible.** Same commit, same bytes — apart from the
`generated_on` stamp. A customer diffing two releases must never see churn
manufactured by the builder itself; there is a test that enforces it.

## Development

```sh
make lint      # gofmt + go vet
make test      # go test ./...
make validate  # lint the dataset with warnings fatal
make dist      # artefact + static page into dist/
```

The shipped dataset is part of the test suite: `go test ./...` fails if
`data/countries` stops linting, or if the committed example artefact drifts out
of step with the YAML it was built from.

## Not built yet

- **Source-change watcher.** Scheduled fetch of each source URL, content hash,
  alert on change. Detection only, never automated parsing into fields.
- **Webhook delivery.** The `-json` payload is the intended body; delivery,
  per-customer tokens and retries are not written.
- **The remaining 22 countries**, and flipping the eight seed files from
  `draft` to `verified`. That is reading and typing, not engineering.

## Scope

This is a dataset of what the rules are and when they bite. It is deliberately
not: legal advice, invoice validation, format schemas (XSD/Schematron), sample
invoices, or an automated scraper. Every field is entered and checked by a
human.

## License

[MIT](LICENSE). The licence covers both the code and the dataset in
`data/countries/`, including the committed release artefacts in `examples/`.
