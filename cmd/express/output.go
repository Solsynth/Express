package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// emit prints raw as JSON when asJSON is set (or no renderer exists),
// otherwise defers to the human renderer.
func emit(out io.Writer, asJSON bool, raw json.RawMessage, human func(io.Writer) error) error {
	if asJSON || human == nil {
		if len(raw) == 0 {
			return nil
		}
		return outputJSON(out, raw)
	}
	return human(out)
}

func okLine(out io.Writer, asJSON bool, message string) error {
	if asJSON {
		return outputJSON(out, []byte(`{"ok":true}`))
	}
	fmt.Fprintln(out, message)
	return nil
}

func renderProductBare(w io.Writer, raw json.RawMessage, color bool) error {
	var product productRef
	if err := json.Unmarshal(raw, &product); err != nil {
		return outputJSON(w, raw)
	}
	name := dash(product.Name)
	if color {
		name = ansiBold + name + ansiReset
	}
	fmt.Fprintf(w, "%s (%s)\n", name, dash(product.Slug))
	fmt.Fprintf(w, "ID: %s\n", dash(product.ID))
	return nil
}

func renderChannelDetail(w io.Writer, raw json.RawMessage, color bool) error {
	var channel channelView
	if err := json.Unmarshal(raw, &channel); err != nil {
		return outputJSON(w, raw)
	}
	name := dash(channel.DisplayName)
	if color {
		name = ansiBold + name + ansiReset
	}
	retention := "default"
	if channel.ArtifactRetention != nil {
		retention = fmt.Sprintf("%d", *channel.ArtifactRetention)
	}
	fmt.Fprintf(w, "%s [%s]\n", name, dash(channel.Name))
	fmt.Fprintf(w, "ID:         %s\n", dash(channel.ID))
	fmt.Fprintf(w, "Retention:  %s\n", retention)
	return nil
}

func renderTable(w io.Writer, color bool, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	var builder strings.Builder
	for i, h := range headers {
		builder.WriteString(fmt.Sprintf("%-*s  ", widths[i], h))
	}
	headerLine := strings.TrimRight(builder.String(), " ")
	if color {
		headerLine = ansiBold + headerLine + ansiReset
	}
	fmt.Fprintln(w, headerLine)
	for _, row := range rows {
		builder.Reset()
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			builder.WriteString(fmt.Sprintf("%-*s  ", widths[i], cell))
		}
		fmt.Fprintln(w, strings.TrimRight(builder.String(), " "))
	}
}

func dash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func shortTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return value.UTC().Format("2006-01-02")
}

type productRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type publisherRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Nick string `json:"nick"`
}

type artifactRef struct {
	ID           string `json:"id"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	FileName     string `json:"file_name"`
	Size         int64  `json:"size"`
	DownloadURL  string `json:"download_url"`
	Expired      bool   `json:"expired"`
}

type releaseSummary struct {
	ID          string        `json:"id"`
	Version     string        `json:"version"`
	Title       string        `json:"title"`
	Channel     string        `json:"channel"`
	Channels    []string      `json:"channels"`
	Status      string        `json:"status"`
	PublishedAt *time.Time    `json:"published_at"`
	ForceUpdate bool          `json:"force_update"`
	Artifacts   []artifactRef `json:"artifacts"`
}

type marketplaceApp struct {
	Product   productRef     `json:"product"`
	Publisher publisherRef   `json:"publisher"`
	Latest    releaseSummary `json:"latest"`
}

type appsListResponse struct {
	Data   []marketplaceApp `json:"data"`
	Total  int              `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

func publisherName(p publisherRef) string {
	switch {
	case p.Name != "":
		return p.Name
	case p.Nick != "":
		return p.Nick
	default:
		return dash(p.ID)
	}
}

func releaseChannels(r releaseSummary) string {
	if len(r.Channels) > 0 {
		return strings.Join(r.Channels, ",")
	}
	return dash(r.Channel)
}

type productResponse struct {
	Product   productRef     `json:"product"`
	Publisher publisherRef   `json:"publisher"`
	Latest    releaseSummary `json:"latest"`
}

type channelView struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	DisplayName       string         `json:"display_name"`
	ArtifactRetention *int           `json:"artifact_retention"`
	Latest            releaseSummary `json:"latest"`
}

