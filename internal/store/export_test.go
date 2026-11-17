package store

import "time"

// SetNowForTest overrides the internal clock during store_test package tests.
func (s *Store) SetNowForTest(now func() time.Time) {
	s.now = now
}
