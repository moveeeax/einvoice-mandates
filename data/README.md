# Dataset source and schema

One YAML file per country in `countries/`, named after the lower-case ISO 3166-1
alpha-2 code. `einvoice-mandates validate` is the authority on what is legal;
this file explains the intent behind each field.

## Curation rules

1. **Nothing enters the dataset without a citation.** Every mandate carries at
   least one `sources` entry, and the linter warns if none of them is the tax
   authority itself.
2. **Never guess a date.** If a law is on the books but its start is pegged to
   a future event, leave `starts_on` empty, put the trigger in
   `starts_on_rule`, and set the phase status to `awaiting_trigger`. Spain's
   Crea y Crece mandate is the worked example.
3. **A moved deadline keeps its history.** When a phase slips, set
   `previous_starts_on` to the date it moved from instead of overwriting it
   silently. That is what makes the postponement visible in the diff and on the
   rendered table.
4. **`review_status` is the honesty flag.** A country stays `draft` until every
   field has been checked against its cited source by a human. `build` refuses
   to publish draft countries unless `-allow-draft` is passed explicitly.
5. **Re-verify on a cycle.** `verified_on` and `last_reviewed` older than a year
   produce lint warnings, and CI runs `validate -strict`, so a stale citation
   breaks the build.

> The eight seed files currently in `countries/` are all `review_status: draft`.
> They were compiled from the cited official pages to prove the schema survives
> contact with reality, and they have not yet been re-checked field by field.
> Do not treat them as authoritative until that flag flips.

## Country file

| Field | Required | Notes |
| --- | --- | --- |
| `schema_version` | yes | Must match the schema version of the binary (currently `1`). |
| `country` | yes | ISO 3166-1 alpha-2, upper case. |
| `country_name` | yes | English short name. |
| `region` | yes | `EU`, `EEA`, `UK`, `NA`, `LATAM`, `MEA`, `APAC`. |
| `tax_authority` | yes | Name of the body that runs the regime. |
| `review_status` | yes | `draft` or `verified`. |
| `last_reviewed` | yes | `YYYY-MM-DD`. Warns after a year. |
| `mandates` | yes | At least one. |
| `notes` | no | Free text for context a field cannot carry. |

## Mandate

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | Lower-kebab-case, unique within the country. Part of the diff join key, so renaming one reads as a removal plus an addition. |
| `title` | yes | Human-readable name of the programme. |
| `scope` | yes | `b2g`, `b2b`, `b2c`, `e-reporting`. |
| `model` | yes | `clearance`, `centralised-exchange`, `interoperability`, `post-audit`, `real-time-reporting`. |
| `status` | yes | `announced`, `draft_law`, `enacted`, `in_force`, `postponed`, `repealed`. |
| `network` | no | Platform or network, e.g. `SdI`, `KSeF`, `Peppol`. |
| `formats` | yes | At least one syntax, e.g. `FatturaPA`, `UBL 2.1`. |
| `phases` | yes | At least one, in chronological order. |
| `penalty` | no | Describe the regime; do not quote figures you have not re-read. |
| `sources` | yes | At least one. |

## Phase

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | Lower-kebab-case, unique within the mandate. |
| `starts_on` | conditional | `YYYY-MM-DD`. Required unless `starts_on_rule` is set. |
| `starts_on_rule` | conditional | The event that will fix the date. Mutually exclusive with `starts_on`. |
| `previous_starts_on` | no | The date this phase moved from. Must be earlier than `starts_on`. |
| `obligation` | yes | `issue`, `receive`, `report`, `store`. |
| `applies_to` | yes | Prose description of the population pulled in. |
| `threshold` | no | Structured size test, see below. |
| `status` | yes | `in_force`, `planned`, `awaiting_trigger`, `postponed`, `cancelled`. |
| `source_ref` | no | `id` of a source on the same mandate. |

`status` and `starts_on` are cross-checked: a phase cannot be `in_force` with a
future date, and `awaiting_trigger` is only valid on a phase without a date.

## Threshold

| Field | Required | Notes |
| --- | --- | --- |
| `metric` | yes | e.g. `annual_turnover`, `annual_revenue`, `invoice_amount`. |
| `operator` | yes | `gt`, `gte`, `lt`, `lte`, `eq`, `between`. |
| `value` | yes | Lower bound for `between`. |
| `value_max` | conditional | Only with `between`, and must exceed `value`. |
| `currency` | no | ISO 4217, upper case. |
| `period` | no | Reference period the metric is measured over. |

## Source

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | Lower-kebab-case, unique within the mandate. |
| `url` | yes | Absolute `https` URL. |
| `title` | yes | Title of the page or instrument. |
| `publisher` | yes | Who published it. |
| `language` | yes | ISO 639-1, lower case. |
| `verified_on` | yes | Date a human last opened it. Warns after a year. |
| `official` | yes | `true` only for the authority or the legal gazette. |
