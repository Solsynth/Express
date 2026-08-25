package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiCyan   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
)

func colorEnabled(out io.Writer) bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("NO_COLOR")), "1") || os.Getenv("NO_COLOR") != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("EXPRESS_COLOR")), "never") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("EXPRESS_COLOR")), "always") {
		return true
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func colorize(out io.Writer, color, text string) string {
	if !colorEnabled(out) {
		return text
	}
	return color + text + ansiReset
}

func statusLine(out io.Writer, color, text string) error {
	_, err := fmt.Fprintln(out, colorize(out, color, text))
	return err
}

func promptLabel(out io.Writer, label string) string {
	return colorize(out, ansiCyan+ansiBold, label)
}

func interactive(out io.Writer) bool {
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func promptDefault(out io.Writer, label, fallback string) (string, error) {
	if fallback != "" {
		label += " [" + fallback + "]"
	}
	value, err := promptLine(out, promptLabel(out, label)+": ")
	if err != nil {
		return "", err
	}
	if value == "" {
		return fallback, nil
	}
	return value, nil
}
