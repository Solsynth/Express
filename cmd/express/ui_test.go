package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestStatusLineHonorsColorEnvironment(t *testing.T) {
	original := os.Getenv("EXPRESS_COLOR")
	originalNoColor := os.Getenv("NO_COLOR")
	t.Cleanup(func() {
		_ = os.Setenv("EXPRESS_COLOR", original)
		_ = os.Setenv("NO_COLOR", originalNoColor)
	})

	_ = os.Setenv("EXPRESS_COLOR", "always")
	_ = os.Unsetenv("NO_COLOR")
	var colored bytes.Buffer
	if err := statusLine(&colored, ansiGreen, "Ready"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored.String(), "\x1b[32mReady") {
		t.Fatalf("colored output = %q", colored.String())
	}

	_ = os.Setenv("NO_COLOR", "1")
	var plain bytes.Buffer
	if err := statusLine(&plain, ansiGreen, "Ready"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatalf("plain output = %q", plain.String())
	}
}

func TestBodyFromFlagOrPromptRequiresBodyForNonInteractiveOutput(t *testing.T) {
	_, err := bodyFromFlagOrPrompt("", io.Discard, func(io.Writer) ([]byte, error) {
		return []byte(`{"ok":true}`), nil
	})
	if err == nil || !strings.Contains(err.Error(), "--body is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestProgressReaderRendersBar(t *testing.T) {
	datalo := []byte("artifact bytes for progress")
	reader := newProgressReader(bytes.NewReader(datalo), int64(len(datalo)), io.Discard)
	buf := make([]byte, 8)
	n, err := reader.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("read n=%d err=%v", n, err)
	}
	if reader.read != int64(n) {
		t.Fatalf("reader.read = %d, want %d", reader.read, n)
	}
	// Read remaining
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if reader.read != int64(len(datalo)) {
		t.Fatalf("reader.read = %d after full read, want %d", reader.read, len(datalo))
	}
	_ = rest
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		b    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1536, "1.5 KB"},
		{2 << 20, "2.0 MB"},
		{3 << 30, "3.0 GB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.b); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.b, got, c.want)
		}
	}
}
