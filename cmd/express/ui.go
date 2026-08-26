package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"golang.org/x/term"
)

var progressModel = progress.New(
	progress.WithDefaultGradient(),
	progress.WithWidth(40),
	progress.WithoutPercentage(),
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

// progressReader wraps an io.Reader and renders a progress bar to a writer
// as data is read. Use clearProgressWriter to remove the bar after a
// successful transfer.
type progressReader struct {
	reader    io.Reader
	total     int64
	read      int64
	writer    io.Writer
	lastWidth int
	finished  bool
}

func newProgressReader(r io.Reader, total int64, w io.Writer) *progressReader {
	return &progressReader{reader: r, total: total, writer: w}
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	r.render()
	if err != nil {
		r.finished = true
		return n, err
	}
	return n, nil
}

func (r *progressReader) render() {
	if r.total <= 0 {
		return
	}
	percent := float64(r.read) / float64(r.total)
	if percent > 1 {
		percent = 1
	}
	bar := progressModel.ViewAs(percent)
	line := fmt.Sprintf("\r  Uploading %s / %s %s",
		humanBytes(r.read), humanBytes(r.total), bar)
	fmt.Fprint(r.writer, line)
}

// clearProgressWriter overwrites the progress bar with spaces so the line
// is clean after a successful upload.
func clearProgressWriter(w io.Writer) {
	fmt.Fprint(w, "\r"+strings.Repeat(" ", 80)+"\r")
}

func humanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
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
