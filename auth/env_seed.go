package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codex2api/database"
)

const (
	codexAsAPIAPIKeyEnv          = "CODEX_AS_API_API_KEY"
	codexAPIKeysEnv              = "CODEX_API_KEYS"
	codexAsAPIAuthSourceEnv      = "CODEX_AS_API_AUTH_SOURCE"
	codexAsAPIOpenClawAuthEnv    = "CODEX_AS_API_OPENCLAW_AUTH_PATH"
	codexAsAPIOpenClawStateEnv   = "CODEX_AS_API_OPENCLAW_STATE_PATH"
	codexAsAPIOpenClawProfileEnv = "CODEX_AS_API_OPENCLAW_PROFILE"
	defaultOpenClawAuthPath      = "~/.openclaw/agents/main/agent/auth-profiles.json"
)

type openClawProfile struct {
	Name      string
	Access    string
	Refresh   string
	AccountID string
	PlanType  string
	Email     string
	ExpiresAt time.Time
}

// SeedAPIKeysFromEnv preserves lightweight deployments that provide caller keys
// through environment variables instead of creating them from the admin UI.
func SeedAPIKeysFromEnv(ctx context.Context, db *database.DB) error {
	keys := envAPIKeys()
	if len(keys) == 0 {
		return nil
	}
	existingRows, err := db.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	existing := make(map[string]bool, len(existingRows))
	for _, row := range existingRows {
		existing[strings.TrimSpace(row.Key)] = true
	}

	inserted := 0
	for _, key := range keys {
		if existing[key] {
			continue
		}
		name := "env"
		if len(keys) > 1 {
			name = fmt.Sprintf("env-%d", inserted+1)
		}
		if _, err := db.InsertAPIKey(ctx, name, key); err != nil {
			return err
		}
		inserted++
	}
	if inserted > 0 {
		log.Printf("Seeded %d API key(s) from environment", inserted)
	}
	return nil
}

// ImportOpenClawFromEnv imports the OpenClaw-managed Codex OAuth profile once.
// Token refresh and request scheduling then use codex2api's regular account path.
func ImportOpenClawFromEnv(ctx context.Context, db *database.DB) error {
	if !useOpenClawEnv() {
		return nil
	}
	profile, err := loadOpenClawProfileFromEnv()
	if err != nil {
		return err
	}

	existingRows, err := db.ListActive(ctx)
	if err != nil {
		return err
	}
	credentials := openClawCredentials(profile)
	for _, row := range existingRows {
		if row.GetCredential("account_id") != profile.AccountID && row.GetCredential("chatgpt_account_id") != profile.AccountID {
			continue
		}
		if err := db.UpdateCredentials(ctx, row.ID, credentials); err != nil {
			return err
		}
		log.Printf("Synced OpenClaw Codex OAuth profile %s into existing account %d", profile.Name, row.ID)
		return nil
	}

	name := profile.Name
	if profile.Email != "" {
		name = "OpenClaw " + profile.Email
	}
	if _, err := db.InsertAccountWithCredentials(ctx, name, credentials, ""); err != nil {
		return err
	}
	log.Printf("Imported OpenClaw Codex OAuth profile %s into account store", profile.Name)
	return nil
}

func openClawCredentials(profile *openClawProfile) map[string]interface{} {
	credentials := map[string]interface{}{
		"auth_source":        "openclaw",
		"openclaw_profile":   profile.Name,
		"access_token":       profile.Access,
		"refresh_token":      profile.Refresh,
		"account_id":         profile.AccountID,
		"chatgpt_account_id": profile.AccountID,
	}
	if profile.PlanType != "" {
		credentials["plan_type"] = profile.PlanType
	}
	if profile.Email != "" {
		credentials["email"] = profile.Email
	}
	if !profile.ExpiresAt.IsZero() {
		credentials["expires_at"] = profile.ExpiresAt.Format(time.RFC3339)
	}
	return credentials
}

func envAPIKeys() []string {
	var values []string
	if key := strings.TrimSpace(os.Getenv(codexAsAPIAPIKeyEnv)); key != "" {
		values = append(values, key)
	}
	for _, key := range strings.Split(os.Getenv(codexAPIKeysEnv), ",") {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	sort.Strings(values)
	result := values[:0]
	seen := make(map[string]bool, len(values))
	for _, key := range values {
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, key)
	}
	return result
}

func useOpenClawEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(codexAsAPIAuthSourceEnv)), "openclaw") ||
		strings.TrimSpace(os.Getenv(codexAsAPIOpenClawAuthEnv)) != "" ||
		strings.TrimSpace(os.Getenv(codexAsAPIOpenClawProfileEnv)) != ""
}

