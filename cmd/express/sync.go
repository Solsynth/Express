package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"src.solsynth.dev/solsynth/distribution/internal/marketplace"
)

// secretFlags collects repeated --secret key=value pairs.
type secretFlags struct {
	values map[string]string
}

func (s *secretFlags) String() string {
	if s == nil || len(s.values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(s.values))
	for key := range s.values {
		keys = append(keys, key)
	}
	return strings.Join(keys, ",")
}

func (s *secretFlags) Set(value string) error {
	key, secret, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return errors.New("--secret must be key=value")
	}
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[strings.TrimSpace(key)] = strings.TrimSpace(secret)
	return nil
}

// syncCommand dispatches the sync resource.
func syncCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer, asJSON bool) error {
	action, rest, err := requireAction(args, "sync")
	if err != nil {
		return err
	}
	switch action {
	case "connection":
		return syncConnectionCommand(ctx, client, rest, stdout, stderr, asJSON)
	case "link":
		return syncLinkCommand(ctx, client, rest, stdout, stderr, asJSON)
	case "job":
		return syncJobCommand(ctx, client, rest, stdout, stderr, asJSON)
	case "providers":
		return syncProvidersCommand(ctx, client, rest, stdout, stderr, asJSON)
	default:
		return fmt.Errorf("unknown sync action %q", action)
	}
}

func syncConnectionCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer, asJSON bool) error {
	action, rest, err := requireAction(args, "sync connection")
	if err != nil {
		return err
	}
	fs := newCommand("sync connection "+action, rest, stderr)
	publisher := fs.String("publisher", "", "publisher name")
	id := fs.String("id", "", "connection ID")
	provider := fs.String("provider", "", "provider module ID")
	name := fs.String("name", "", "connection name")
	baseURL := fs.String("base-url", "", "destination base URL")
	bodyPath := fs.String("body", "", "JSON request body file, or - for stdin")
	var secrets secretFlags
	fs.Var(&secrets, "secret", "adapter secret as key=value (repeatable)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	resolvedPublisher, err := resolvePublisherValue(ctx, client, stderr, *publisher)
	if err != nil {
		return err
	}
	*publisher = resolvedPublisher
	switch action {
	case "list":
		raw, err := client.ListSyncConnections(ctx, *publisher)
		if err != nil {
			return err
		}
		return emit(stdout, asJSON, raw, func(w io.Writer) error { return renderSyncConnections(w, raw, colorEnabled(stdout)) })
	case "create":
		body, err := connectionBody(*bodyPath, *provider, *name, *baseURL, secrets.values)
		if err != nil {
			return err
		}
		raw, err := client.CreateSyncConnection(ctx, *publisher, body)
		if err != nil {
			return err
		}
		return emit(stdout, asJSON, raw, func(w io.Writer) error { return renderSyncConnection(w, raw, colorEnabled(stdout)) })
	case "update":
		if err := requireValue("id", *id); err != nil {
			return err
		}
		body, err := connectionBody(*bodyPath, *provider, *name, *baseURL, secrets.values)
		if err != nil {
			return err
		}
		raw, err := client.UpdateSyncConnection(ctx, *publisher, *id, body)
		if err != nil {
			return err
		}
		return emit(stdout, asJSON, raw, func(w io.Writer) error { return renderSyncConnection(w, raw, colorEnabled(stdout)) })
	case "delete":
		if err := requireValue("id", *id); err != nil {
			return err
		}
		if err := client.DeleteSyncConnection(ctx, *publisher, *id); err != nil {
			return err
		}
		return okLine(stdout, asJSON, "Connection deleted.")
	default:
		return fmt.Errorf("unknown sync connection action %q", action)
	}
}

// connectionBody builds a connection request from flags or a JSON body file.
func connectionBody(bodyPath, provider, name, baseURL string, secret map[string]string) ([]byte, error) {
	if strings.TrimSpace(bodyPath) != "" {
		return bodyFromFlag(bodyPath)
	}
	if strings.TrimSpace(provider) == "" {
		return nil, errors.New("--provider is required")
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("--name is required")
	}
	request := map[string]any{"provider": strings.TrimSpace(provider), "name": strings.TrimSpace(name)}
	if base := strings.TrimSpace(baseURL); base != "" {
		request["base_url"] = base
	}
	if len(secret) > 0 {
		request["secret"] = secret
	}
	return json.Marshal(request)
}

func syncLinkCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer, asJSON bool) error {
	action, rest, err := requireAction(args, "sync link")
	if err != nil {
		return err
	}
	fs := newCommand("sync link "+action, rest, stderr)
	product := fs.String("product", "", "product ID")
	id := fs.String("id", "", "link ID")
	connection := fs.String("connection", "", "connection ID")
	repository := fs.String("repository", "", "destination repository (owner/name)")
	include := fs.String("include", "", "comma-separated channels to mirror")
	exclude := fs.String("exclude", "", "comma-separated channels to skip")
	draft := fs.Bool("draft", false, "create destination releases as drafts")
	prerelease := fs.Bool("prerelease", false, "mark destination releases as prereleases")
	disabled := fs.Bool("disabled", false, "store the link disabled")
	bodyPath := fs.String("body", "", "JSON request body file, or - for stdin")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	resolvedProduct, err := resolveProductValue(ctx, client, stderr, *product)
	if err != nil {
		return err
	}
	*product = resolvedProduct
	switch action {
	case "list":
		raw, err := client.ListSyncLinks(ctx, *product)
		if err != nil {
			return err
		}
		return emit(stdout, asJSON, raw, func(w io.Writer) error { return renderSyncLinks(w, raw, colorEnabled(stdout)) })
	case "create", "update":
		if action == "update" {
			if err := requireValue("id", *id); err != nil {
				return err
			}
		}
		var body []byte
		if strings.TrimSpace(*bodyPath) != "" {
			body, err = bodyFromFlag(*bodyPath)
		} else {
			body, err = linkBody(*connection, *repository, *include, *exclude, *draft, *prerelease, !*disabled)
		}
		if err != nil {
			return err
		}
		if action == "create" {
			raw, err := client.CreateSyncLink(ctx, *product, body)
			if err != nil {
				return err
			}
			return emit(stdout, asJSON, raw, func(w io.Writer) error { return renderSyncLink(w, raw, colorEnabled(stdout)) })
		}
		raw, err := client.UpdateSyncLink(ctx, *product, *id, body)
		if err != nil {
			return err
		}
		return emit(stdout, asJSON, raw, func(w io.Writer) error { return renderSyncLink(w, raw, colorEnabled(stdout)) })
	case "delete":
		if err := requireValue("id", *id); err != nil {
			return err
		}
		if err := client.DeleteSyncLink(ctx, *product, *id); err != nil {
			return err
		}
		return okLine(stdout, asJSON, "Link deleted.")
	default:
		return fmt.Errorf("unknown sync link action %q", action)
	}
}

