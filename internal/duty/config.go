package duty

import (
	"fmt"
	"os"
	"strings"
)

// Config controls the duty agent's model connection. It is resolved by the
// main package from CLI flags, environment, and config.yaml using the same
// precedence as the rest of dockerview (CLI > env > yaml > default).
type Config struct {
	// Enabled toggles the duty agent HTTP endpoints.
	Enabled bool
	// Provider is always "openai-compatible" in this build. The OpenAI
	// official plugin is a special case of the compatible plugin.
	Provider string
	// BaseURL is the OpenAI-compatible endpoint. Defaults to
	// https://api.openai.com/v1 and can point at an internal gateway.
	BaseURL string
	// Model is the model name sent to the endpoint (e.g. gpt-4o-mini).
	Model string
	// APIKey is the resolved key. Never read from yaml directly.
	APIKey string
	// APIKeyFile is a path to a 0600 file containing the key.
	APIKeyFile string
}

// DefaultConfig returns the built-in defaults.
func DefaultConfig() Config {
	return Config{
		Provider: "openai-compatible",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4o-mini",
	}
}

// ResolveKey loads the API key from Config.APIKey, Config.APIKeyFile, or the
// OPENAI_API_KEY / DOCKERVIEW_AGENT_API_KEY environment variables. It returns
// "" when no key is configured, which signals fake/drill mode.
func (c *Config) ResolveKey() (string, error) {
	if c.APIKey != "" {
		return strings.TrimSpace(c.APIKey), nil
	}
	if c.APIKeyFile != "" {
		b, err := os.ReadFile(c.APIKeyFile)
		if err != nil {
			return "", fmt.Errorf("duty: read api_key_file: %w", err)
		}
		k := strings.TrimSpace(strings.TrimRight(string(b), "\r\n"))
		if k == "" {
			return "", fmt.Errorf("duty: api_key_file %q is empty", c.APIKeyFile)
		}
		return k, nil
	}
	if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(os.Getenv("DOCKERVIEW_AGENT_API_KEY")); v != "" {
		return v, nil
	}
	return "", nil
}

// Mode returns "live" when a key is available, "fake" otherwise.
func (c *Config) Mode() string {
	k, _ := c.ResolveKey()
	if k != "" {
		return "live"
	}
	return "fake"
}
