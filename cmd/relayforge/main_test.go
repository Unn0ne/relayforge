package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Unn0ne/relayforge/internal/buildinfo"
)

func TestWriteVersion(t *testing.T) {
	var output bytes.Buffer
	if err := writeVersion(&output); err != nil {
		t.Fatal(err)
	}
	var result buildinfo.Info
	if err := json.NewDecoder(&output).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Version == "" || result.Commit == "" || result.GoVersion == "" {
		t.Fatalf("version = %+v", result)
	}
}
