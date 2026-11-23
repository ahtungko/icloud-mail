package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"icloud-api/internal/domain"
)

func TestAdminAliasUpdateChangesEnabledAndGroupAtomically(t *testing.T) {
	env := newAdminAPITestEnv(t)
	cookie, csrf, _ := env.createSession(t, "atomic-group-admin", "atomic-group-password")
	account := adminAPITestCreateAccount(t, env, "atomic-group-owner@icloud.com")
	alias, err := env.store.CreateAlias(context.Background(), domain.Alias{
		AccountID: account.ID,
		Address:   "atomic-group-alias@icloud.com",
		Enabled:   false,
	})
	if err != nil {
		t.Fatalf("create disabled alias: %v", err)
	}
	group, err := env.store.CreateMailGroup(context.Background(), "事务测试")
	if err != nil {
		t.Fatalf("create mail group: %v", err)
	}
	target := "/admin/api/v1/aliases/" + strconv.FormatInt(alias.ID, 10)
	body := adminAPITestJSON(t, map[string]any{"enabled": true, "group_id": group.ID})

	response := env.request(t, http.MethodPatch, target, body,
		"application/json", []*http.Cookie{cookie}, csrf)
	if response.Code != http.StatusOK {
		t.Fatalf("combined alias update status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data adminAPIAliasDTO `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode combined alias update: %v", err)
	}
	if !payload.Data.Enabled || payload.Data.GroupID == nil || *payload.Data.GroupID != group.ID {
		t.Fatalf("combined alias update response = %#v", payload.Data)
	}

	if err := env.store.SetAliasEnabled(context.Background(), alias.ID, false); err != nil {
		t.Fatalf("reset alias enabled state: %v", err)
	}
	if _, err := env.store.SetAliasGroup(context.Background(), alias.ID, nil); err != nil {
		t.Fatalf("reset alias group: %v", err)
	}
	if _, err := env.store.DB().ExecContext(context.Background(), `
		CREATE TRIGGER reject_admin_alias_group_update
		BEFORE UPDATE OF group_id ON aliases
		BEGIN
			SELECT RAISE(ABORT, 'forced admin alias group failure');
		END`); err != nil {
		t.Fatalf("create alias group failure trigger: %v", err)
	}

	response = env.request(t, http.MethodPatch, target, body,
		"application/json", []*http.Cookie{cookie}, csrf)
	if response.Code != http.StatusInternalServerError || adminAPITestErrorCode(t, response) != "INTERNAL_ERROR" {
		t.Fatalf("failed combined alias update status = %d, body=%s", response.Code, response.Body.String())
	}

	got, err := env.store.GetAlias(context.Background(), alias.ID)
	if err != nil {
		t.Fatalf("read alias after failed update: %v", err)
	}
	if got.Enabled || got.GroupID != nil {
		t.Fatalf("alias was partially updated through API: enabled=%v group_id=%v", got.Enabled, got.GroupID)
	}
}

func TestAdminAliasUpdateCanMovePendingAliasWhileKeepingItDisabled(t *testing.T) {
	env := newAdminAPITestEnv(t)
	cookie, csrf, _ := env.createSession(t, "pending-group-admin", "pending-group-password")
	account := adminAPITestCreateAccount(t, env, "pending-group-owner@icloud.com")
	alias, err := env.store.CreateAlias(context.Background(), domain.Alias{
		AccountID: account.ID,
		Address:   "pending-group-alias@icloud.com",
		Enabled:   false,
	})
	if err != nil {
		t.Fatalf("create disabled alias: %v", err)
	}
	if _, err := env.store.DB().ExecContext(context.Background(), `
		UPDATE aliases SET last_sync_error = ? WHERE id = ?`,
		domain.AppleAliasConfirmationPending, alias.ID); err != nil {
		t.Fatalf("mark alias confirmation pending: %v", err)
	}
	group, err := env.store.CreateMailGroup(context.Background(), "待确认")
	if err != nil {
		t.Fatalf("create mail group: %v", err)
	}

	response := env.request(t, http.MethodPatch,
		"/admin/api/v1/aliases/"+strconv.FormatInt(alias.ID, 10),
		adminAPITestJSON(t, map[string]any{"enabled": false, "group_id": group.ID}),
		"application/json", []*http.Cookie{cookie}, csrf)
	if response.Code != http.StatusOK {
		t.Fatalf("move pending alias status = %d, body=%s", response.Code, response.Body.String())
	}

	got, err := env.store.GetAlias(context.Background(), alias.ID)
	if err != nil {
		t.Fatalf("read pending alias after group move: %v", err)
	}
	if got.Enabled || got.GroupID == nil || *got.GroupID != group.ID ||
		got.LastSyncError != domain.AppleAliasConfirmationPending {
		t.Fatalf("pending alias after group move = %#v", got)
	}
}

