package api

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"bop/internal/config"
)

// LoadTokens resolves the read-scoped bearer tokens the API will accept,
// from whichever of cfg.TokensFile/cfg.TokenEnv is set
// (config.Config.Validate already enforces exactly one when cfg.Enabled).
// TokensFile supports one token per line, blank lines and #-prefixed
// comments ignored, so an operator can list multiple tokens (e.g. one per
// consumer, for independent rotation); TokenEnv holds exactly one, the
// same file-or-env choice every other BOP secret offers.
func LoadTokens(cfg config.APIConfig) ([]string, error) {
	if cfg.TokenEnv != "" {
		token := os.Getenv(cfg.TokenEnv)
		if token == "" {
			return nil, fmt.Errorf("api: environment variable %q (token_env) is not set", cfg.TokenEnv)
		}
		return []string{token}, nil
	}
	return loadTokenFile(cfg.TokensFile, "tokens_file")
}

// LoadWriteTokens resolves the write-scoped bearer tokens (see
// config.APIConfig's doc comment: separate from, not a role on,
// TokensFile/TokenEnv). Unlike LoadTokens, both fields are optional -
// returning (nil, nil) when neither is set means "no write tokens
// configured," which POST-style mutating endpoints must treat as
// "nobody can call this," not "anyone can."
func LoadWriteTokens(cfg config.APIConfig) ([]string, error) {
	if cfg.WriteTokenEnv == "" && cfg.WriteTokensFile == "" {
		return nil, nil
	}
	if cfg.WriteTokenEnv != "" {
		token := os.Getenv(cfg.WriteTokenEnv)
		if token == "" {
			return nil, fmt.Errorf("api: environment variable %q (write_token_env) is not set", cfg.WriteTokenEnv)
		}
		return []string{token}, nil
	}
	return loadTokenFile(cfg.WriteTokensFile, "write_tokens_file")
}

func loadTokenFile(path, fieldName string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("api: open %s: %w", fieldName, err)
	}
	defer f.Close()

	var tokens []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tokens = append(tokens, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("api: read %s: %w", fieldName, err)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("api: %s %q contains no tokens", fieldName, path)
	}
	return tokens, nil
}
