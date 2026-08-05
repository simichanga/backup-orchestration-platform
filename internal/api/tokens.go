package api

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"bop/internal/config"
)

// LoadTokens resolves the bearer tokens the API will accept, from whichever
// of cfg.TokensFile/cfg.TokenEnv is set (config.Config.Validate already
// enforces exactly one when cfg.Enabled). TokensFile supports one token per
// line, blank lines and #-prefixed comments ignored, so an operator can
// list multiple tokens (e.g. one per consumer, for independent rotation);
// TokenEnv holds exactly one, the same file-or-env choice every other BOP
// secret offers.
func LoadTokens(cfg config.APIConfig) ([]string, error) {
	if cfg.TokenEnv != "" {
		token := os.Getenv(cfg.TokenEnv)
		if token == "" {
			return nil, fmt.Errorf("api: environment variable %q (token_env) is not set", cfg.TokenEnv)
		}
		return []string{token}, nil
	}

	f, err := os.Open(cfg.TokensFile)
	if err != nil {
		return nil, fmt.Errorf("api: open tokens_file: %w", err)
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
		return nil, fmt.Errorf("api: read tokens_file: %w", err)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("api: tokens_file %q contains no tokens", cfg.TokensFile)
	}
	return tokens, nil
}
