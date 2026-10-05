package main

import (
	"fmt"
	"strings"

	"github.com/matt0x6f/irc-client/internal/storage"
)

func (a *App) PreferPrivateContactSession(networkID int64, reference, nick string) error {
	a.mu.RLock()
	client := a.ircClients[networkID]
	a.mu.RUnlock()
	if client == nil || !client.IsConnectedDirect() {
		return fmt.Errorf("not connected")
	}
	return client.PreferPrivateContactSession(reference, nick)
}

func (a *App) OpenPrivateContact(networkID int64, reference string) (*storage.PrivateMessageConversation, error) {
	a.mu.RLock()
	client := a.ircClients[networkID]
	a.mu.RUnlock()
	var contact *storage.PrivateMessageConversation
	var err error
	if strings.HasPrefix(reference, "@") {
		contact, err = a.storage.FindPMConversation(networkID, reference)
	} else if client != nil {
		contact, err = client.OpenPrivateContact(reference)
	} else {
		contact, _, err = a.storage.GetOrCreatePMConversation(networkID, reference, "")
	}
	if err != nil {
		return nil, err
	}
	contact.Reference = storage.PMReference(contact.ID)
	if err := a.SetPrivateMessageOpen(networkID, contact.Reference, true); err != nil {
		return nil, err
	}
	contacts, err := a.GetPrivateContacts(networkID)
	if err != nil {
		return nil, err
	}
	for _, snapshot := range contacts {
		if snapshot.ID == contact.ID {
			return &snapshot, nil
		}
	}
	contact.Presence = "unknown"
	contact.Sessions = make([]string, 0)
	return contact, nil
}

// GetPrivateContacts returns stable saved identities enriched with current
// verified sessions. Disconnected clients retain history with unknown presence.
func (a *App) GetPrivateContacts(networkID int64) ([]storage.PrivateMessageConversation, error) {
	a.mu.RLock()
	client := a.ircClients[networkID]
	a.mu.RUnlock()
	if client != nil {
		return client.PrivateContacts()
	}
	contacts, err := a.storage.GetOpenPMConversations(networkID, "")
	if err != nil {
		return nil, err
	}
	for i := range contacts {
		contacts[i].Reference = storage.PMReference(contacts[i].ID)
		contacts[i].Presence = "unknown"
		contacts[i].Sessions = make([]string, 0)
	}
	return contacts, nil
}
