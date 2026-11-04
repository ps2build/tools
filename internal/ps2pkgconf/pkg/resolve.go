// Package pkg resolves a pkg-config-style module name against ps2build's
// own installed package.yaml manifests under $PS2DEV/packages, instead of
// real .pc files - a single source of truth (package.yaml already has
// include_dirs/lib) rather than hand-maintaining .pc files as a second,
// separately-driftable copy of the same information.
//
// Schema mirrors ps2build's own internal/pkgmanifest deliberately - this
// tool reads the exact same package.yaml files ps2build itself installs
// and consumes, not a parallel format.
package pkg

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Artifact struct {
	Kind        string   `yaml:"kind"`
	Target      []string `yaml:"target"`
	IncludeDirs []string `yaml:"include_dirs"`
	Lib         string   `yaml:"lib"`
}

type Manifest struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"` // optional; defaults to "0.0.0" if absent

	Artifacts []Artifact `yaml:"artifacts"`

	dir string // absolute, forward-slash - not a YAML field
}

func (m *Manifest) libraryArtifact(target string) (Artifact, bool) {
	for _, a := range m.Artifacts {
		if a.Kind != "library" {
			continue
		}
		for _, t := range a.Target {
			if t == target {
				return a, true
			}
		}
	}
	return Artifact{}, false
}

func load(manifestPath string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", manifestPath, err)
	}
	absPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, err
	}
	m.dir = filepath.ToSlash(filepath.Dir(absPath))
	return &m, nil
}

// find looks up exactly one directory name under packageRoot's core/ and
// world/ subdirectories, matching ps2build's own pkgmanifest.Find order.
func find(packageRoot, name string) (*Manifest, bool, error) {
	for _, tier := range []string{"core", "world"} {
		p := path.Join(packageRoot, tier, name, "package.yaml")
		if _, err := os.Stat(p); err != nil {
			continue
		}
		m, err := load(p)
		if err != nil {
			return nil, false, err
		}
		return m, true, nil
	}
	return nil, false, nil
}

// Find resolves a pkg-config module query (e.g.
func Find(ps2dev, name string) (*Manifest, bool, error) {
	packageRoot := path.Join(filepath.ToSlash(ps2dev), "packages")
	base := trimPrefix(name, "lib")
	if m, ok, err := find(packageRoot, base); err != nil || ok {
		return m, ok, err
	}
	return find(packageRoot, "lib"+base)
}

// CFlags returns "-I<dir>" for each of the library artifact's
// include_dirs, resolved absolute against the manifest's own directory.
func (m *Manifest) CFlags() ([]string, bool) {
	a, ok := m.libraryArtifact("ee")
	if !ok {
		return nil, false
	}
	out := make([]string, len(a.IncludeDirs))
	for i, inc := range a.IncludeDirs {
		out[i] = "-I" + path.Join(m.dir, inc)
	}
	return out, true
}

// LibDir returns "-L<dir>" for the library artifact's archive directory.
func (m *Manifest) LibDir() (string, bool) {
	a, ok := m.libraryArtifact("ee")
	if !ok {
		return "", false
	}
	return "-L" + path.Dir(path.Join(m.dir, a.Lib)), true
}

// LibName returns "-l<name>" derived from the archive's own filename
// (lib<name>.a -> -l<name>), matching the exact convention every ps2build
// package's own -l flag already relies on - never the package's own
// manifest Name field, which is sometimes spelled differently in a
// package's own source tree (see libcglue's cglue-vs-libcglue history).
func (m *Manifest) LibName() (string, bool) {
	a, ok := m.libraryArtifact("ee")
	if !ok {
		return "", false
	}
	base := path.Base(a.Lib)
	base = trimSuffix(base, ".a")
	base = trimPrefix(base, "lib")
	return "-l" + base, true
}

func (m *Manifest) ModVersion() string {
	if m.Version != "" {
		return m.Version
	}
	return "0.0.0"
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
