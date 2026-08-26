package main

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", args, err, output)
	}
}

func TestPlatformArchitectureMapping(t *testing.T) {
	if got := defaultPlatform(); got == "" {
		t.Fatal("default platform is empty")
	}
	if got := defaultArchitecture(); got == "" {
		t.Fatal("default architecture is empty")
	}
}

func TestGitDefaultVersionFromTag(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-m", "init")
	runGit(t, dir, "tag", "v1.4.0")

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)

	var output strings.Builder
	if got := gitDefaultVersion(&output); got != "1.4.0" {
		t.Fatalf("git version = %q, want 1.4.0 (output %q)", got, output.String())
	}
	if !strings.Contains(output.String(), "1.4.0") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestGitDefaultVersionEmptyWithoutTag(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-m", "init")

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)

	if got := gitDefaultVersion(io.Discard); got != "" {
		t.Fatalf("git version = %q, want empty without tag", got)
	}
}

func TestProductPickerModelSelectByEnter(t *testing.T) {
	items := []pickerItem{
		{id: "p1", name: "One", slug: "one", publisher: "acme", latest: "1.0.0"},
		{id: "p2", name: "Two", slug: "two", publisher: "acme", latest: "2.0.0"},
	}
	m := newPickerModel(items, 80)

	// Navigate to second item
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(pickerModel)
	var cmd tea.Cmd
	result, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	final := result.(pickerModel)

	if final.chosen != "p2" {
		t.Fatalf("chosen = %q, want p2", final.chosen)
	}
	_ = cmd
}

func TestProductPickerModelSelectByFirst(t *testing.T) {
	items := []pickerItem{
		{id: "p1", name: "One", slug: "one", publisher: "acme", latest: "1.0.0"},
		{id: "p2", name: "Two", slug: "two", publisher: "acme", latest: "2.0.0"},
	}
	m := newPickerModel(items, 80)

	// First item is selected by default; press Enter
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	final := result.(pickerModel)

	if final.chosen != "p1" {
		t.Fatalf("chosen = %q, want p1", final.chosen)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestProductPickerModelEscCancel(t *testing.T) {
	items := []pickerItem{
		{id: "p1", name: "One", slug: "one", publisher: "acme"},
	}
	m := newPickerModel(items, 80)
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	final := result.(pickerModel)
	if !final.cancelled {
		t.Fatal("expected cancelled = true")
	}
}
