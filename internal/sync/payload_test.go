package sync

import (
	"strings"
	"testing"
)

func TestReleaseDescriptionAppendsArtifactTable(t *testing.T) {
	artifacts := []ArtifactLink{
		{Name: "app-arm64.tar.gz", Platform: "macos", Architecture: "arm64", Size: 12 * 1024 * 1024, URL: "https://downloads.test/artifacts/1/download"},
		{Name: "app-x64.deb", Platform: "linux", Architecture: "x64", Size: 1024, URL: "https://downloads.test/artifacts/2/download"},
	}
	got := releaseDescription("Fixed the updater.\n", artifacts)
	want := strings.Join([]string{
		"Fixed the updater.",
		"",
		"---",
		"",
		"| Artifact | Platform | Architecture | Size | Download |",
		"| --- | --- | --- | --- | --- |",
		"| app-arm64.tar.gz | macos | arm64 | 12.0 MiB | [Download](https://downloads.test/artifacts/1/download) |",
		"| app-x64.deb | linux | x64 | 1.0 KiB | [Download](https://downloads.test/artifacts/2/download) |",
	}, "\n")
	if got != want {
		t.Fatalf("releaseDescription() =\n%s\nwant\n%s", got, want)
	}
}

func TestReleaseDescriptionWithoutNotesOrArtifacts(t *testing.T) {
	// No artifacts: the publisher's notes are returned untouched.
	if got := releaseDescription("Only notes.", nil); got != "Only notes." {
		t.Fatalf("releaseDescription() = %q, want the notes verbatim", got)
	}
	// No notes: no leading divider before the table.
	table := releaseDescription("", []ArtifactLink{{Name: "a.zip", Platform: "windows", Architecture: "x64", Size: 1, URL: "https://downloads.test/a"}})
	if strings.HasPrefix(table, "---") || strings.HasPrefix(table, "\n") {
		t.Fatalf("releaseDescription() = %q, want the table to start the body", table)
	}
	if !strings.HasPrefix(table, "| Artifact |") {
		t.Fatalf("releaseDescription() = %q, want the table header first", table)
	}
	// Nothing at all stays empty rather than emitting an empty table.
	if got := releaseDescription("", nil); got != "" {
		t.Fatalf("releaseDescription() = %q, want empty", got)
	}
	// An artifact without a URL would render a dead link, so it is dropped.
	if got := releaseDescription("notes", []ArtifactLink{{Name: "x", URL: ""}}); got != "notes" {
		t.Fatalf("releaseDescription() = %q, want the notes without a table", got)
	}
}

func TestReleaseDescriptionEscapesTableCells(t *testing.T) {
	got := releaseDescription("", []ArtifactLink{{Name: "a|b\nc", Platform: "linux", Architecture: "", Size: 0, URL: "https://downloads.test/x"}})
	if strings.Contains(got, "| a|b") {
		t.Fatalf("releaseDescription() did not escape the pipe: %s", got)
	}
	if !strings.Contains(got, `\|`) {
		t.Fatalf("releaseDescription() = %s, want an escaped pipe", got)
	}
	if !strings.Contains(got, "| - |") {
		t.Fatalf("releaseDescription() = %s, want a placeholder for the empty architecture", got)
	}
}

func TestHumanSize(t *testing.T) {
	for _, tc := range []struct {
		size int64
		want string
	}{
		{0, "-"},
		{-5, "-"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{2147483648, "2.0 GiB"},
	} {
		if got := humanSize(tc.size); got != tc.want {
			t.Fatalf("humanSize(%d) = %q, want %q", tc.size, got, tc.want)
		}
	}
}
