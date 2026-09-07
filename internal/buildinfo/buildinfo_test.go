package buildinfo

import (
	"strings"
	"testing"
)

func TestCurrent(t *testing.T) {
	result := Current()
	if result.Version == "" || result.Commit == "" || result.BuiltAt == "" {
		t.Fatalf("info = %+v", result)
	}
	if !strings.HasPrefix(result.GoVersion, "go") {
		t.Fatalf("go version = %q", result.GoVersion)
	}
}