func loadOpenClawProfileFromEnv() (*openClawProfile, error) {
	authPath := expandHome(firstNonEmpty(os.Getenv(codexAsAPIOpenClawAuthEnv), defaultOpenClawAuthPath))
	root, profiles, err := readOpenClawProfiles(authPath)
	if err != nil {
		return nil, err
	}
	name, err := selectOpenClawProfile(authPath, root, profiles)
	if err != nil {
		return nil, err
	}
	raw, ok := profiles[name].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("OpenClaw auth profile is invalid: %s", name)
	}
	profile := &openClawProfile{
		Name:      name,
		Access:    stringField(raw, "access"),
		Refresh:   stringField(raw, "refresh"),
		AccountID: stringField(raw, "accountId"),
		PlanType:  stringField(raw, "chatgptPlanType"),
		Email:     stringField(raw, "email"),
		ExpiresAt: expiresField(raw["expires"]),
	}
	for field, value := range map[string]string{
		"access":    profile.Access,
		"refresh":   profile.Refresh,
		"accountId": profile.AccountID,
	} {
		if value == "" {
			return nil, fmt.Errorf("OpenClaw auth profile %s %s is missing", name, field)
		}
	}
	return profile, nil
}

func readOpenClawProfiles(authPath string) (map[string]interface{}, map[string]interface{}, error) {
	raw, err := os.ReadFile(authPath)
	if err != nil {
		return nil, nil, err
	}
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, nil, fmt.Errorf("OpenClaw auth profile file is invalid JSON: %w", err)
	}
	profilesValue := root["profiles"]
	if profilesValue == nil {
		profilesValue = root
	}
	profiles, ok := profilesValue.(map[string]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("OpenClaw auth profiles must be an object")
	}
	return root, profiles, nil
}

func selectOpenClawProfile(authPath string, _ map[string]interface{}, profiles map[string]interface{}) (string, error) {
	if explicit := strings.TrimSpace(os.Getenv(codexAsAPIOpenClawProfileEnv)); explicit != "" {
		if _, ok := profiles[explicit]; !ok {
			return "", fmt.Errorf("OpenClaw auth profile not found: %s", explicit)
		}
		return explicit, nil
	}
	if profile := readOpenClawLastGoodProfile(authPath); profile != "" {
		if _, ok := profiles[profile]; ok {
			return profile, nil
		}
	}
	if order := readOpenClawOrderedProfiles(authPath); len(order) > 0 {
		for _, profile := range order {
			if _, ok := profiles[profile]; ok {
				return profile, nil
			}
		}
	}
	candidates := make([]string, 0, len(profiles))
	for name, value := range profiles {
		if strings.HasPrefix(name, "openai-codex:") && isOpenClawOAuthProfile(value) {
			candidates = append(candidates, name)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return "", fmt.Errorf("no OpenClaw openai-codex OAuth profile is available")
	}
	return candidates[0], nil
}

func readOpenClawLastGoodProfile(authPath string) string {
	state := readOpenClawState(authPath)
	if state == nil {
		return ""
	}
	return firstStringFromNestedMap(state["lastGood"], "openai-codex")
}

func readOpenClawOrderedProfiles(authPath string) []string {
	state := readOpenClawState(authPath)
	if state == nil {
		return nil
	}
	return stringSliceFromNestedMap(state["order"], "openai-codex")
}

func readOpenClawState(authPath string) map[string]interface{} {
	statePath := expandHome(os.Getenv(codexAsAPIOpenClawStateEnv))
	if statePath == "" {
		statePath = filepath.Join(filepath.Dir(authPath), "auth-state.json")
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		return nil
	}
	var state map[string]interface{}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil
	}
	return state
}

func isOpenClawOAuthProfile(value interface{}) bool {
	profile, ok := value.(map[string]interface{})
	if !ok {
		return false
	}
	return stringField(profile, "provider") == "openai-codex" &&
		stringField(profile, "type") == "oauth" &&
		stringField(profile, "access") != "" &&
		stringField(profile, "refresh") != ""
}

func stringField(values map[string]interface{}, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func firstStringFromNestedMap(value interface{}, key string) string {
	values, ok := value.(map[string]interface{})
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringField(values, key))
}

func stringSliceFromNestedMap(value interface{}, key string) []string {
	values, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := values[key].([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}

func expiresField(value interface{}) time.Time {
	var raw float64
	switch typed := value.(type) {
	case float64:
		raw = typed
	case int64:
		raw = float64(typed)
	case int:
		raw = float64(typed)
	default:
		return time.Time{}
	}
	if raw <= 0 {
		return time.Time{}
	}
	if raw < 1e12 {
		return time.Unix(int64(raw), 0).UTC()
	}
	return time.UnixMilli(int64(raw)).UTC()
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
