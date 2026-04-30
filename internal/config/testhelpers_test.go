package config_test

import (
	"os"
	"strings"
)

func writeFile(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o600)
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
