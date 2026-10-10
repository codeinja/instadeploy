package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
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

// manifestName records, in a project folder, the files the last deploy copied from the
// source, so the next deploy can tell files removed from the repository apart from files
// a container wrote.
const manifestName = ".insta-deploy-files"

// syncTree copies src over dst like copyTree, then deletes the files the previous sync
// copied that src no longer has (a file deleted from the repository must not linger and,
// say, break the build). Files dst has for any other reason, such as data containers
// wrote through relative bind mounts, are left alone. The first sync of a folder has no
// manifest, so it deletes nothing.
func syncTree(src, dst string) error {
	previous, err := readManifest(dst)
	if err != nil {
		return err
	}
	current, err := listFiles(src)
	if err != nil {
		return err
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	now := make(map[string]bool, len(current))
	for _, f := range current {
		now[f] = true
	}
	for _, f := range previous {
		if now[f] {
			continue
		}
		target, err := safeJoin(dst, f)
		if err != nil {
			continue // a tampered manifest never reaches outside the project
		}
		if info, err := os.Lstat(target); err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(target); err != nil {
			return err
		}
		removeEmptyParents(dst, filepath.Dir(target))
	}
	return writeManifest(dst, current)
}

// listFiles returns the regular files under dir, relative and slash-separated.
func listFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(dir, p)
			if rel != manifestName {
				out = append(out, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func readManifest(dir string) ([]string, error) {
	f, err := os.Open(filepath.Join(dir, manifestName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			out = append(out, line)
		}
	}
	return out, sc.Err()
}

func writeManifest(dir string, files []string) error {
	tmp := filepath.Join(dir, manifestName+".tmp")
	if err := os.WriteFile(tmp, []byte(strings.Join(files, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, manifestName))
}

// removeEmptyParents removes dir and its empty ancestors, stopping at root.
func removeEmptyParents(root, dir string) {
	for isWithin(root, dir) && filepath.Clean(dir) != filepath.Clean(root) {
		if os.Remove(dir) != nil { // fails when not empty
			return
		}
		dir = filepath.Dir(dir)
	}
}
