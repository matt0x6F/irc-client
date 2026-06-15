package main

// Build metadata. These defaults are overwritten at link time via
//
//	-ldflags "-X main.version=... -X main.commit=... -X main.buildDate=..."
//
// (see the build task in Taskfile.yml and the release workflow). The "dev"/
// "none"/"unknown" defaults mean `wails dev` and a flag-less `wails build`
// still produce a sensible, self-labelling dev build.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// BuildInfo is the build metadata exposed to the frontend. buildDate is a
// string (ISO-8601), never time.Time: a time.Time in a Wails-bound signature
// breaks the generated TypeScript model.
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

// GetBuildInfo returns the version, commit, and build date stamped into the
// binary at link time.
func (a *App) GetBuildInfo() BuildInfo {
	return BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	}
}
