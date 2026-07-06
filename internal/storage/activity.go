package storage

import (
	"context"
	"fmt"
)

// WriteActivityItem inserts one activity row and returns it with its assigned ID.
// Activity items are low-volume, so this writes directly (no batch buffer).
func (s *Storage) WriteActivityItem(item ActivityItem) (ActivityItem, error) {
	s.closedMu.RLock()
	closed := s.closed
	s.closedMu.RUnlock()
	if closed {
		return ActivityItem{}, fmt.Errorf("storage is closed")
	}
	row, err := s.queries.CreateActivityItem(context.Background(), convertActivityItemToCreateParams(item))
	if err != nil {
		return ActivityItem{}, fmt.Errorf("create activity item: %w", err)
	}
	return convertActivityItemFromDB(row), nil
}

// ListActivityItems returns up to limit items, newest first.
func (s *Storage) ListActivityItems(limit int) ([]ActivityItem, error) {
	rows, err := s.queries.ListActivityItems(context.Background(), int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list activity items: %w", err)
	}
	out := make([]ActivityItem, len(rows))
	for i, r := range rows {
		out[i] = convertActivityItemFromDB(r)
	}
	return out, nil
}

func (s *Storage) MarkActivitySeen(id int64) error {
	if err := s.queries.MarkActivityItemSeen(context.Background(), id); err != nil {
		return fmt.Errorf("mark activity seen: %w", err)
	}
	return nil
}

func (s *Storage) MarkAllActivitySeen() error {
	if err := s.queries.MarkAllActivityItemsSeen(context.Background()); err != nil {
		return fmt.Errorf("mark all activity seen: %w", err)
	}
	return nil
}

func (s *Storage) DismissActivity(id int64) error {
	if err := s.queries.DeleteActivityItem(context.Background(), id); err != nil {
		return fmt.Errorf("dismiss activity: %w", err)
	}
	return nil
}

func (s *Storage) ClearSeenActivity() error {
	if err := s.queries.DeleteSeenActivityItems(context.Background()); err != nil {
		return fmt.Errorf("clear seen activity: %w", err)
	}
	return nil
}

func (s *Storage) ClearAllActivity() error {
	if err := s.queries.DeleteAllActivityItems(context.Background()); err != nil {
		return fmt.Errorf("clear all activity: %w", err)
	}
	return nil
}
