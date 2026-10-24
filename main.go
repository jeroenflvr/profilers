// Command profilers collects dotfiles, credential directories and project
// secret/config files into a tar.gz archive laid out relative to $HOME.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"profilers/internal/archive"
	"profilers/internal/config"
	"profilers/internal/scan"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "profilers:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		cfgPath = flag.String("c", "", "config file (default: $PROFILERS_CONFIG, ./config.toml, ~/.config/profilers/config.toml)")
		outPath = flag.String("o", "", "output archive path (overrides output.dir/output.name)")
		dryRun  = flag.Bool("dry-run", false, "list what would be archived without writing anything")
		verbose = flag.Bool("v", false, "print each file as it is archived")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: profilers [-c config.toml] [-o archive.tgz] [--dry-run] [-v]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	home, err := config.HomeDir()
	if err != nil {
		return err
	}
	path, err := findConfig(*cfgPath, home)
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	warn := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "warn: "+format+"\n", args...)
	}

	entries := scan.New(cfg, home, warn).Run()

	dest := *outPath
	if dest == "" {
		dest = filepath.Join(config.ExpandHome(cfg.Output.Dir, home), expandName(cfg.Output.Name))
	} else {
		dest = config.ExpandHome(dest, home)
	}

	if *dryRun || *verbose {
		for _, e := range entries {
			switch {
			case e.IsDir():
				continue
			case e.IsSymlink():
				fmt.Printf("%10s  %s -> %s\n", "link", e.Name, e.Link)
			default:
				fmt.Printf("%10s  %s\n", human(e.Info.Size()), e.Name)
			}
		}
	}

	if *dryRun {
		var st archive.Stats
		for _, e := range entries {
			switch {
			case e.IsDir():
				st.Dirs++
			case e.IsSymlink():
				st.Links++
			default:
				st.Files++
				st.Bytes += e.Info.Size()
			}
		}
		fmt.Printf("\n%s\nwould write: %s\n", summary(st), dest)
		return nil
	}

	st, err := archive.Write(dest, entries, warn)
	if err != nil {
		return err
	}
	fi, err := os.Stat(dest)
	if err != nil {
		return err
	}
	fmt.Printf("%s\nwrote %s (%s)\n", summary(st), dest, human(fi.Size()))
	return nil
}

func findConfig(explicit, home string) (string, error) {
	candidates := []string{explicit, os.Getenv("PROFILERS_CONFIG"), "config.toml",
		filepath.Join(home, ".config", "profilers", "config.toml")}
	if explicit != "" {
		candidates = candidates[:1]
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("no config found (tried: %s)", strings.Join(nonEmpty(candidates), ", "))
}

func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func expandName(tmpl string) string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	host, _, _ = strings.Cut(host, ".")
	r := strings.NewReplacer(
		"{hostname}", host,
		"{timestamp}", time.Now().Format("20060102-150405"),
	)
	return r.Replace(tmpl)
}

func summary(st archive.Stats) string {
	return fmt.Sprintf("%d files, %d dirs, %d symlinks, %s", st.Files, st.Dirs, st.Links, human(st.Bytes))
}

func human(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
