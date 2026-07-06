package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/matt0x6f/irc-client/internal/storage/generated"
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

// ListInviteActivity returns non-expired invite rows for a network, newest first.
func (s *Storage) ListInviteActivity(networkID int64, now time.Time) ([]ActivityItem, error) {
	rows, err := s.queries.ListInviteActivity(context.Background(), db.ListInviteActivityParams{
		NetworkID: networkID,
		ExpiresAt: sql.NullTime{Time: now.UTC(), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list invite activity: %w", err)
	}
	out := make([]ActivityItem, len(rows))
	for i, r := range rows {
		out[i] = convertActivityItemFromDB(r)
	}
	return out, nil
}

// DeleteInviteActivity removes a single invite row (e.g. once the invite is acted on).
func (s *Storage) DeleteInviteActivity(networkID int64, inviter, channel string) error {
	if err := s.queries.DeleteInviteActivity(context.Background(), db.DeleteInviteActivityParams{
		NetworkID: networkID, Actor: inviter, Target: channel,
	}); err != nil {
		return fmt.Errorf("delete invite activity: %w", err)
	}
	return nil
}

// DeleteInviteActivityFromSender removes all invite rows from a given inviter on a network.
func (s *Storage) DeleteInviteActivityFromSender(networkID int64, inviter string) error {
	if err := s.queries.DeleteInviteActivityFromSender(context.Background(), db.DeleteInviteActivityFromSenderParams{
		NetworkID: networkID, Actor: inviter,
	}); err != nil {
		return fmt.Errorf("delete invites from sender: %w", err)
	}
	return nil
}

// SweepExpiredInvites deletes expired invite rows and returns affected network IDs.
func (s *Storage) SweepExpiredInvites(now time.Time) ([]int64, error) {
	cutoff := sql.NullTime{Time: now.UTC(), Valid: true}
	ctx := context.Background()
	nets, err := s.queries.NetworksWithExpiredInvites(ctx, cutoff)
	if err != nil {
		return nil, fmt.Errorf("find expired invite networks: %w", err)
	}
	if err := s.queries.DeleteExpiredInviteActivity(ctx, cutoff); err != nil {
		return nil, fmt.Errorf("delete expired invites: %w", err)
	}
	return nets, nil
}
