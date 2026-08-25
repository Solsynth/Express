package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"src.solsynth.dev/solsynth/distribution/internal/marketplace"
)

var promptReader = bufio.NewReader(os.Stdin)

type storedAuth struct {
	BaseURL        string `json:"base_url"`
	Token          string `json:"token"`
	RefreshToken   string `json:"refresh_token,omitempty"`
	TokenExpiresAt int64  `json:"token_expires_at,omitempty"`
}

func authStorePath(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, nil
	}
	if configured := strings.TrimSpace(os.Getenv("SOL_SYNTH_EXPRESS_CONFIG")); configured != "" {
		return configured, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(configDir, "dev.solsynth.express", "config.json"), nil
}

func loadStoredAuth(path string) (storedAuth, error) {
	if path == "" {
		return storedAuth{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return storedAuth{}, nil
	}
	if err != nil {
		return storedAuth{}, fmt.Errorf("read auth config %s: %w", path, err)
	}
	var auth storedAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return storedAuth{}, fmt.Errorf("parse auth config %s: %w", path, err)
	}
	return auth, nil
}

func saveStoredAuth(path string, auth storedAuth) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("auth config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create auth config directory: %w", err)
	}
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return fmt.Errorf("encode auth config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write auth config %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure auth config %s: %w", path, err)
	}
	return nil
}

func deleteStoredAuth(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove auth config %s: %w", path, err)
	}
	return nil
}

func authCommand(args []string, stdout, stderr io.Writer, configPath, defaultBaseURL, defaultToken, defaultClientID, defaultDiscoveryURL, defaultScope string) error {
	action, rest, err := requireAction(args, "auth")
	if err != nil {
		return err
	}
	fs := newCommand("auth "+action, rest, stderr)
	token := fs.String("token", defaultToken, "Sphere access token")
	baseURL := fs.String("base-url", defaultBaseURL, "DistributionCenter base URL")
	clientID := fs.String("client-id", defaultClientID, "registered OAuth public client ID")
	discoveryURL := fs.String("oidc-discovery-url", defaultDiscoveryURL, "OpenID Connect discovery URL")
	scope := fs.String("scope", defaultScope, "OAuth scope")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	path, err := authStorePath(configPath)
	if err != nil {
		return err
	}

	switch action {
	case "login":
		stored, err := loadStoredAuth(path)
		if err != nil {
			return err
		}
		if strings.TrimSpace(*baseURL) == "" {
			*baseURL = firstEnv("DISTRIBUTION_API_URL", "DISTRIBUTION_BASE_URL", "SOL_SYNTH_EXPRESS_URL")
		}
		if strings.TrimSpace(*baseURL) == "" {
			*baseURL = stored.BaseURL
		}
		if strings.TrimSpace(*baseURL) == "" {
			*baseURL = defaultDistributionBaseURL
		}
		if strings.TrimSpace(*token) == "" {
			*token = firstEnv("DISTRIBUTION_TOKEN", "SPHERE_ACCESS_TOKEN")
		}
		if strings.TrimSpace(*token) == "" {
			if strings.TrimSpace(*clientID) == "" {
				*clientID = firstEnv("SOL_SYNTH_EXPRESS_CLIENT_ID", "OAUTH_CLIENT_ID")
			}
			httpClient := http.DefaultClient
			oauthContext := context.Background()
			tokenResponse, loginErr := deviceLogin(oauthContext, httpClient, *discoveryURL, *clientID, *scope, stderr)
			if loginErr != nil {
				return loginErr
			}
			*token = tokenResponse.AccessToken
			if err := saveStoredAuth(path, storedAuth{
				BaseURL:        strings.TrimSpace(*baseURL),
				Token:          strings.TrimSpace(*token),
				RefreshToken:   tokenResponse.RefreshToken,
				TokenExpiresAt: tokenExpiry(tokenResponse.ExpiresIn),
			}); err != nil {
				return err
			}
			return outputJSON(stdout, []byte(`{"authenticated":true,"stored":true,"method":"device"}`))
		}
		if strings.TrimSpace(*token) == "" {
			return errors.New("token is required")
		}
		if _, err := marketplace.NewClient(*baseURL, *token); err != nil {
			return err
		}
		if err := saveStoredAuth(path, storedAuth{BaseURL: strings.TrimSpace(*baseURL), Token: strings.TrimSpace(*token)}); err != nil {
			return err
		}
		return outputJSON(stdout, []byte(`{"authenticated":true,"stored":true,"method":"token"}`))
	case "status":
		stored, err := loadStoredAuth(path)
		if err != nil {
			return err
		}
		source := "none"
		if firstEnv("DISTRIBUTION_TOKEN", "SPHERE_ACCESS_TOKEN") != "" {
			source = "environment"
		} else if stored.Token != "" {
			source = "config"
		}
		base := firstEnv("DISTRIBUTION_API_URL", "DISTRIBUTION_BASE_URL", "SOL_SYNTH_EXPRESS_URL")
		if base == "" {
			base = stored.BaseURL
		}
		if base == "" {
			base = defaultDistributionBaseURL
		}
		status, err := json.Marshal(map[string]any{
			"authenticated": source != "none",
			"base_url":      base,
			"token_source":  source,
			"token_expires": stored.TokenExpiresAt,
		})
		if err != nil {
			return err
		}
		return outputJSON(stdout, status)
	case "logout":
		if err := deleteStoredAuth(path); err != nil {
			return err
		}
		return outputJSON(stdout, []byte(`{"authenticated":false,"stored":false}`))
	default:
		return fmt.Errorf("unknown auth action %q; use login, status, or logout", action)
	}
}

func promptLine(out io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return "", err
	}
	line, err := promptReader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read prompt: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func promptSecret(out io.Writer, prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return promptLine(out, prompt)
	}
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return "", err
	}
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	return strings.TrimSpace(string(secret)), nil
}
