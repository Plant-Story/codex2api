package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codex2api/database"
)

func TestSeedAPIKeysFromEnvUsesCodexAsAPIAlias(t *testing.T) {
	db := newSeedTestDB(t)
	t.Setenv(codexAsAPIAPIKeyEnv, "sk-palmer-test")
	t.Setenv(codexAPIKeysEnv, "sk-extra, sk-palmer-test")

	if err := SeedAPIKeysFromEnv(context.Background(), db); err != nil {
		t.Fatalf("SeedAPIKeysFromEnv() error = %v", err)
	}

	rows, err := db.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("seeded key count = %d, want 2", len(rows))
	}
	if err := SeedAPIKeysFromEnv(context.Background(), db); err != nil {
		t.Fatalf("SeedAPIKeysFromEnv() second call error = %v", err)
	}
	count, err := db.CountAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("CountAPIKeys() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("key count after second seed = %d, want 2", count)
	}
}

func TestImportOpenClawFromEnvUsesLastGoodProfile(t *testing.T) {
	db := newSeedTestDB(t)
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth-profiles.json")
	statePath := filepath.Join(dir, "auth-state.json")
	writeJSON(t, authPath, map[string]interface{}{
		"version": 1,
		"profiles": map[string]interface{}{
			"openai-codex:first@example.com": map[string]interface{}{
				"provider":        "openai-codex",
				"type":            "oauth",
				"access":          "access-first",
				"refresh":         "refresh-first",
				"accountId":       "acct-first",
				"chatgptPlanType": "plus",
				"email":           "first@example.com",
			},
			"openai-codex:last@example.com": map[string]interface{}{
				"provider":        "openai-codex",
				"type":            "oauth",
				"access":          "access-last",
				"refresh":         "refresh-last",
				"accountId":       "acct-last",
				"chatgptPlanType": "pro",
				"email":           "last@example.com",
				"expires":         float64(9999999999999),
			},
		},
	})
	writeJSON(t, statePath, map[string]interface{}{
		"version": 1,
		"lastGood": map[string]interface{}{
			"openai-codex": "openai-codex:last@example.com",
		},
	})
	t.Setenv(codexAsAPIAuthSourceEnv, "openclaw")
	t.Setenv(codexAsAPIOpenClawAuthEnv, authPath)
	t.Setenv(codexAsAPIOpenClawStateEnv, statePath)

	if err := ImportOpenClawFromEnv(context.Background(), db); err != nil {
		t.Fatalf("ImportOpenClawFromEnv() error = %v", err)
	}

	rows, err := db.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("account count = %d, want 1", len(rows))
	}
	row := rows[0]
	if got := row.GetCredential("openclaw_profile"); got != "openai-codex:last@example.com" {
		t.Fatalf("openclaw_profile = %q, want last profile", got)
	}
	if got := row.GetCredential("access_token"); got != "access-last" {
		t.Fatalf("access_token = %q, want access-last", got)
	}
	if got := row.GetCredential("refresh_token"); got != "refresh-last" {
		t.Fatalf("refresh_token = %q, want refresh-last", got)
	}
	if got := row.GetCredential("account_id"); got != "acct-last" {
		t.Fatalf("account_id = %q, want acct-last", got)
	}

	writeJSON(t, authPath, map[string]interface{}{
		"version": 1,
		"profiles": map[string]interface{}{
			"openai-codex:last@example.com": map[string]interface{}{
				"provider":        "openai-codex",
				"type":            "oauth",
				"access":          "access-new",
				"refresh":         "refresh-new",
				"accountId":       "acct-last",
				"chatgptPlanType": "pro",
				"email":           "last@example.com",
			},
		},
	})
	if err := ImportOpenClawFromEnv(context.Background(), db); err != nil {
		t.Fatalf("ImportOpenClawFromEnv() second call error = %v", err)
	}
	rows, err = db.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive() second call error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("account count after second import = %d, want 1", len(rows))
	}
	if got := rows[0].GetCredential("access_token"); got != "access-new" {
		t.Fatalf("access_token after second import = %q, want access-new", got)
	}
	if got := rows[0].GetCredential("refresh_token"); got != "refresh-new" {
		t.Fatalf("refresh_token after second import = %q, want refresh-new", got)
	}
}

func newSeedTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.New("sqlite", filepath.Join(t.TempDir(), "codex2api.db"))
	if err != nil {
		t.Fatalf("database.New() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func writeJSON(t *testing.T, path string, value interface{}) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