// linkBody builds a link request from flags.
func linkBody(connection, repository, include, exclude string, draft, prerelease, enabled bool) ([]byte, error) {
	if strings.TrimSpace(connection) == "" {
		return nil, errors.New("--connection is required")
	}
	request := map[string]any{
		"connection_id": strings.TrimSpace(connection),
		"repository":    strings.TrimSpace(repository),
		"draft":         draft,
		"prerelease":    prerelease,
		"enabled":       enabled,
	}
	channels := map[string]any{}
	if names := splitList(include); len(names) > 0 {
		channels["include"] = names
	}
	if names := splitList(exclude); len(names) > 0 {
		channels["exclude"] = names
	}
	if len(channels) > 0 {
		request["channels"] = channels
	}
	return json.Marshal(request)
}

func splitList(value string) []string {
	items := make([]string, 0, 2)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func syncJobCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer, asJSON bool) error {
	action, rest, err := requireAction(args, "sync job")
	if err != nil {
		return err
	}
	fs := newCommand("sync job "+action, rest, stderr)
	product := fs.String("product", "", "product ID")
	id := fs.String("id", "", "job ID")
	state := fs.String("state", "", "filter by state: pending, running, done, failed")
	limit := fs.Int("limit", 20, "number of jobs")
	offset := fs.Int("offset", 0, "pagination offset")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	resolvedProduct, err := resolveProductValue(ctx, client, stderr, *product)
	if err != nil {
		return err
	}
	*product = resolvedProduct
	switch action {
	case "list":
		query := url.Values{"limit": {strconv.Itoa(*limit)}, "offset": {strconv.Itoa(*offset)}}
		if strings.TrimSpace(*state) != "" {
			query.Set("state", *state)
		}
		raw, err := client.ListSyncJobs(ctx, *product, query)
		if err != nil {
			return err
		}
		return emit(stdout, asJSON, raw, func(w io.Writer) error { return renderSyncJobs(w, raw, colorEnabled(stdout)) })
	case "retry":
		if err := requireValue("id", *id); err != nil {
			return err
		}
		raw, err := client.RetrySyncJob(ctx, *product, *id)
		if err != nil {
			return err
		}
		return emit(stdout, asJSON, raw, func(w io.Writer) error { return renderSyncJob(w, raw, colorEnabled(stdout)) })
	default:
		return fmt.Errorf("unknown sync job action %q", action)
	}
}

func syncProvidersCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer, asJSON bool) error {
	fs := newCommand("sync providers", args, stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	raw, err := client.Modules(ctx)
	if err != nil {
		return err
	}
	return emit(stdout, asJSON, raw, func(w io.Writer) error { return renderModules(w, raw, colorEnabled(stdout)) })
}

// renderModules prints the enabled module IDs that can be sync destinations.
func renderModules(w io.Writer, raw json.RawMessage, color bool) error {
	var response struct {
		Modules []struct {
			ID           string   `json:"id"`
			Kind         string   `json:"kind"`
			Capabilities []string `json:"capabilities"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return outputJSON(w, raw)
	}
	rows := make([][]string, 0, len(response.Modules))
	for _, module := range response.Modules {
		if module.Kind != "integration" {
			continue
		}
		rows = append(rows, []string{module.ID, module.Kind, strings.Join(module.Capabilities, ",")})
	}
	renderTable(w, color, []string{"ID", "KIND", "CAPABILITIES"}, rows)
	return nil
}

func renderSyncConnections(w io.Writer, raw json.RawMessage, color bool) error {
	var response struct {
		Data []syncConnectionView `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return outputJSON(w, raw)
	}
	rows := make([][]string, 0, len(response.Data))
	for _, connection := range response.Data {
		secret := "-"
		if connection.HasSecret {
			secret = "set"
		}
		rows = append(rows, []string{connection.Name, connection.Provider, dash(connection.BaseURL), secret})
	}
	renderTable(w, color, []string{"NAME", "PROVIDER", "BASE URL", "SECRET"}, rows)
	return nil
}

func renderSyncConnection(w io.Writer, raw json.RawMessage, color bool) error {
	var connection syncConnectionView
	if err := json.Unmarshal(raw, &connection); err != nil {
		return outputJSON(w, raw)
	}
	secret := "not set"
	if connection.HasSecret {
		secret = "set"
	}
	name := dash(connection.Name)
	if color {
		name = ansiBold + name + ansiReset
	}
	fmt.Fprintf(w, "%s [%s]\n", name, dash(connection.Provider))
	fmt.Fprintf(w, "ID:       %s\n", dash(connection.ID))
	fmt.Fprintf(w, "Base URL: %s\n", dash(connection.BaseURL))
	fmt.Fprintf(w, "Secret:   %s\n", secret)
	return nil
}

func renderSyncLinks(w io.Writer, raw json.RawMessage, color bool) error {
	var response struct {
		Data []syncLinkView `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return outputJSON(w, raw)
	}
	rows := make([][]string, 0, len(response.Data))
	for _, link := range response.Data {
		rows = append(rows, []string{
			dash(link.Repository),
			dash(link.Provider),
			channelFilterText(link.Channels),
			shortTime(link.LastSyncedAt),
		})
	}
	renderTable(w, color, []string{"REPOSITORY", "PROVIDER", "CHANNELS", "LAST SYNC"}, rows)
	return nil
}

func renderSyncLink(w io.Writer, raw json.RawMessage, color bool) error {
	var link syncLinkView
	if err := json.Unmarshal(raw, &link); err != nil {
		return outputJSON(w, raw)
	}
	repository := dash(link.Repository)
	if color {
		repository = ansiBold + repository + ansiReset
	}
	fmt.Fprintf(w, "%s -> %s\n", repository, dash(link.Provider))
	fmt.Fprintf(w, "ID:         %s\n", dash(link.ID))
	fmt.Fprintf(w, "Connection: %s\n", dash(link.Connection))
	fmt.Fprintf(w, "Channels:   %s\n", channelFilterText(link.Channels))
	fmt.Fprintf(w, "Enabled:    %t\n", link.Enabled)
	return nil
}

func renderSyncJobs(w io.Writer, raw json.RawMessage, color bool) error {
	var page struct {
		Data []syncJobView `json:"data"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return outputJSON(w, raw)
	}
	rows := make([][]string, 0, len(page.Data))
	for _, job := range page.Data {
		rows = append(rows, []string{
			job.State,
			job.Action,
			dash(job.Version),
			strconv.Itoa(job.Attempts),
			truncateText(job.LastError, 60),
			dash(job.ExternalURL),
		})
	}
	renderTable(w, color, []string{"STATE", "ACTION", "VERSION", "ATTEMPTS", "LAST ERROR", "EXTERNAL URL"}, rows)
	return nil
}

func renderSyncJob(w io.Writer, raw json.RawMessage, color bool) error {
	var job syncJobView
	if err := json.Unmarshal(raw, &job); err != nil {
		return outputJSON(w, raw)
	}
	state := job.State
	if color {
		state = ansiBold + state + ansiReset
	}
	fmt.Fprintf(w, "%s %s %s\n", state, dash(job.Action), dash(job.Version))
	fmt.Fprintf(w, "ID:       %s\n", dash(job.ID))
	fmt.Fprintf(w, "Attempts: %d\n", job.Attempts)
	fmt.Fprintf(w, "Next:     %s\n", job.NextAttempt.UTC().Format(time.RFC3339))
	if job.LastError != "" {
		fmt.Fprintf(w, "Error:    %s\n", job.LastError)
	}
	if job.ExternalURL != "" {
		fmt.Fprintf(w, "URL:      %s\n", job.ExternalURL)
	}
	return nil
}

func channelFilterText(filter channelFilterView) string {
	parts := make([]string, 0, 2)
	if len(filter.Include) > 0 {
		parts = append(parts, "include:"+strings.Join(filter.Include, ","))
	}
	if len(filter.Exclude) > 0 {
		parts = append(parts, "exclude:"+strings.Join(filter.Exclude, ","))
	}
	if len(parts) == 0 {
		return "all"
	}
	return strings.Join(parts, " ")
}

func truncateText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return dash(value)
	}
	return value[:limit] + "..."
}