func TestAdminAliasUpdateLabelAndClear(t *testing.T) {
	env := newAdminAPITestEnv(t)
	cookie, csrf, _ := env.createSession(t, "label-admin", "label-password")
	account := adminAPITestCreateAccount(t, env, "label-owner@icloud.com")
	alias, err := env.store.CreateAlias(context.Background(), domain.Alias{AccountID: account.ID, Address: "label-alias@icloud.com", Label: "old", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	target := "/admin/api/v1/aliases/" + strconv.FormatInt(alias.ID, 10)
	for _, value := range []any{"送人了", ""} {
		response := env.request(t, http.MethodPatch, target, adminAPITestJSON(t, map[string]any{"label": value}), "application/json", []*http.Cookie{cookie}, csrf)
		if response.Code != http.StatusOK {
			t.Fatalf("label update status=%d body=%s", response.Code, response.Body.String())
		}
		got, readErr := env.store.GetAlias(context.Background(), alias.ID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if got.Label != value {
			t.Fatalf("label=%q want %q", got.Label, value)
		}
	}
	tooLong := strings.Repeat("x", 101)
	response := env.request(t, http.MethodPatch, target, adminAPITestJSON(t, map[string]any{"label": tooLong}), "application/json", []*http.Cookie{cookie}, csrf)
	if response.Code != http.StatusBadRequest || adminAPITestErrorCode(t, response) != "VALIDATION_FAILED" {
		t.Fatalf("long label status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminAliasUpdateLocalNoteAndClear(t *testing.T) {
	env := newAdminAPITestEnv(t)
	cookie, csrf, _ := env.createSession(t, "local-note-admin", "local-note-password")
	account := adminAPITestCreateAccount(t, env, "local-note-owner@icloud.com")
	alias, err := env.store.CreateAlias(context.Background(), domain.Alias{AccountID: account.ID, Address: "local-note-alias@icloud.com", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	target := "/admin/api/v1/aliases/" + strconv.FormatInt(alias.ID, 10)
	for _, value := range []string{"送人了", ""} {
		response := env.request(t, http.MethodPatch, target, adminAPITestJSON(t, map[string]any{"local_note": value}), "application/json", []*http.Cookie{cookie}, csrf)
		if response.Code != http.StatusOK {
			t.Fatalf("local note update status=%d body=%s", response.Code, response.Body.String())
		}
		got, readErr := env.store.GetAlias(context.Background(), alias.ID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if got.LocalNote != value {
			t.Fatalf("local_note=%q want %q", got.LocalNote, value)
		}
	}
	response := env.request(t, http.MethodPatch, target, adminAPITestJSON(t, map[string]any{"local_note": strings.Repeat("x", 101)}), "application/json", []*http.Cookie{cookie}, csrf)
	if response.Code != http.StatusBadRequest || adminAPITestErrorCode(t, response) != "VALIDATION_FAILED" {
		t.Fatalf("long local note status=%d body=%s", response.Code, response.Body.String())
	}
}
