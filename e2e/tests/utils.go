package tests

import (
	"bufio"
	"os"
	"strconv"
)

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

func NodeIDS(n int) []string {
	var ids []string
	for i := 0; i < n; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	return ids
}
