package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"src.solsynth.dev/solsynth/distribution/internal/marketplace"
)

// defaultPlatform maps the build host OS to the release artifact platform
// vocabulary used by DistributionCenter.
func defaultPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return runtime.GOOS
	}
}

func defaultArchitecture() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "arm64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}

// gitDefaultVersion returns the current git tag for the checkout, with a
// leading "v" stripped. Empty when git is unavailable, there is no tag, or
// the checkout is dirty enough that no exact tag matches.
func gitDefaultVersion(out io.Writer) string {
	command := exec.Command("git", "describe", "--tags", "--exact-match")
	output, err := command.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok && strings.TrimSpace(string(output)) != "" {
			_ = statusLine(out, ansiYellow, fmt.Sprintf("No git tag on this commit; pass --version (%s)", strings.TrimSpace(string(output))))
		}
		return ""
	}
	tag := strings.TrimSpace(string(output))
	tag = strings.TrimPrefix(tag, "v")
	if tag == "" {
		return ""
	}
	_ = statusLine(out, ansiCyan, fmt.Sprintf("Using git tag as version: %s", tag))
	return tag
}

// --- pickerItem implements list.Item (FilterValue) and list.DefaultItem ---

type pickerItem struct {
	id        string
	name      string
	slug      string
	publisher string
	latest    string
}

func (i pickerItem) Title() string       { return i.name }
func (i pickerItem) Description() string { return fmt.Sprintf("%s · %s", i.slug, i.publisher) }
func (i pickerItem) FilterValue() string { return i.name + " " + i.slug + " " + i.publisher }

// --- pickerModel is the Bubble Tea model for the product picker ---

type pickerModel struct {
	list      list.Model
	chosen    string
	cancelled bool
}

func newPickerModel(items []pickerItem, width int) pickerModel {
	delegate := list.NewDefaultDelegate()
	delegate.SetHeight(2)
	delegate.SetSpacing(0)

	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = item
	}

	l := list.New(listItems, delegate, width, 20)
	l.Title = "Select a product"
	l.FilterInput = textinput.New()
	l.FilterInput.Prompt = "/ "
	l.InfiniteScrolling = true
	l.SetFilteringEnabled(true)

	return pickerModel{list: l}
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height-4)
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			if m.list.FilterState() == list.Filtering {
				break
			}
			if item, ok := m.list.SelectedItem().(pickerItem); ok {
				m.chosen = item.id
				return m, tea.Quit
			}
		case tea.KeyEsc, tea.KeyCtrlC:
			m.cancelled = true
			return m, tea.Quit
		}
	case tea.FocusMsg:
		m.list, _ = m.list.Update(msg)
	case tea.BlurMsg:
		m.list, _ = m.list.Update(msg)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m pickerModel) View() string {
	return m.list.View()
}

// pickProduct runs the interactive bubbletea picker.
func pickProduct(ctx context.Context, client *marketplace.Client, stderr io.Writer) (string, error) {
	raw, err := client.ListMarketplaceApps(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("list products for selection: %w", err)
	}
	var response appsListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode product list: %w", err)
	}
	if len(response.Data) == 0 {
		return "", errors.New("no products found in the marketplace")
	}

	items := make([]pickerItem, len(response.Data))
	for i, app := range response.Data {
		items[i] = pickerItem{
			id:        app.Product.ID,
			name:      app.Product.Name,
			slug:      app.Product.Slug,
			publisher: publisherName(app.Publisher),
			latest:    app.Latest.Version,
		}
	}

	m := newPickerModel(items, 80)
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(stderr))
	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("run picker: %w", err)
	}
	final := result.(pickerModel)
	if final.cancelled {
		return "", errors.New("selection cancelled")
	}
	if final.chosen == "" {
		return "", errors.New("no product selected")
	}
	return final.chosen, nil
}

// resolveProductValue resolves a required value, prompting interactively;
// for product references it falls back to listing the marketplace for pick.
func resolveProductValue(ctx context.Context, client *marketplace.Client, stderr io.Writer, value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return value, nil
	}
	if !interactive(stderr) {
		return "", errors.New("--product is required")
	}
	return pickProduct(ctx, client, stderr)
}
