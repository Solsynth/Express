package sync

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"src.solsynth.dev/solsynth/distribution/internal/database"
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
