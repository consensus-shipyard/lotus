package fs

import (
	"errors"
	"os"
	"path"
	"path/filepath"
)

func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for dir != "" {
		info, err := os.Stat(path.Join(dir, "go.mod"))
		if err == nil && info != nil && !info.IsDir() {
			return dir, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		dir = filepath.Dir(dir)
	}

	return dir, nil
}

func CopyFile(src string, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	err = os.WriteFile(dst, data, 0644)
	if err != nil {
		return err
	}
	return nil
}
