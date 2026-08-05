package api

import (
	"os"
	"path/filepath"
	"testing"

	"bop/internal/config"
)

func TestLoadTokensFromEnv(t *testing.T) {
	t.Setenv("BOP_TEST_API_TOKEN", "secret-token")
	tokens, err := LoadTokens(config.APIConfig{TokenEnv: "BOP_TEST_API_TOKEN"})
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0] != "secret-token" {
		t.Errorf("LoadTokens = %v, want [secret-token]", tokens)
	}
}

func TestLoadTokensFromEnvUnset(t *testing.T) {
	_, err := LoadTokens(config.APIConfig{TokenEnv: "BOP_TEST_API_TOKEN_UNSET"})
	if err == nil {
		t.Fatal("LoadTokens: expected an error for an unset token_env, got nil")
	}
}

func TestLoadTokensFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.txt")
	writeFile(t, path, "token-one\n# a comment\n\ntoken-two\n")

	tokens, err := LoadTokens(config.APIConfig{TokensFile: path})
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if len(tokens) != 2 || tokens[0] != "token-one" || tokens[1] != "token-two" {
		t.Errorf("LoadTokens = %v, want [token-one token-two]", tokens)
	}
}

func TestLoadTokensFromFileMissing(t *testing.T) {
	_, err := LoadTokens(config.APIConfig{TokensFile: filepath.Join(t.TempDir(), "does-not-exist.txt")})
	if err == nil {
		t.Fatal("LoadTokens: expected an error for a missing tokens_file, got nil")
	}
}

func TestLoadTokensFromFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.txt")
	writeFile(t, path, "# only comments\n\n")

	_, err := LoadTokens(config.APIConfig{TokensFile: path})
	if err == nil {
		t.Fatal("LoadTokens: expected an error for a tokens_file with no actual tokens, got nil")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
