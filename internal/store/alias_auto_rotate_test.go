package store_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"icloud-api/internal/domain"
	"icloud-api/internal/secure"
)

func TestAliasAutoRotateLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	cipher, err := secure.NewCipher(bytes.Repeat([]byte{0x77}, 32))
	if err != nil {
		t.Fatal(err)
	}
	db.ConfigureAliasCredentialFactory(func(aliasID, version int64) (domain.AliasCredentialMaterial, error) {
		_, material, issueErr := secure.NewAliasCredentialMaterial(cipher, aliasID, version)
		return material, issueErr
	})

	account := createAccount(t, ctx, db, "Auto Rotate", "auto-rotate@icloud.com")
	if err := db.ConfigureMailArchive(t.TempDir(), 1<<20); err != nil {
		t.Fatal(err)
	}
	alias := createAlias(t, ctx, db, account.ID, "auto-rotate-alias@icloud.com", bytes.Repeat([]byte{0x11}, 32))
	if _, err := db.DB().ExecContext(ctx, `UPDATE aliases SET credential_mode = 'v2' WHERE id = ?`, alias.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAliasCredentials(ctx); err != nil {
		t.Fatal(err)
	}

	baseTime := time.Date(2026, 11, 17, 15, 1, 0, 0, time.UTC) // 3:01
	db.SetNowForTest(func() time.Time { return baseTime })

	// 1. Arm auto-rotation for 3600 seconds (1 hour) at 3:01
	updated, err := db.SetAliasAutoRotate(ctx, alias.ID, 3600)
	if err != nil {
		t.Fatalf("SetAliasAutoRotate failed: %v", err)
	}
	if updated.AutoRotateDurationSeconds != 3600 {
		t.Fatalf("expected duration 3600, got %d", updated.AutoRotateDurationSeconds)
	}
	if updated.AutoRotateArmedAt == nil || !updated.AutoRotateArmedAt.Equal(baseTime) {
		t.Fatalf("expected armed_at %v, got %v", baseTime, updated.AutoRotateArmedAt)
	}
	if updated.AutoRotateDeadline != nil {
		t.Fatalf("expected deadline nil before receiving code, got %v", updated.AutoRotateDeadline)
	}

	// 2. Incoming OTP before armed time (e.g., historical code at 3:00) should NOT trigger deadline
	oldOTPTime := time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC)
	aliases, err := db.ListAliases(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	applyArchiveV2Batch(t, ctx, db, account.ID, aliases, []domain.ArchivedMessage{
		{
			AccountID:    account.ID,
			UIDValidity:  1,
			UID:          100,
			MessageID:    "<msg-1@test.com>",
			InternalDate: oldOTPTime,
			HeaderDate:   &oldOTPTime,
			Subject:      "Your code is 123456",
			AliasIDs:     []int64{alias.ID},
			OTP:          "123456",
		},
	}, 1, 100, true)

	aliasAfterOldOTP, err := db.GetAlias(ctx, alias.ID)
	if err != nil {
		t.Fatal(err)
	}
	if aliasAfterOldOTP.AutoRotateDeadline != nil {
		t.Fatalf("expected deadline still nil after old OTP, but was %v", aliasAfterOldOTP.AutoRotateDeadline)
	}

	// 3. New OTP received at 3:20 should trigger countdown
	codeTime := time.Date(2026, 11, 17, 15, 20, 0, 0, time.UTC)
	db.SetNowForTest(func() time.Time { return codeTime })

	aliases, err = db.ListAliases(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	applyArchiveV2Batch(t, ctx, db, account.ID, aliases, []domain.ArchivedMessage{
		{
			AccountID:    account.ID,
			UIDValidity:  1,
			UID:          101,
			MessageID:    "<msg-2@test.com>",
			InternalDate: codeTime,
			HeaderDate:   &codeTime,
			Subject:      "Your code is 654321",
			AliasIDs:     []int64{alias.ID},
			OTP:          "654321",
		},
	}, 1, 101, false)

	aliasAfterCode, err := db.GetAlias(ctx, alias.ID)
	if err != nil {
		t.Fatal(err)
	}
	expectedDeadline := codeTime.Add(time.Hour) // 4:20
	if aliasAfterCode.AutoRotateDeadline == nil || !aliasAfterCode.AutoRotateDeadline.Equal(expectedDeadline) {
		t.Fatalf("expected deadline %v, got %v", expectedDeadline, aliasAfterCode.AutoRotateDeadline)
	}

	// 4. Another OTP arrives at 3:40: should NOT extend the deadline (Fixed window policy)
	secondCodeTime := time.Date(2026, 11, 17, 15, 40, 0, 0, time.UTC)
	db.SetNowForTest(func() time.Time { return secondCodeTime })

	aliases, err = db.ListAliases(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	applyArchiveV2Batch(t, ctx, db, account.ID, aliases, []domain.ArchivedMessage{
		{
			AccountID:    account.ID,
			UIDValidity:  1,
			UID:          102,
			MessageID:    "<msg-3@test.com>",
			InternalDate: secondCodeTime,
			HeaderDate:   &secondCodeTime,
			Subject:      "Your code is 888999",
			AliasIDs:     []int64{alias.ID},
			OTP:          "888999",
		},
	}, 1, 102, false)

	aliasAfterSecondCode, err := db.GetAlias(ctx, alias.ID)
	if err != nil {
		t.Fatal(err)
	}
	if aliasAfterSecondCode.AutoRotateDeadline == nil || !aliasAfterSecondCode.AutoRotateDeadline.Equal(expectedDeadline) {
		t.Fatalf("expected deadline fixed at %v, but got %v", expectedDeadline, aliasAfterSecondCode.AutoRotateDeadline)
	}

	// 5. Query due aliases: at 4:19 not due; at 4:20 due!
	dueBefore, err := db.ListDueAutoRotateAliases(ctx, expectedDeadline.Add(-time.Second), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(dueBefore) != 0 {
		t.Fatalf("expected 0 due before deadline, got %d", len(dueBefore))
	}

	dueAt, err := db.ListDueAutoRotateAliases(ctx, expectedDeadline, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(dueAt) != 1 || dueAt[0].ID != alias.ID {
		t.Fatalf("expected 1 due at deadline, got %d", len(dueAt))
	}

	// 6. Rotate credentials: rotates bundle and clears auto-rotate fields (单次生效)
	rotated, err := db.RotateAliasCredentials(ctx, alias.ID)
	if err != nil {
		t.Fatalf("RotateAliasCredentials failed: %v", err)
	}
	if rotated.CredentialVersion <= aliasAfterSecondCode.CredentialVersion {
		t.Fatalf("expected version bumped, got %d", rotated.CredentialVersion)
	}
	if rotated.AutoRotateDurationSeconds != 0 {
		t.Fatalf("expected auto_rotate_duration_seconds reset to 0, got %d", rotated.AutoRotateDurationSeconds)
	}
	if rotated.AutoRotateArmedAt != nil {
		t.Fatalf("expected armed_at reset to nil, got %v", rotated.AutoRotateArmedAt)
	}
	if rotated.AutoRotateDeadline != nil {
		t.Fatalf("expected deadline reset to nil, got %v", rotated.AutoRotateDeadline)
	}

	// Verify it is no longer due
	dueAfterRotation, err := db.ListDueAutoRotateAliases(ctx, expectedDeadline.Add(time.Hour), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(dueAfterRotation) != 0 {
		t.Fatalf("expected 0 due after rotation, got %d", len(dueAfterRotation))
	}
}
