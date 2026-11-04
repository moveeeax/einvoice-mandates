// Package dataset loads the hand-curated YAML source tree from disk.
package dataset

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/moveeeax/einvoice-mandates/internal/model"
)

// File pairs a parsed country with the path it came from, so that every
// downstream error can point at a file a human can open.
type File struct {
	Path    string
	Country model.Country
}

// Load reads every *.yaml file directly under dir and returns them sorted by
// country code. Parse errors are returned for the first offending file.
func Load(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read data dir: %w", err)
	}

	var files []File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		c, err := LoadFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: path, Country: c})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no YAML country files found in %s", dir)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Country.Code < files[j].Country.Code
	})
	return files, nil
}

// LoadFile parses a single country YAML file. Unknown fields are rejected:
// a typo in a key is a data error, not something to silently ignore.
func LoadFile(path string) (model.Country, error) {
	var c model.Country
	f, err := os.Open(path)
	if err != nil {
		return c, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}
