package fsx

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type Files struct{}

func New() *Files { return &Files{} }

func (Files) Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (Files) MkdirAll(path string) error { return os.MkdirAll(path, 0o755) }

func (Files) CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm())
}

func (Files) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (Files) WriteFile(path string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (Files) RemoveAll(path string) error { return os.RemoveAll(path) }

func (Files) Symlink(target, link string) error {
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, link)
}

func (Files) ListDirs(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func (Files) ListFiles(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func (Files) Mode(path string) (fs.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	return info.Mode(), nil
}

func (Files) RealPath(path string) (string, error) { return filepath.EvalSymlinks(path) }

func (Files) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

func (Files) Chmod(path string, perm fs.FileMode) error { return os.Chmod(path, perm) }

func (Files) TempFile(pattern string) (string, error) {
	f, err := os.CreateTemp(os.TempDir(), pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	return name, f.Close()
}

// Size sums the files under path, or is the size of path if it is a file. It
// does not follow links, so a link out of the tree is not counted as its target,
// and what it cannot read counts as nothing: the number is for deciding what is
// worth deleting, not for accounting.
func (Files) Size(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total, nil
}
