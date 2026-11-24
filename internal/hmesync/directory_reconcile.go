package hmesync

import (
	"context"
	"errors"
	"time"

	"icloud-api/internal/apple"
	"icloud-api/internal/domain"
	"icloud-api/internal/store"
)

type directoryReconcileRepository interface {
	ListAliasesByAccount(context.Context, int64) ([]domain.Alias, error)
}

// Supplement a partial listing with positive entries from the other channel.
// An omitted address is never evidence for removing its local credentials.
func (s *Service) directoryForReconciliation(ctx context.Context, account domain.Account, startedAt time.Time, directory syncDirectory) (syncDirectory, error) {
	repo, ok := s.repo.(directoryReconcileRepository)
	if !ok {
		return directory, nil
	}
	local, err := repo.ListAliasesByAccount(ctx, account.ID)
	if err != nil {
		return directory, err
	}
	addresses := make(map[string]bool, len(directory.list.Aliases))
	for _, alias := range directory.list.Aliases {
		addresses[domain.NormalizeEmail(alias.HME)] = true
	}
	missing := false
	for _, alias := range local {
		if alias.CreatedAt.After(startedAt) || (!alias.Enabled && alias.LastSyncError == domain.AppleAliasConfirmationPending && alias.CreatedAt.After(startedAt.Add(-24*time.Hour))) {
			continue
		}
		if !addresses[domain.NormalizeEmail(alias.Address)] {
			missing = true
			break
		}
	}
	if !missing {
		return directory, nil
	}
	// A fallback means Web validation failed. Account absence alone must not
	// erase an address which may still be positive in the unavailable channel.
	if directory.fromAccount {
		return directory, ErrSessionExpired
	}
	if _, ok := s.client.(accountDirectoryLister); !ok {
		return directory, nil
	}
	if _, ok := s.repo.(accountSessionRepository); !ok {
		return directory, nil
	}
	managed, err := s.readAccountManagementSession(ctx, account.ID)
	if errors.Is(err, store.ErrNotFound) || Code(err) == CodeAccountSessionExpired || err == nil && (managed == nil || managed.RefreshRejected) {
		return directory, nil
	}
	if err != nil {
		return directory, err
	}
	secondary, err := s.readAccountDirectoryForSync(ctx, account, ErrLoginRequired)
	if err != nil {
		return directory, err
	}
	if _, _, err := filterAliases(secondary.list, account); err != nil {
		return directory, err
	}
	merged := directory
	merged.list.Aliases = append([]apple.Alias(nil), directory.list.Aliases...)
	for _, alias := range secondary.list.Aliases {
		address := domain.NormalizeEmail(alias.HME)
		if !addresses[address] {
			merged.list.Aliases = append(merged.list.Aliases, alias)
			addresses[address] = true
		}
	}
	if _, _, err := filterAliases(merged.list, account); err != nil {
		return directory, err
	}
	return merged, nil
}
