package sync

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// decodeSecret decodes the sealed JSON payload into the adapter-facing map.
func decodeSecret(plain []byte, out *map[string]string) error {
	decoded := map[string]string{}
	if err := json.Unmarshal(plain, &decoded); err != nil {
		return fmt.Errorf("sealed sync secret is unreadable")
	}
	*out = decoded
	return nil
}

// channelNames returns the sorted channel names of a release.
func channelNames(release *database.Release) []string {
	names := make([]string, 0, len(release.Channels))
	for _, channel := range release.Channels {
		if name := strings.TrimSpace(channel.Name); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// releaseTitle falls back to "{product} {version}" when the release has no
// title.
func releaseTitle(release *database.Release, product *database.Product) string {
	if title := strings.TrimSpace(release.Title); title != "" {
		return title
	}
	name := strings.TrimSpace(product.Name)
	if name == "" {
		name = product.Slug
	}
	return strings.TrimSpace(name + " " + release.Version)
}

// releaseNotes prefers the authored notes, then the en-US description, then
// any description.
func releaseNotes(release *database.Release) string {
	if notes := strings.TrimSpace(release.ReleaseNotes); notes != "" {
		return notes
	}
	if description := strings.TrimSpace(release.Descriptions["en-US"]); description != "" {
		return description
	}
	if description := strings.TrimSpace(release.Descriptions["en_US"]); description != "" {
		return description
	}
	locales := make([]string, 0, len(release.Descriptions))
	for locale := range release.Descriptions {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	for _, locale := range locales {
		if description := strings.TrimSpace(release.Descriptions[locale]); description != "" {
			return description
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

// releaseDescription renders the body a destination stores for a release: the
// publisher's notes verbatim, then a horizontal rule and a markdown table of
// downloadable artifacts. The table is omitted when the release has no
// downloadable artifact, and the rule is omitted when there are no notes, so
// the publisher's text is never altered or prefixed.
//
// URLs are the public download endpoints; presigned URLs are deliberately never
// used because a destination release outlives any signature.
func releaseDescription(notes string, artifacts []ArtifactLink) string {
	notes = strings.TrimRight(notes, "\n")
	rows := make([]ArtifactLink, 0, len(artifacts))
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.URL) == "" {
			continue
		}
		rows = append(rows, artifact)
	}
	if len(rows) == 0 {
		return notes
	}
	var builder strings.Builder
	if strings.TrimSpace(notes) != "" {
		builder.WriteString(notes)
		builder.WriteString("\n\n---\n\n")
	}
	builder.WriteString("| Artifact | Platform | Architecture | Size | Download |\n")
	builder.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, artifact := range rows {
		name := strings.TrimSpace(artifact.Name)
		if name == "" {
			name = "download"
		}
		builder.WriteString("| ")
		builder.WriteString(tableCell(name))
		builder.WriteString(" | ")
		builder.WriteString(tableCell(artifact.Platform))
		builder.WriteString(" | ")
		builder.WriteString(tableCell(artifact.Architecture))
		builder.WriteString(" | ")
		builder.WriteString(humanSize(artifact.Size))
		builder.WriteString(" | [Download](")
		builder.WriteString(strings.ReplaceAll(artifact.URL, ")", "%29"))
		builder.WriteString(") |\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}

// tableCell escapes the characters that would break a markdown table row.
func tableCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	value = strings.ReplaceAll(value, "|", `\|`)
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

// humanSize renders a byte count the way a release reader expects to see it.
func humanSize(size int64) string {
	if size <= 0 {
		return "-"
	}
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

// snapshotFromService converts the release service's snapshot into the payload
// snapshot stored on the job, so the service package stays free of sync types.
func snapshotFromService(in service.ReleaseSyncSnapshot) PayloadSnapshot {
	snapshot := PayloadSnapshot{
		ProductSlug: in.ProductSlug,
		ProductName: in.ProductName,
		ProductID:   in.ProductID,
		Version:     in.Version,
		Title:       in.Title,
		Notes:       in.Notes,
		Channels:    in.Channels,
		Prerelease:  in.Prerelease,
		Draft:       in.Draft,
	}
	for _, artifact := range in.Artifacts {
		snapshot.Artifacts = append(snapshot.Artifacts, ArtifactLink{
			Name: artifact.Name, Platform: artifact.Platform,
			Architecture: artifact.Architecture, Size: artifact.Size, URL: artifact.URL,
		})
	}
	return snapshot
}
