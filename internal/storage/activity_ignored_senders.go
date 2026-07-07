package storage

import (
	"context"
	"fmt"

	db "github.com/matt0x6f/irc-client/internal/storage/generated"
)

// IgnoredSenderRow is one ignored sender annotated with its network name, for
// the settings UI's grouped list.
type IgnoredSenderRow struct {
	NetworkID   int64  `json:"networkId"`
	NetworkName string `json:"networkName"`
	Nick        string `json:"nick"`
}

// AddIgnoredSender adds a nick to a network's Activity ignore list (idempotent).
func (s *Storage) AddIgnoredSender(networkID int64, nick string) error {
	if err := s.queries.AddIgnoredSender(context.Background(), db.AddIgnoredSenderParams{
		NetworkID: networkID,
		Nick:      nick,
	}); err != nil {
		return fmt.Errorf("failed to add ignored sender %q: %w", nick, err)
	}
	return nil
}

// RemoveIgnoredSender removes a nick from a network's Activity ignore list.
func (s *Storage) RemoveIgnoredSender(networkID int64, nick string) error {
	if err := s.queries.RemoveIgnoredSender(context.Background(), db.RemoveIgnoredSenderParams{
		NetworkID: networkID,
		Nick:      nick,
	}); err != nil {
		return fmt.Errorf("failed to remove ignored sender %q: %w", nick, err)
	}
	return nil
}

// ListIgnoredSendersByNetwork returns a network's ignored nicks, sorted.
func (s *Storage) ListIgnoredSendersByNetwork(networkID int64) ([]string, error) {
	rows, err := s.queries.ListIgnoredSendersByNetwork(context.Background(), networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to list ignored senders: %w", err)
	}
	return rows, nil
}

// ListAllIgnoredSenders returns every ignored sender annotated with its network
// name, ordered by network then nick.
func (s *Storage) ListAllIgnoredSenders() ([]IgnoredSenderRow, error) {
	rows, err := s.queries.ListAllIgnoredSenders(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list all ignored senders: %w", err)
	}
	out := make([]IgnoredSenderRow, len(rows))
	for i, r := range rows {
		out[i] = IgnoredSenderRow{NetworkID: r.NetworkID, NetworkName: r.NetworkName, Nick: r.Nick}
	}
	return out, nil
}

// IsSenderIgnored reports whether nick is on networkID's ignore list
// (case-insensitive).
func (s *Storage) IsSenderIgnored(networkID int64, nick string) (bool, error) {
	n, err := s.queries.CountIgnoredSender(context.Background(), db.CountIgnoredSenderParams{
		NetworkID: networkID,
		Nick:      nick,
	})
	if err != nil {
		return false, fmt.Errorf("failed to check ignored sender %q: %w", nick, err)
	}
	return n > 0, nil
}
