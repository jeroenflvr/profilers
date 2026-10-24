// Package scan discovers files to archive according to a config.Config.
package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"profilers/internal/config"
)

// Entry is one item destined for the archive.
type Entry struct {
	Source string      // path on disk to read from (symlink target when following)
	Name   string      // path inside the archive, relative to $HOME
	Info   fs.FileInfo // Stat (followed) or Lstat (unfollowed symlink) result
	Link   string      // symlink target, only when Info is a symlink
}

func (e Entry) IsDir() bool     { return e.Info.IsDir() }
func (e Entry) IsSymlink() bool { return e.Info.Mode()&fs.ModeSymlink != 0 }

type Scanner struct {
	cfg         config.Config
	home        string
	warn        func(format string, args ...any)
	excludeDirs map[string]bool
	seen        map[string]bool // archive names already queued
	visited     map[string]bool // resolved directory paths already walked
	entries     []Entry
}

func New(cfg config.Config, home string, warn func(string, ...any)) *Scanner {
	s := &Scanner{
		cfg:         cfg,
		home:        home,
		warn:        warn,
		excludeDirs: map[string]bool{},
		seen:        map[string]bool{},
		visited:     map[string]bool{},
	}
	for _, d := range cfg.Patterns.ExcludeDirs {
		s.excludeDirs[d] = true
	}
	return s
}

// Run performs all configured scans and returns entries sorted by archive name.
func (s *Scanner) Run() []Entry {
	if s.cfg.Home.Dotfiles {
		s.homeDotfiles()
	}
	for _, d := range s.cfg.Home.DotDirs {
		s.dotDir(d)
	}
	for _, sc := range s.cfg.Scan {
		s.scanDir(sc)
	}
	for _, app := range s.cfg.MacOS.AppSupport {
		s.fullTree(filepath.Join(s.home, "Library", "Application Support", app), nil)
	}
	sort.Slice(s.entries, func(i, j int) bool { return s.entries[i].Name < s.entries[j].Name })
	return s.entries
}

type walkOpts struct {
	root        string
	maxDepth    int             // -1 = unlimited
	excludeRel  map[string]bool // paths relative to root to prune
	match       func(name string) bool
	includeDirs bool
}

func (s *Scanner) homeDotfiles() {
	ents, err := os.ReadDir(s.home)
	if err != nil {
		s.warn("read %s: %v", s.home, err)
		return
	}
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), ".") || e.IsDir() || s.matchAny(s.cfg.Home.DotfileExcludes, e.Name()) {
			continue
		}
		full := filepath.Join(s.home, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			s.addSymlink(full, func(string) bool { return true })
			continue
		}
		if e.Type().IsRegular() {
			s.addRegular(full)
		}
	}
}

func (s *Scanner) dotDir(name string) {
	root := filepath.Join(s.home, name)
	excl := map[string]bool{}
	for _, rel := range s.cfg.Home.DotDirExcludes[name] {
		excl[filepath.Clean(rel)] = true
	}
	s.fullTree(root, excl)
}

// fullTree archives an entire directory, if it exists, including directory
// entries so permissions (e.g. 0700 on ~/.ssh) are preserved.
func (s *Scanner) fullTree(root string, excludeRel map[string]bool) {
	info, err := os.Stat(root)
	if err != nil {
		if !os.IsNotExist(err) {
			s.warn("stat %s: %v", root, err)
		}
		return
	}
	if !info.IsDir() {
		s.addRegular(root)
		return
	}
	s.addDir(root, info)
	s.walk(root, 0, walkOpts{root: root, maxDepth: -1, excludeRel: excludeRel, includeDirs: true})
}

func (s *Scanner) scanDir(sc config.Scan) {
	root := config.ExpandHome(sc.Path, s.home)
	if _, err := os.Stat(root); err != nil {
		s.warn("scan %s: %v", sc.Path, err)
		return
	}
	s.walk(root, 0, walkOpts{root: root, maxDepth: sc.Depth, match: s.matchPattern})
}

func (s *Scanner) matchPattern(name string) bool {
	return s.matchAny(s.cfg.Patterns.Include, name)
}

func (s *Scanner) matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