type channelFilterView struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

type syncConnectionView struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	HasSecret bool   `json:"has_secret"`
}

type syncLinkView struct {
	ID           string            `json:"id"`
	Provider     string            `json:"provider"`
	Connection   string            `json:"connection"`
	Repository   string            `json:"repository"`
	Channels     channelFilterView `json:"channels"`
	Enabled      bool              `json:"enabled"`
	LastSyncedAt *time.Time        `json:"last_synced_at"`
}

type syncJobView struct {
	ID          string    `json:"id"`
	Action      string    `json:"action"`
	State       string    `json:"state"`
	Version     string    `json:"version"`
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt_at"`
	ExternalURL string    `json:"external_url"`
	LastError   string    `json:"last_error"`
}

// resolvePublisherValue resolves a publisher name, falling back to the
// marketplace product list the same way product selection does.
func resolvePublisherValue(ctx context.Context, client *marketplace.Client, stderr io.Writer, value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return value, nil
	}
	if !interactive(stderr) {
		return "", errors.New("--publisher is required")
	}
	raw, err := client.ListMarketplaceApps(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("list products for publisher selection: %w", err)
	}
	var response appsListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode product list: %w", err)
	}
	seen := map[string]bool{}
	names := make([]string, 0, 4)
	for _, app := range response.Data {
		name := publisherName(app.Publisher)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return "", errors.New("no publishers found in the marketplace")
	}
	if len(names) == 1 {
		return names[0], nil
	}
	items := make([]pickerItem, len(names))
	for i, name := range names {
		items[i] = pickerItem{id: name, name: name}
	}
	m := newPickerModel(items, 80)
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(stderr))
	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("run picker: %w", err)
	}
	final := result.(pickerModel)
	if final.cancelled || final.chosen == "" {
		return "", errors.New("no publisher selected")
	}
	return final.chosen, nil
}
