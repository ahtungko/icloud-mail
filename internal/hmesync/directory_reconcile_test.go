package hmesync

import (
	"context"
	"fmt"
	"testing"

	"icloud-api/internal/apple"
	"icloud-api/internal/domain"
	"icloud-api/internal/secure"
)

func TestSyncDirectoryReconcilesMissingAndPreservesOtherChannelPositive(t *testing.T) {
	for _, scenario := range []string{"success", "both-empty", "secondary-fails", "secondary-wrong-owner"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			s, db, account, _, _ := directoryServiceFixture(t)
			saveDirectoryWeb(t, s, account)
			missing, err := db.CreateAlias(ctx, domain.Alias{AccountID: account.ID, Address: "phantom@icloud.com", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			onlyAccount, err := db.CreateAlias(ctx, domain.Alias{AccountID: account.ID, Address: "account-only@icloud.com", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			reads := 0
			s.client = &directoryTestClient{fakeAppleClient: &fakeAppleClient{
				validate: func(_ context.Context, web apple.Session) (apple.Session, error) { return web, nil },
				list: func(_ context.Context, web apple.Session) (apple.ListResult, apple.Session, error) {
					list := ownedDirectory(account)
					if scenario == "both-empty" {
						list.Aliases = []apple.Alias{}
					}
					return list, web, nil
				},
			}, read: func(_ context.Context, managed apple.AccountSession) (apple.ListResult, apple.AccountSession, error) {
				reads++
				if scenario == "secondary-fails" {
					return apple.ListResult{}, managed, &apple.Error{Kind: apple.ErrService, StatusCode: 503}
				}
				list := ownedDirectory(account)
				if scenario == "both-empty" {
					list.Aliases = []apple.Alias{}
					return list, managed, nil
				}
				if scenario == "secondary-wrong-owner" {
					list.SelectedForwardTo = "different@icloud.com"
					list.ForwardToEmails = nil
					list.Aliases = nil
					return list, managed, nil
				}
				list.Aliases = append(list.Aliases, apple.Alias{HME: onlyAccount.Address, AnonymousID: "account-only-id", ForwardToEmail: account.Email, IsActive: true})
				return list, managed, nil
			}}
			result, err := s.SyncAliases(ctx, account.ID)
			if err != nil || reads != 1 {
				t.Fatalf("sync err=%v reads=%d", err, reads)
			}
			if _, err := db.GetAlias(ctx, onlyAccount.ID); err != nil {
				t.Fatal("positive entry in other channel removed")
			}
			_, absentErr := db.GetAlias(ctx, missing.ID)
			if scenario == "both-empty" {
				if absentErr != nil || result.Summary.RemovedCount != 0 || result.Summary.Total != 0 || result.Summary.DirectoryWarning != "" {
					t.Fatalf("empty directories not reconciled: %+v err=%v", result.Summary, absentErr)
				}
			} else if scenario == "success" {
				if absentErr != nil || result.Summary.RemovedCount != 0 || result.Summary.Total != 3 || result.Summary.DirectoryWarning != "" {
					t.Fatalf("missing not reconciled: %+v err=%v", result.Summary, absentErr)
				}
			} else {
				if absentErr != nil || result.Summary.RemovedCount != 0 || result.Summary.DirectoryWarning != "APPLE_DIRECTORY_RECONCILIATION_PENDING" {
					t.Fatalf("failed read removed address: %+v err=%v", result.Summary, absentErr)
				}
				if len(result.Created) != 2 || result.Created[0].APIKey == "" {
					t.Fatal("secondary failure discarded successfully imported credentials")
				}
			}
		})
	}
}

func TestPartialDirectoryPreserves154OTPLinks(t *testing.T) {
	ctx := context.Background()
	s, db, account, _, _ := directoryServiceFixture(t)
	account.IMAPUsername = "receiver@gmail.com"
	account.IMAPHost = "imap.gmail.com"
	account, err := db.UpdateAccount(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	saveDirectoryWeb(t, s, account)
	cipher := s.cipher.(*secure.Cipher)
	tokens := make(map[int64]string)
	for i := 0; i < 154; i++ {
		a, err := db.CreateAlias(ctx, domain.Alias{AccountID: account.ID, Address: fmt.Sprintf("old%d@icloud.com", i), Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		tokens[a.ID], err = cipher.OTPToken(a.ID, a.APIKeyHash)
		if err != nil {
			t.Fatal(err)
		}
	}
	s.client = &fakeAppleClient{
		validate: func(_ context.Context, session apple.Session) (apple.Session, error) { return session, nil },
		list: func(_ context.Context, session apple.Session) (apple.ListResult, apple.Session, error) {
			return apple.ListResult{SelectedForwardTo: account.IMAPUsername, Aliases: []apple.Alias{{AnonymousID: "phone", HME: "phone@icloud.com", ForwardToEmail: account.IMAPUsername, IsActive: true}}}, session, nil
		},
	}
	for i := 0; i < 2; i++ {
		result, err := s.SyncAliases(ctx, account.ID)
		if err != nil || result.Summary.CreatedCount != 1-i || result.Summary.RemovedCount != 0 {
			t.Fatalf("sync %d: %+v %v", i, result.Summary, err)
		}
	}
	for id, token := range tokens {
		a, err := db.GetAlias(ctx, id)
		if err != nil || !a.Enabled || !cipher.VerifyOTPToken(token, a.ID, a.APIKeyHash) {
			t.Fatalf("old link %d invalid after sync: %v", id, err)
		}
	}
}