func (s *Scanner) walk(dir string, depth int, o walkOpts) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		s.warn("resolve %s: %v", dir, err)
		return
	}
	if s.visited[real] {
		return
	}
	s.visited[real] = true

	ents, err := os.ReadDir(dir)
	if err != nil {
		s.warn("read %s: %v", dir, err)
		return
	}
	for _, e := range ents {
		name := e.Name()
		full := filepath.Join(dir, name)
		typ := e.Type()

		// Prune by name before touching the entry: excluded dirs may be slow
		// virtual mounts (e.g. ~/OrbStack) or TCC-protected (~/.Trash).
		if s.excludedDir(full, name, o) && (typ.IsDir() || typ&fs.ModeSymlink != 0) {
			continue
		}

		switch {
		case typ&fs.ModeSymlink != 0:
			if !s.cfg.Output.FollowSymlinks {
				s.addSymlink(full, o.match)
				continue
			}
			st, err := os.Stat(full)
			if err != nil {
				s.warn("follow %s: %v", full, err)
				continue
			}
			if st.IsDir() {
				s.descend(full, depth, st, o)
			} else if st.Mode().IsRegular() && (o.match == nil || o.match(name)) {
				target, _ := filepath.EvalSymlinks(full)
				s.add(Entry{Source: target, Name: s.archiveName(full), Info: st})
			}
		case typ.IsDir():
			info, err := e.Info()
			if err != nil {
				s.warn("stat %s: %v", full, err)
				continue
			}
			s.descend(full, depth, info, o)
		case typ.IsRegular():
			if o.match == nil || o.match(name) {
				s.addRegular(full)
			}
		}
	}
}

func (s *Scanner) excludedDir(full, name string, o walkOpts) bool {
	rel, _ := filepath.Rel(o.root, full)
	return s.excludeDirs[name] || o.excludeRel[rel]
}

func (s *Scanner) descend(full string, depth int, info fs.FileInfo, o walkOpts) {
	if o.includeDirs {
		s.addDir(full, info)
	}
	if o.maxDepth < 0 || depth < o.maxDepth {
		s.walk(full, depth+1, o)
	}
}

func (s *Scanner) addRegular(full string) {
	info, err := os.Lstat(full)
	if err != nil {
		s.warn("stat %s: %v", full, err)
		return
	}
	if !info.Mode().IsRegular() {
		return
	}
	s.add(Entry{Source: full, Name: s.archiveName(full), Info: info})
}

// addSymlink adds a symlink either as a link entry or, when following, as
// the resolved regular file. Directory targets are ignored here.
func (s *Scanner) addSymlink(full string, match func(string) bool) {
	if !s.cfg.Output.FollowSymlinks {
		info, err := os.Lstat(full)
		if err != nil {
			s.warn("stat %s: %v", full, err)
			return
		}
		target, err := os.Readlink(full)
		if err != nil {
			s.warn("readlink %s: %v", full, err)
			return
		}
		if match == nil || match(filepath.Base(full)) {
			s.add(Entry{Source: full, Name: s.archiveName(full), Info: info, Link: target})
		}
		return
	}
	st, err := os.Stat(full)
	if err != nil {
		s.warn("follow %s: %v", full, err)
		return
	}
	if !st.Mode().IsRegular() {
		return
	}
	if match == nil || match(filepath.Base(full)) {
		target, _ := filepath.EvalSymlinks(full)
		s.add(Entry{Source: target, Name: s.archiveName(full), Info: st})
	}
}

func (s *Scanner) addDir(full string, info fs.FileInfo) {
	s.add(Entry{Source: full, Name: s.archiveName(full), Info: info})
}

func (s *Scanner) add(e Entry) {
	if e.Name == "" || s.seen[e.Name] {
		return
	}
	s.seen[e.Name] = true
	s.entries = append(s.entries, e)
}

// archiveName maps a disk path to its archive path: relative to $HOME when
// inside it, otherwise the absolute path without the leading slash.
func (s *Scanner) archiveName(full string) string {
	if rel, err := filepath.Rel(s.home, full); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return strings.TrimPrefix(filepath.ToSlash(full), "/")
}
