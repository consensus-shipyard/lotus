package tests

import (
	"bufio"
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

func ReadFileLineByLine(filePath string) ([]string, error) {
	var lines []string

	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close() // nolint

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		l := scanner.Text()
		lines = append(lines, l)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
