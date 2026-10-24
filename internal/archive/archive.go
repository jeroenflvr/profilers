// Package archive writes scan entries into a gzip-compressed tar file.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"profilers/internal/scan"
)

type Stats struct {
	Files, Dirs, Links int
	Bytes              int64
}

// Write creates path (mode 0600, refusing to overwrite) and streams entries into it.
// Files that cannot be opened are reported via warn and skipped.
func Write(path string, entries []scan.Entry, warn func(string, ...any)) (Stats, error) {
	var st Stats
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return st, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return st, err
	}
	ok := false
	defer func() {
		if !ok {
			os.Remove(path)
		}
	}()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		if err := writeEntry(tw, e, &st, warn); err != nil {
			return st, fmt.Errorf("%s: %w", e.Name, err)
		}
	}

	if err := tw.Close(); err != nil {
		return st, err
	}
	if err := gz.Close(); err != nil {
		return st, err
	}
	if err := f.Close(); err != nil {
		return st, err
	}
	ok = true
	return st, nil
}

func writeEntry(tw *tar.Writer, e scan.Entry, st *Stats, warn func(string, ...any)) error {
	hdr, err := tar.FileInfoHeader(e.Info, e.Link)
	if err != nil {
		return err
	}
	hdr.Name = e.Name
	hdr.Format = tar.FormatPAX

	switch {
	case e.IsDir():
		hdr.Name += "/"
		st.Dirs++
		return tw.WriteHeader(hdr)
	case e.IsSymlink():
		st.Links++
		return tw.WriteHeader(hdr)
	}

	src, err := os.Open(e.Source)
	if err != nil {
		warn("skip %s: %v", e.Name, err)
		return nil
	}
	defer src.Close()

	// Re-stat the open handle so the header size matches what we copy even
	// if the file changed since scanning.
	cur, err := src.Stat()
	if err != nil {
		return err
	}
	hdr.Size = cur.Size()
	hdr.ModTime = cur.ModTime()

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	n, err := io.CopyN(tw, src, hdr.Size)
	if err != nil {
		return fmt.Errorf("copy after %d bytes: %w", n, err)
	}
	st.Files++
	st.Bytes += n
	return nil
}
