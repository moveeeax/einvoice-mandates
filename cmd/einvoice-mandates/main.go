// Command einvoice-mandates lints, builds, diffs and renders the e-invoicing
// mandate dataset.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/moveeeax/einvoice-mandates/internal/build"
	"github.com/moveeeax/einvoice-mandates/internal/dataset"
	"github.com/moveeeax/einvoice-mandates/internal/diff"
	"github.com/moveeeax/einvoice-mandates/internal/model"
	"github.com/moveeeax/einvoice-mandates/internal/render"
	"github.com/moveeeax/einvoice-mandates/internal/validate"
)

// version is overridden at release time with -ldflags "-X main.version=v4".
var version = "dev"

const usage = `einvoice-mandates — versioned e-invoicing mandate dataset

Usage:
  einvoice-mandates validate [-data DIR] [-json] [-strict]
  einvoice-mandates build    -release TAG [-data DIR] [-o FILE] [-allow-draft]
  einvoice-mandates diff     OLD.json NEW.json [-min LEVEL] [-country CODES] [-json] [-exit-code]
  einvoice-mandates render   RELEASE.json [-o FILE]
  einvoice-mandates version

Commands:
  validate  lint the YAML country tree against the schema
  build     compile the YAML tree into a single release JSON artefact
  diff      report what changed between two release artefacts
  render    write the static public HTML table for a release
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var ec exitCode
		if errors.As(err, &ec) {
			os.Exit(int(ec))
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// exitCode lets a command choose a specific status without printing twice.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitCode(2)
	}
	switch args[0] {
	case "validate":
		return cmdValidate(args[1:], stdout, stderr)
	case "build":
		return cmdBuild(args[1:], stdout, stderr)
	case "diff":
		return cmdDiff(args[1:], stdout, stderr)
	case "render":
		return cmdRender(args[1:], stdout, stderr)
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "einvoice-mandates %s (schema v%d)\n", version, model.SchemaVersion)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return exitCode(2)
	}
}

// parsePermuted parses flags that may appear before, after or between
// positional arguments. The stdlib flag package stops at the first
// non-flag, which makes "diff a.json b.json -json" silently ignore -json.
func parsePermuted(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func cmdValidate(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("data", "data/countries", "directory of country YAML files")
	asJSON := fs.Bool("json", false, "emit findings as JSON")
	strict := fs.Bool("strict", false, "treat warnings as failures")
	if err := fs.Parse(args); err != nil {
		return exitCode(2)
	}

	files, err := dataset.Load(*dir)
	if err != nil {
		return err
	}
	report := validate.Run(files, validate.Options{})

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		for _, f := range report.Findings {
			fmt.Fprintln(stdout, f)
		}
		fmt.Fprintf(stdout, "%d file(s) checked: %d error(s), %d warning(s)\n",
			len(files), report.Errors(), report.Warnings())
	}

	if report.Errors() > 0 || (*strict && report.Warnings() > 0) {
		return exitCode(1)
	}
	return nil
}

func cmdBuild(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("data", "data/countries", "directory of country YAML files")
	release := fs.String("release", "", "release tag for the artefact, e.g. v4 (required)")
	out := fs.String("o", "-", "output file, or - for stdout")
	allowDraft := fs.Bool("allow-draft", false, "publish countries still marked review_status: draft")
	skipLint := fs.Bool("skip-validate", false, "build even if the dataset does not lint")
	if err := fs.Parse(args); err != nil {
		return exitCode(2)
	}
	if *release == "" {
		fmt.Fprintln(stderr, "error: -release is required")
		return exitCode(2)
	}

	files, err := dataset.Load(*dir)
	if err != nil {
		return err
	}
	if !*skipLint {
		report := validate.Run(files, validate.Options{})
		if n := report.Errors(); n > 0 {
			for _, f := range report.Findings {
				if f.Severity == validate.Error {
					fmt.Fprintln(stderr, f)
				}
			}
			return fmt.Errorf("dataset does not validate: %d error(s)", n)
		}
	}

	rel, err := build.Build(files, build.Options{Release: *release, AllowDraft: *allowDraft})
	if err != nil {
		return err
	}
	b, err := build.Marshal(rel)
	if err != nil {
		return err
	}
	if err := writeOut(*out, b, stdout); err != nil {
		return err
	}
	if *out != "-" {
		fmt.Fprintf(stderr, "wrote %s: %d countries, %d mandates, %d phases\n",
			*out, rel.Counts.Countries, rel.Counts.Mandates, rel.Counts.Phases)
	}
	return nil
}

func cmdDiff(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	min := fs.String("min", "", "minimum significance to report: editorial|informational|material")
	countries := fs.String("country", "", "comma-separated alpha-2 codes to restrict output to")
	asJSON := fs.Bool("json", false, "emit the structured diff as JSON (the webhook payload shape)")
	exitOnChange := fs.Bool("exit-code", false, "exit 1 when there is at least one reported change")
	pos, err := parsePermuted(fs, args)
	if err != nil {
		return exitCode(2)
	}
	if len(pos) != 2 {
		fmt.Fprintln(stderr, "error: diff needs exactly two release artefacts: OLD.json NEW.json")
		return exitCode(2)
	}

	oldRel, err := readRelease(pos[0])
	if err != nil {
		return err
	}
	newRel, err := readRelease(pos[1])
	if err != nil {
		return err
	}

	if *min != "" {
		switch diff.Significance(*min) {
		case diff.Editorial, diff.Informational, diff.Material:
		default:
			return fmt.Errorf("invalid -min %q: want editorial, informational or material", *min)
		}
	}

	var codes []string
	if *countries != "" {
		codes = strings.Split(*countries, ",")
	}
	result := diff.Compare(oldRel, newRel).Apply(diff.Filter{
		MinSignificance: diff.Significance(*min),
		Countries:       codes,
	})

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	} else if err := diff.RenderText(stdout, result); err != nil {
		return err
	}

	if *exitOnChange && len(result.Changes) > 0 {
		return exitCode(1)
	}
	return nil
}

func cmdRender(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "-", "output HTML file, or - for stdout")
	pos, err := parsePermuted(fs, args)
	if err != nil {
		return exitCode(2)
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "error: render needs exactly one release artefact")
		return exitCode(2)
	}
	rel, err := readRelease(pos[0])
	if err != nil {
		return err
	}

	var buf strings.Builder
	if err := render.HTML(&buf, rel); err != nil {
		return err
	}
	if err := writeOut(*out, []byte(buf.String()), stdout); err != nil {
		return err
	}
	if *out != "-" {
		fmt.Fprintf(stderr, "wrote %s: %d rows\n", *out, len(render.Flatten(rel)))
	}
	return nil
}

func readRelease(path string) (build.Release, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return build.Release{}, fmt.Errorf("read %s: %w", path, err)
	}
	rel, err := build.Unmarshal(b)
	if err != nil {
		return build.Release{}, fmt.Errorf("%s: %w", path, err)
	}
	return rel, nil
}

func writeOut(path string, b []byte, stdout io.Writer) error {
	if path == "-" {
		_, err := stdout.Write(b)
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
