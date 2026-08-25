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
