package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// safeJoin joins a relative path (from a deployment spec) onto dir, refusing anything that
// would land outside it.
func safeJoin(dir, name string) (string, error) {
	clean := path.Clean("/" + name)[1:]
	if clean == "" || strings.Contains(name, "\x00") || path.IsAbs(name) || strings.HasPrefix(path.Clean(name), "..") {
		return "", fmt.Errorf("unsafe path: %q", name)
	}
	target := filepath.Join(dir, filepath.FromSlash(clean))
	if !isWithin(dir, target) {
		return "", fmt.Errorf("unsafe path: %q", name)
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
