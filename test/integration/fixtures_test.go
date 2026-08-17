package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()

	pathParts := append([]string{"testdata"}, parts...)
	data, err := os.ReadFile(filepath.Join(pathParts...))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func readTextFixture(t *testing.T, parts ...string) string {
	t.Helper()
	return strings.TrimSuffix(string(readFixture(t, parts...)), "\n")
}