type channelListResponse struct {
	Data []channelView `json:"data"`
}

type releaseListResponse struct {
	Data  []releaseSummary `json:"data"`
	Total int              `json:"total"`
}

type updateResponse struct {
	UpdateAvailable bool           `json:"update_available"`
	CurrentVersion  string         `json:"current_version"`
	Release         releaseSummary `json:"release"`
}

type metricsResponse struct {
	Checks    int64            `json:"checks"`
	DAU       int64            `json:"dau"`
	MAU       int64            `json:"mau"`
	ByVersion map[string]int64 `json:"by_version"`
}

type uploadKeyView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	KeyPrefix string     `json:"key_prefix"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	Key       string     `json:"key"`
}

type uploadKeyListResponse struct {
	Data []uploadKeyView `json:"data"`
}

func renderApps(w io.Writer, raw json.RawMessage, color bool) error {
	var response appsListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return outputJSON(w, raw)
	}
	rows := make([][]string, 0, len(response.Data))
	for _, app := range response.Data {
		rows = append(rows, []string{
			dash(app.Product.Name),
			dash(app.Product.Slug),
			publisherName(app.Publisher),
			dash(app.Latest.Version),
			shortTime(app.Latest.PublishedAt),
		})
	}
	renderTable(w, color, []string{"NAME", "SLUG", "PUBLISHER", "LATEST", "PUBLISHED"}, rows)
	fmt.Fprintf(w, "\n%d apps (limit %d, offset %d)\n", response.Total, response.Limit, response.Offset)
	return nil
}

func renderProduct(w io.Writer, raw json.RawMessage, color bool) error {
	var response productResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return outputJSON(w, raw)
	}
	name := response.Product.Name
	if color {
		name = ansiBold + name + ansiReset
	}
	fmt.Fprintf(w, "%s (%s)\n", name, dash(response.Product.Slug))
	fmt.Fprintf(w, "ID:        %s\n", dash(response.Product.ID))
	fmt.Fprintf(w, "Publisher: %s\n", publisherName(response.Publisher))
	if latest := response.Latest; latest.Version != "" {
		status := latest.Status
		if status == "" {
			status = "published"
		}
		fmt.Fprintf(w, "Latest:    v%s [%s] on %s\n", latest.Version, status, releaseChannels(latest))
	} else {
		fmt.Fprintln(w, "Latest:    none")
	}
	return nil
}

func renderChannels(w io.Writer, raw json.RawMessage, color bool) error {
	var response channelListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return outputJSON(w, raw)
	}
	rows := make([][]string, 0, len(response.Data))
	for _, channel := range response.Data {
		retention := "default"
		if channel.ArtifactRetention != nil {
			retention = fmt.Sprintf("%d", *channel.ArtifactRetention)
		}
		display := channel.DisplayName
		if display == "" {
			display = channel.Name
		}
		rows = append(rows, []string{dash(channel.ID), dash(channel.Name), display, retention, dash(channel.Latest.Version)})
	}
	renderTable(w, color, []string{"ID", "NAME", "DISPLAY", "RETENTION", "LATEST"}, rows)
	fmt.Fprintf(w, "\n%d channels\n", len(response.Data))
	return nil
}

func releaseStatus(r releaseSummary) string {
	if r.Status != "" {
		return r.Status
	}
	if r.PublishedAt != nil {
		return "published"
	}
	return "draft"
}

func renderReleases(w io.Writer, raw json.RawMessage, color bool) error {
	var response releaseListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return outputJSON(w, raw)
	}
	rows := make([][]string, 0, len(response.Data))
	for _, release := range response.Data {
		rows = append(rows, []string{
			dash(release.Version),
			releaseStatus(release),
			releaseChannels(release),
			dash(release.Title),
			fmt.Sprintf("%d", len(release.Artifacts)),
			shortTime(release.PublishedAt),
		})
	}
	renderTable(w, color, []string{"VERSION", "STATUS", "CHANNELS", "TITLE", "ARTIFACTS", "PUBLISHED"}, rows)
	fmt.Fprintf(w, "\n%d releases\n", response.Total)
	return nil
}

func renderReleaseDetail(w io.Writer, raw json.RawMessage, color bool) error {
	var release releaseSummary
	if err := json.Unmarshal(raw, &release); err != nil {
		return outputJSON(w, raw)
	}
	title := release.Title
	if title == "" {
		title = release.Version
	}
	if color {
		title = ansiBold + title + ansiReset
	}
	fmt.Fprintf(w, "%s [v%s]\n", title, dash(release.Version))
	fmt.Fprintf(w, "ID:      %s\n", dash(release.ID))
	fmt.Fprintf(w, "Status:  %s\n", releaseStatus(release))
	fmt.Fprintf(w, "Channel: %s\n", releaseChannels(release))
	if release.ForceUpdate {
		fmt.Fprintln(w, "Force update: yes")
	}
	for _, artifact := range release.Artifacts {
		state := ""
		if artifact.Expired {
			state = " (expired)"
		}
		target := artifact.Platform + "/" + artifact.Architecture
		if target == "/" {
			target = artifact.FileName
		}
		fmt.Fprintf(w, "Artifact %-16s %s%s\n", target, dash(artifact.DownloadURL), state)
	}
	return nil
}

func renderUpdateCheck(w io.Writer, raw json.RawMessage, color bool) error {
	var response updateResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return outputJSON(w, raw)
	}
	if !response.UpdateAvailable {
		line := fmt.Sprintf("Up to date (current version %s).", dash(response.CurrentVersion))
		if color {
			line = ansiGreen + line + ansiReset
		}
		fmt.Fprintln(w, line)
		return nil
	}
	line := fmt.Sprintf("Update available: v%s", dash(response.Release.Version))
	if color {
		line = ansiGreen + line + ansiReset
	}
	fmt.Fprintln(w, line)
	if response.Release.Title != "" {
		fmt.Fprintf(w, "Title:   %s\n", response.Release.Title)
	}
	if response.Release.Channel != "" || len(response.Release.Channels) > 0 {
		fmt.Fprintf(w, "Channel: %s\n", releaseChannels(response.Release))
	}
	for _, artifact := range response.Release.Artifacts {
		fmt.Fprintf(w, "Download %-14s %s\n", artifact.Platform+"/"+artifact.Architecture, dash(artifact.DownloadURL))
	}
	if response.Release.ForceUpdate {
		warning := "This update is mandatory (force update)."
		if color {
			warning = ansiYellow + warning + ansiReset
		}
		fmt.Fprintln(w, warning)
	}
	return nil
}

func renderMetrics(w io.Writer, raw json.RawMessage, color bool) error {
	var response metricsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return outputJSON(w, raw)
	}
	fmt.Fprintf(w, "Checks: %d\n", response.Checks)
	fmt.Fprintf(w, "DAU:    %d\n", response.DAU)
	fmt.Fprintf(w, "MAU:    %d\n", response.MAU)
	if len(response.ByVersion) > 0 {
		fmt.Fprintln(w, "By version:")
		for version, count := range response.ByVersion {
			fmt.Fprintf(w, "  %-12s %d\n", version, count)
		}
	}
	return nil
}

func renderUploadKeyCreated(w io.Writer, raw json.RawMessage, color bool) error {
	var key uploadKeyView
	if err := json.Unmarshal(raw, &key); err != nil {
		return outputJSON(w, raw)
	}
	fmt.Fprintf(w, "Upload key created: %s\n", dash(key.Name))
	fmt.Fprintf(w, "ID:         %s\n", dash(key.ID))
	if key.Key != "" {
		secret := key.Key
		if color {
			secret = ansiGreen + secret + ansiReset
		}
		fmt.Fprintf(w, "Key (shown once): %s\n", secret)
	}
	fmt.Fprintln(w, "Store it securely now; it cannot be retrieved again.")
	return nil
}

func renderUploadKeys(w io.Writer, raw json.RawMessage, color bool) error {
	var response uploadKeyListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return outputJSON(w, raw)
	}
	rows := make([][]string, 0, len(response.Data))
	for _, key := range response.Data {
		state := "active"
		if key.RevokedAt != nil {
			state = "revoked"
		}
		rows = append(rows, []string{dash(key.ID), dash(key.Name), dash(key.KeyPrefix), state, shortTime(&key.CreatedAt)})
	}
	renderTable(w, color, []string{"ID", "NAME", "PREFIX", "STATE", "CREATED"}, rows)
	fmt.Fprintf(w, "\n%d keys\n", len(response.Data))
	return nil
}
