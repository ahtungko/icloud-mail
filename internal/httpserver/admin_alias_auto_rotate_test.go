package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"icloud-api/internal/domain"
	"icloud-api/internal/secure"
)

func TestAdminAPISetAliasAutoRotate(t *testing.T) {
	env := newAdminAPITestEnv(t)
	sessionCookie, csrf, _ := env.createSession(t, "auto-rotate-admin", "unused-password")
	account := adminAPITestCreateAccount(t, env, "auto-rotate-test@icloud.com")
	alias, _ := createV2AliasFixture(t, env, account.ID, "auto-rotate-test-alias@icloud.com")

	// 1. Set auto rotation to 60 minutes
	body := []byte(`{"duration_minutes": 60}`)
	path := "/admin/api/v1/aliases/" + strconv.FormatInt(alias.ID, 10) + "/auto-rotate"
	resp := env.request(t, http.MethodPost, path, body, "application/json", []*http.Cookie{sessionCookie}, csrf)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", resp.Code, resp.Body.String())
	}

	var result struct {
		Data adminAPIAliasDTO `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.AutoRotateDurationMinutes != 60 {
		t.Fatalf("expected 60 minutes, got %d", result.Data.AutoRotateDurationMinutes)
	}
	if result.Data.AutoRotateArmedAt == nil {
		t.Fatal("expected auto_rotate_armed_at to be non-nil")
	}
	if result.Data.AutoRotateDeadline != nil {
		t.Fatalf("expected auto_rotate_deadline nil before code, got %v", result.Data.AutoRotateDeadline)
	}

	// 2. Reject legacy alias
	rawKey := legacyCompatAPIKey(0x7c)
	legacyAlias, err := env.store.CreateAlias(context.Background(), domain.Alias{
		AccountID:      account.ID,
		Address:        "legacy-auto-rotate@icloud.com",
		APIKeyHash:     secure.HashToken(rawKey),
		APIKeyPrefix:   rawKey[:12],
		CredentialMode: domain.AliasCredentialModeLegacy,
		Enabled:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := "/admin/api/v1/aliases/" + strconv.FormatInt(legacyAlias.ID, 10) + "/auto-rotate"
	legacyResp := env.request(t, http.MethodPost, legacyPath, body, "application/json", []*http.Cookie{sessionCookie}, csrf)
	if legacyResp.Code != http.StatusConflict || adminAPITestErrorCode(t, legacyResp) != "ALIAS_CREDENTIAL_MODE_UNSUPPORTED" {
		t.Fatalf("expected 409 ALIAS_CREDENTIAL_MODE_UNSUPPORTED for legacy alias, got %d %s", legacyResp.Code, legacyResp.Body.String())
	}

	// 3. Reject invalid duration
	invalidResp := env.request(t, http.MethodPost, path, []byte(`{"duration_minutes": -1}`), "application/json", []*http.Cookie{sessionCookie}, csrf)
	if invalidResp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative duration, got %d", invalidResp.Code)
	}

	// 4. Disable auto rotation (0 minutes)
	disableResp := env.request(t, http.MethodPost, path, []byte(`{"duration_minutes": 0}`), "application/json", []*http.Cookie{sessionCookie}, csrf)
	if disableResp.Code != http.StatusOK {
		t.Fatalf("expected 200 on disable, got %d", disableResp.Code)
	}
	var disableResult struct {
		Data adminAPIAliasDTO `json:"data"`
	}
	if err := json.Unmarshal(disableResp.Body.Bytes(), &disableResult); err != nil {
		t.Fatal(err)
	}
	if disableResult.Data.AutoRotateDurationMinutes != 0 {
		t.Fatalf("expected 0 duration after disable, got %d", disableResult.Data.AutoRotateDurationMinutes)
	}
	if disableResult.Data.AutoRotateArmedAt != nil {
		t.Fatalf("expected armed_at nil after disable, got %v", disableResult.Data.AutoRotateArmedAt)
	}
}

func TestServerProcessDueAliasAutoRotations(t *testing.T) {
	env := newAdminAPITestEnv(t)
	account := adminAPITestCreateAccount(t, env, "due-test@icloud.com")
	alias, _ := createV2AliasFixture(t, env, account.ID, "due-alias@icloud.com")

	now := time.Now().UTC()
	env.server.now = func() time.Time { return now }

	// Arm for 30 minutes
	if _, err := env.store.SetAliasAutoRotate(context.Background(), alias.ID, 1800); err != nil {
		t.Fatal(err)
	}

	// Simulate triggered deadline in the past
	deadline := now.Add(-time.Minute)
	if _, err := env.store.DB().ExecContext(context.Background(),
		`UPDATE aliases SET auto_rotate_deadline = ? WHERE id = ?`,
		deadline.UnixNano(), alias.ID,
	); err != nil {
		t.Fatal(err)
	}

	// Execute processDueAliasAutoRotations
	env.server.processDueAliasAutoRotations(context.Background())

	// Verify alias credentials were automatically rotated and auto-rotate was cleared
	rotated, err := env.store.GetAlias(context.Background(), alias.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.CredentialVersion <= alias.CredentialVersion {
		t.Fatalf("expected credential version bumped, got %d <= %d", rotated.CredentialVersion, alias.CredentialVersion)
	}
	if rotated.AutoRotateDurationSeconds != 0 {
		t.Fatalf("expected duration reset to 0, got %d", rotated.AutoRotateDurationSeconds)
	}
	if rotated.AutoRotateDeadline != nil {
		t.Fatalf("expected deadline reset to nil, got %v", rotated.AutoRotateDeadline)
	}
}
