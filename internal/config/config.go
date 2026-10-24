package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Output   Output   `toml:"output"`
	Home     Home     `toml:"home"`
	Scan     []Scan   `toml:"scan"`
	Patterns Patterns `toml:"patterns"`
	MacOS    MacOS    `toml:"macos"`
}

type Output struct {
	Dir            string `toml:"dir"`
	Name           string `toml:"name"`
	FollowSymlinks bool   `toml:"follow_symlinks"`
}

type Home struct {
	Dotfiles        bool                `toml:"dotfiles"`
	DotfileExcludes []string            `toml:"dotfile_excludes"`
	DotDirs         []string            `toml:"dot_dirs"`
	DotDirExcludes  map[string][]string `toml:"dot_dir_excludes"`
}

type Scan struct {
	Path  string `toml:"path"`
	Depth int    `toml:"depth"`
}

type Patterns struct {
	Include     []string `toml:"include"`
	ExcludeDirs []string `toml:"exclude_dirs"`
}

type MacOS struct {
	AppSupport []string `toml:"app_support"`
}

func Default() Config {
	return Config{
		Output: Output{Dir: "~/Backups", Name: "profilers-{hostname}-{timestamp}.tgz", FollowSymlinks: true},
		Home:   Home{Dotfiles: true},
	}
}

// Load parses the TOML file at path on top of Default().
func Load(path string) (Config, error) {
	cfg := Default()
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return cfg, fmt.Errorf("%s: unknown keys: %s", path, strings.Join(keys, ", "))
	}
	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.Output.Dir == "" || c.Output.Name == "" {
		return fmt.Errorf("output.dir and output.name are required")
	}
	for i, s := range c.Scan {
		if s.Path == "" {
			return fmt.Errorf("scan[%d].path is required", i)
		}
		if s.Depth < 0 {
			return fmt.Errorf("scan[%d].depth must be >= 0", i)
		}
	}
	for _, p := range c.Patterns.Include {
		if _, err := filepath.Match(p, ""); err != nil {
			return fmt.Errorf("patterns.include %q: %w", p, err)
		}
	}
	for _, p := range c.Home.DotfileExcludes {
		if _, err := filepath.Match(p, ""); err != nil {
			return fmt.Errorf("home.dotfile_excludes %q: %w", p, err)
		}
	}
	return nil
}

// ExpandHome replaces a leading "~" with home and cleans the path.
func ExpandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return filepath.Clean(p)
}

func HomeDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Clean(h), nil
}
