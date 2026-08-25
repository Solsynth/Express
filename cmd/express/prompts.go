package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func bodyFromFlagOrPrompt(path string, out io.Writer, prompt func(io.Writer) ([]byte, error)) ([]byte, error) {
	if path != "" {
		return bodyFromFlag(path)
	}
	if !interactive(out) {
		return nil, errors.New("--body is required when stdin is not interactive")
	}
	return prompt(out)
}
func valueOrPrompt(out io.Writer, name, value, label string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return value, nil
	}
	if !interactive(out) {
		return "", fmt.Errorf("--%s is required", name)
	}
	prompted, err := promptLine(out, promptLabel(out, label)+": ")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(prompted) == "" {
		return "", fmt.Errorf("--%s is required", name)
	}
	return prompted, nil
}

func promptProductBody(out io.Writer) ([]byte, error) {
	slug, err := promptLine(out, promptLabel(out, "Product slug")+": ")
	if err != nil {
		return nil, err
	}
	name, err := promptLine(out, promptLabel(out, "Product name")+": ")
	if err != nil {
		return nil, err
	}
	description, err := promptLine(out, promptLabel(out, "Description (optional)")+": ")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(slug) == "" || strings.TrimSpace(name) == "" {
		return nil, errors.New("product slug and name are required")
	}
	return json.Marshal(map[string]any{"slug": slug, "name": name, "description": description})
}

func promptChannelBody(out io.Writer) ([]byte, error) {
	name, err := promptLine(out, promptLabel(out, "Channel name")+": ")
	if err != nil {
		return nil, err
	}
	displayName, err := promptDefault(out, "Display name", name)
	if err != nil {
		return nil, err
	}
	retention, err := promptDefault(out, "Artifact retention (optional)", "")
	if err != nil {
		return nil, err
	}
	body := map[string]any{"name": name, "display_name": displayName}
	if strings.TrimSpace(retention) != "" {
		value, parseErr := strconv.Atoi(retention)
		if parseErr != nil || value < 0 {
			return nil, fmt.Errorf("artifact retention must be a non-negative integer")
		}
		body["artifact_retention"] = value
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("channel name is required")
	}
	return json.Marshal(body)
}

func promptReleaseBody(out io.Writer) ([]byte, error) {
	version, err := promptLine(out, promptLabel(out, "Release version")+": ")
	if err != nil {
		return nil, err
	}
	channelsText, err := promptDefault(out, "Channels (comma-separated)", "stable")
	if err != nil {
		return nil, err
	}
	title, err := promptLine(out, promptLabel(out, "Title (optional)")+": ")
	if err != nil {
		return nil, err
	}
	notes, err := promptLine(out, promptLabel(out, "Release notes (optional)")+": ")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(version) == "" {
		return nil, errors.New("release version is required")
	}
	channels := make([]string, 0)
	for _, channel := range strings.Split(channelsText, ",") {
		if value := strings.TrimSpace(channel); value != "" {
			channels = append(channels, value)
		}
	}
	if len(channels) == 0 {
		return nil, errors.New("at least one release channel is required")
	}
	return json.Marshal(map[string]any{"version": version, "channels": channels, "title": title, "release_notes": notes})
}
