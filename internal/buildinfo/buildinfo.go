package buildinfo

import (
	"runtime"
	"runtime/debug"
)

var (
	Version = "dev"
	Commit  = "none"
	BuiltAt = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuiltAt   string `json:"built_at"`
	GoVersion string `json:"go_version"`
	Dirty     bool   `json:"dirty"`
}

func Current() Info {
	result := Info{
		Version:   Version,
		Commit:    Commit,
		BuiltAt:   BuiltAt,
		GoVersion: runtime.Version(),
	}
	details, ok := debug.ReadBuildInfo()
	if !ok {
		return result
	}
	if result.Version == "dev" && details.Main.Version != "" && details.Main.Version != "(devel)" {
		result.Version = details.Main.Version
	}
	for _, setting := range details.Settings {
		switch setting.Key {
		case "vcs.revision":
			if result.Commit == "none" {
				result.Commit = setting.Value
			}
		case "vcs.time":
			if result.BuiltAt == "unknown" {
				result.BuiltAt = setting.Value
			}
		case "vcs.modified":
			result.Dirty = setting.Value == "true"
		}
	}
	return result
}
