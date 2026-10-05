package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	db "github.com/matt0x6f/irc-client/internal/storage/generated"
)

func (s *Storage) SetPMContactTarget(networkID, id int64, nick, nickKey string) error {
	return s.queries.SetPMContactTarget(context.Background(), db.SetPMContactTargetParams{TargetUser: nick, NicknameKey: nickKey, NetworkID: networkID, ID: id})
}

// FindPMContactByAccount looks up historical ownership without rebinding a
// nickname or claiming that the account has a current session.
func (s *Storage) FindPMContactByAccount(networkID int64, account string) (*PrivateMessageConversation, error) {
	row, err := s.queries.GetPMConversationByAccount(context.Background(), db.GetPMConversationByAccountParams{NetworkID: networkID, Account: account})
	if err != nil {
		return nil, err
	}
	contact := convertPMConversationFromDB(row)
	return &contact, nil
}

func (s *Storage) GetUnboundPMContacts(networkID int64) ([]PrivateMessageConversation, error) {
	rows, err := s.queries.GetUnboundPMContacts(context.Background(), networkID)
	if err != nil {
		return nil, err
	}
	contacts := make([]PrivateMessageConversation, len(rows))
	for i, row := range rows {
		contacts[i] = convertPMConversationFromDB(row)
	}
	return contacts, nil
}

// ReconcilePMIdentity consumes server-verified evidence. Account names are
// compared exactly as returned by the server; IRC nickname folding is separate.
// Observations don't create chats unless create is true (a message/open action).
// Existing account-bound contacts never acquire a different account by nick.
func (s *Storage) ReconcilePMIdentity(networkID int64, nick, nickKey, account, source string, create bool) (*PrivateMessageConversation, bool, error) {
	s.contactMu.Lock()
	defer s.contactMu.Unlock()
	ctx := context.Background()
	if account != "" {
		row, err := s.queries.GetPMConversationByAccount(ctx, db.GetPMConversationByAccountParams{NetworkID: networkID, Account: account})
		if err == nil {
			conv := convertPMConversationFromDB(row)
			return &conv, false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
	}
	row, err := s.queries.GetUnboundPMConversation(ctx, db.GetUnboundPMConversationParams{NetworkID: networkID, NicknameKey: nickKey, TargetUser: strings.ToLower(nick)})
	created := false
	if errors.Is(err, sql.ErrNoRows) {
		if !create {
			return nil, false, nil
		}
		now := time.Now()
		row, err = s.queries.CreatePMConversation(ctx, db.CreatePMConversationParams{NetworkID: networkID, TargetUser: strings.ToLower(nick), IsOpen: true, CreatedAt: now, UpdatedAt: sql.NullTime{Time: now, Valid: true}})
		created = true
	}
	if err != nil {
		return nil, false, fmt.Errorf("resolve PM identity: %w", err)
	}
	if account != "" {
		if err := s.queries.BindPMConversationAccount(ctx, db.BindPMConversationAccountParams{Account: account, NicknameKey: nickKey, IdentitySource: source, IdentityObservedAt: time.Now().Unix(), NetworkID: networkID, ID: row.ID}); err != nil {
			return nil, false, err
		}
		row, err = s.queries.GetPMConversationByID(ctx, db.GetPMConversationByIDParams{NetworkID: networkID, ID: row.ID})
		if err != nil {
			return nil, false, err
		}
	}
	conv := convertPMConversationFromDB(row)
	return &conv, created, nil
}
