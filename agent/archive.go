package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxZipFiles    = 20000
	maxZipUnpacked = 2 << 30 // 2 GB
)

// extractZip unpacks a build context into dest. Only regular files and
// directories are allowed: no absolute paths, no "..", no symlinks, so the
// archive can't write outside dest. If root is set ("my-app/"), that
// folder's contents are extracted instead of the whole archive.
func extractZip(zipPath, root, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("the build context is not a valid ZIP: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > maxZipFiles {
		return fmt.Errorf("the ZIP has too many files")
	}

	var total uint64
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if root != "" {
			if !strings.HasPrefix(name, root) {
				continue
			}
			name = strings.TrimPrefix(name, root)
		}
		if name == "" || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		target, err := safeJoin(dest, name)
		if err != nil {
			return err
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		case !mode.IsRegular():
			return fmt.Errorf("the ZIP contains %s, which is not a regular file", f.Name)
		}
		total += f.UncompressedSize64
		if total > maxZipUnpacked {
			return errors.New("the ZIP unpacks to more than 2 GB")
		}
		if err := extractFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	perm := os.FileMode(0o644)
	if f.Mode()&0o111 != 0 {
		perm = 0o755
	}
	// Remove first: an existing symlink at this path must not be followed.
	os.Remove(target)
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer out.Close()
	// The declared size can lie; stop at the limit.
	_, err = io.Copy(out, io.LimitReader(rc, maxZipUnpacked))
	return err
}

// safeJoin joins a relative archive path onto dir, refusing anything that
// would land outside it.
func safeJoin(dir, name string) (string, error) {
	clean := path.Clean("/" + name)[1:]
	if clean == "" || strings.Contains(name, "\x00") || path.IsAbs(name) || strings.HasPrefix(path.Clean(name), "..") {
		return "", fmt.Errorf("unsafe path in archive: %q", name)
	}
	target := filepath.Join(dir, filepath.FromSlash(clean))
	if !isWithin(dir, target) {
		return "", fmt.Errorf("unsafe path in archive: %q", name)
	}
	return target, nil
}

// isWithin reports whether path is dir or inside it.
func isWithin(dir, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../") && !filepath.IsAbs(rel)
}

// copyTree copies regular files from src into dst, overwriting files that
// exist and leaving other files in dst alone (so data a container wrote
// into the project folder survives a redeploy). Symlinks are skipped.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case !d.Type().IsRegular():
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		info, _ := in.Stat()
		os.Remove(target)
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
