package main

import "testing"

// GetBuildInfo should surface the package-level build vars verbatim. In the
// test environment no ldflags are applied, so it returns the compiled-in
// defaults.
func TestGetBuildInfo(t *testing.T) {
	app := &App{}
	got := app.GetBuildInfo()

	if got.Version != version {
		t.Errorf("Version = %q, want %q", got.Version, version)
	}
	if got.Commit != commit {
		t.Errorf("Commit = %q, want %q", got.Commit, commit)
	}
	if got.BuildDate != buildDate {
		t.Errorf("BuildDate = %q, want %q", got.BuildDate, buildDate)
	}
}
