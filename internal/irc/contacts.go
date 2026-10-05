package irc

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// ResolvePrivateTarget validates local contact references immediately before
// sending. An explicit raw nickname remains an ordinary IRC address.
func (c *IRCClient) ResolvePrivateTarget(reference string) (string, error) {
	if !strings.HasPrefix(reference, "@") {
		return reference, nil
	}
	conv, err := c.storage.FindPMConversation(c.networkID, reference)
	if err != nil {
		return "", err
	}
	c.contactMu.Lock()
	defer c.contactMu.Unlock()
	sessions := c.matchingContactSessions(*conv)
	if len(sessions) != 0 {
		for _, session := range sessions {
			if c.sameName(session.Nick, conv.TargetUser) {
				return session.Nick, nil
			}
		}
		return sessions[0].Nick, nil
	}
	if conv.Account == "" {
		return conv.TargetUser, nil
	}
	return "", fmt.Errorf("this contact's current nickname is unknown")
}

func (c *IRCClient) ContactPresence(reference string) (string, error) {
	conv, err := c.storage.FindPMConversation(c.networkID, reference)
	if err != nil {
		return "unknown", err
	}
	if !c.IsConnectedDirect() {
		return "unknown", nil
	}
	c.contactMu.Lock()
	defer c.contactMu.Unlock()
	if len(c.matchingContactSessions(*conv)) != 0 {
		return "online", nil
	}
	return "unknown", nil
}

func (c *IRCClient) PrivateContacts() ([]storage.PrivateMessageConversation, error) {
	contacts, err := c.storage.GetOpenPMConversations(c.networkID, c.network.Nickname)
	if err != nil {
		return nil, err
	}
	connected := c.IsConnectedDirect()
	c.contactMu.Lock()
	defer c.contactMu.Unlock()
	for i := range contacts {
		contact := &contacts[i]
		contact.Reference = storage.PMReference(contact.ID)
		contact.Presence = "unknown"
		contact.Sessions = make([]string, 0)
		if !connected {
			continue
		}
		sessions := c.matchingContactSessions(*contact)
		for _, session := range sessions {
			contact.Sessions = append(contact.Sessions, session.Nick)
		}
		if len(sessions) != 0 {
			contact.Presence = "online"
			contact.Target = sessions[0].Nick
			for _, session := range sessions {
				if c.sameName(session.Nick, contact.TargetUser) {
					contact.Target = session.Nick
					break
				}
			}
		}
	}
	return contacts, nil
}

// Caller holds contactMu. Account-bound contacts need current server evidence;
// bare nick observations cannot confirm an account's identity.
func (c *IRCClient) matchingContactSessions(conv storage.PrivateMessageConversation) []contactSession {
	sessions := make([]contactSession, 0)
	for _, session := range c.contactSessions {
		if session.ContactID != conv.ID {
			continue
		}
		if conv.Account != "" && (session.Account != conv.Account || !c.contactSourceEnabled(session.Source)) {
			continue
		}
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool { return c.foldKey(sessions[i].Nick) < c.foldKey(sessions[j].Nick) })
	return sessions
}

type contactSession struct {
	Nick      string
	Account   string
	Source    string
	ContactID int64
}

func (c *IRCClient) OpenPrivateContact(nick string) (*storage.PrivateMessageConversation, error) {
	contact, _, err := c.getOrCreatePMContact(nick)
	return contact, err
}

// PreferPrivateContactSession accepts only a current session belonging to the
// saved contact. Preference changes the delivery address, never message ownership.
func (c *IRCClient) PreferPrivateContactSession(reference, nick string) error {
	conv, err := c.storage.FindPMConversation(c.networkID, reference)
	if err != nil {
		return err
	}
	c.contactMu.Lock()
	defer c.contactMu.Unlock()
	for _, session := range c.matchingContactSessions(*conv) {
		if c.sameName(session.Nick, nick) {
			if err := c.storage.SetPMContactTarget(c.networkID, conv.ID, session.Nick, c.foldKey(session.Nick)); err != nil {
				return err
			}
			c.emitChannelsChanged()
			return nil
		}
	}
	return fmt.Errorf("that nickname is not a verified session of this contact")
}

func (c *IRCClient) getOrCreatePMContact(nick string) (*storage.PrivateMessageConversation, bool, error) {
	c.contactMu.Lock()
	defer c.contactMu.Unlock()
	if c.contactSessions == nil {
		c.contactSessions = make(map[string]contactSession)
	}
	key := c.foldKey(nick)
	session := c.contactSessions[key]
	if session.ContactID != 0 {
		conv, err := c.storage.FindPMConversation(c.networkID, storage.PMReference(session.ContactID))
		if err != nil {
			return nil, false, err
		}
		if conv.Account == "" || (session.Account == conv.Account && c.contactSourceEnabled(session.Source)) {
			return conv, false, nil
		}
		session.ContactID = 0
	}
	if !c.contactSourceEnabled(session.Source) {
		session.Account = ""
		session.Source = ""
	}
	conv, created, err := c.reconcileContactIdentity(nick, session.Account, session.Source, true)
	if err != nil {
		return nil, false, err
	}
	session.Nick = nick
	session.ContactID = conv.ID
	c.contactSessions[key] = session
	return conv, created, nil
}

func (c *IRCClient) pmConversationID(nick string) int64 {
	if nick == "" {
		return 0
	}
	c.contactMu.Lock()
	defer c.contactMu.Unlock()
	return c.contactSessions[c.foldKey(nick)].ContactID
}

func (c *IRCClient) renamePrivateContact(oldNick, newNick string) (bool, error) {
	c.contactMu.Lock()
	defer c.contactMu.Unlock()
	oldKey, newKey := c.foldKey(oldNick), c.foldKey(newNick)
	session, known := c.contactSessions[oldKey]
	if known && session.ContactID != 0 {
		contact, err := c.storage.FindPMConversation(c.networkID, storage.PMReference(session.ContactID))
		if err != nil {
			return false, err
		}
		if c.sameName(contact.TargetUser, oldNick) {
			if err := c.storage.SetPMContactTarget(c.networkID, session.ContactID, newNick, newKey); err != nil {
				return false, err
			}
		}
		delete(c.contactSessions, oldKey)
		session.Nick = newNick
		c.contactSessions[newKey] = session
		return true, nil
	}
	// A nickname-only watch can follow its observed rename. Saved account-bound
	// contacts require a matching session; an old nickname alone is insufficient.
	conv, err := c.storage.FindPMConversation(c.networkID, oldNick)
	if err != nil || conv.Account != "" {
		return false, nil
	}
	if err := c.storage.SetPMContactTarget(c.networkID, conv.ID, strings.ToLower(newNick), newKey); err != nil {
		return false, err
	}
	if c.contactSessions == nil {
		c.contactSessions = make(map[string]contactSession)
	}
	c.contactSessions[newKey] = contactSession{Nick: newNick, ContactID: conv.ID}
	delete(c.contactSessions, oldKey)
	return true, nil
}

func (c *IRCClient) contactSourceEnabled(source string) bool {
	if source == "whox" {
		c.mu.RLock()
		enabled := c.supportsWHOX
		c.mu.RUnlock()
		return enabled
	}
	if source == "whois" {
		return true
	}
	return c.capEnabled(source)
}

func (c *IRCClient) observeContactAccount(nick, account, source string) {
	if c.storage == nil || !c.contactSourceEnabled(source) {
		return
	}
	c.contactMu.Lock()
	defer c.contactMu.Unlock()
	if c.contactSessions == nil {
		c.contactSessions = make(map[string]contactSession)
	}
	key := c.foldKey(nick)
	previous := c.contactSessions[key]
	c.contactSessions[key] = contactSession{Nick: nick, Account: account, Source: source}
	conv, _, err := c.reconcileContactIdentity(nick, account, source, false)
	if err != nil {
		logger.Log.Error().Err(err).Msg("Failed to reconcile contact identity")
		return
	}
	if conv == nil {
		if previous.ContactID != 0 {
			c.emitChannelsChanged()
		}
		return
	}
	c.contactSessions[key] = contactSession{Nick: nick, Account: account, Source: source, ContactID: conv.ID}
	preferred, known := c.contactSessions[c.foldKey(conv.TargetUser)]
	if !known || preferred.ContactID != conv.ID || preferred.Account != conv.Account {
		if err := c.storage.SetPMContactTarget(c.networkID, conv.ID, nick, c.foldKey(nick)); err != nil {
			logger.Log.Error().Err(err).Msg("Failed to update contact nickname")
			return
		}
	}
	c.emitChannelsChanged()
}

func (c *IRCClient) forgetContactSession(nick string) {
	c.contactMu.Lock()
	session := c.contactSessions[c.foldKey(nick)]
	delete(c.contactSessions, c.foldKey(nick))
	c.contactMu.Unlock()
	if session.ContactID != 0 {
		c.emitChannelsChanged()
	}
}

func (c *IRCClient) invalidateContactSources() {
	c.contactMu.Lock()
	changed := false
	for key, session := range c.contactSessions {
		if session.Source != "" && !c.contactSourceEnabled(session.Source) {
			delete(c.contactSessions, key)
			changed = true
		}
	}
	c.contactMu.Unlock()
	if changed {
		c.emitChannelsChanged()
	}
}

// Legacy rows have no negotiated nickname key yet. Compare those names using
// this connection's CASEMAPPING before learning an account or creating a chat.
func (c *IRCClient) reconcileContactIdentity(nick, account, source string, create bool) (*storage.PrivateMessageConversation, bool, error) {
	key := c.foldKey(nick)
	contact, created, err := c.storage.ReconcilePMIdentity(c.networkID, nick, key, account, source, false)
	if err != nil || contact != nil {
		return contact, created, err
	}
	// ASCII case is already covered by storage. Only RFC1459 punctuation can
	// require scanning unbound legacy names; avoid that work for unrelated rosters.
	if !strings.ContainsAny(nick, "[]{}\\|~^") {
		if !create {
			return nil, false, nil
		}
		return c.storage.ReconcilePMIdentity(c.networkID, nick, key, account, source, true)
	}
	candidates, err := c.storage.GetUnboundPMContacts(c.networkID)
	if err != nil {
		return nil, false, err
	}
	for _, candidate := range candidates {
		if c.sameName(candidate.TargetUser, nick) {
			if err := c.storage.SetPMContactTarget(c.networkID, candidate.ID, nick, key); err != nil {
				return nil, false, err
			}
			break
		}
	}
	return c.storage.ReconcilePMIdentity(c.networkID, nick, key, account, source, create)
}

func (c *IRCClient) rememberHistoryContact(reference, target string) {
	if IsChannelName(target) {
		return
	}
	conv, err := c.storage.FindPMConversation(c.networkID, reference)
	if err != nil {
		return
	}
	c.contactMu.Lock()
	if c.historyContacts == nil {
		c.historyContacts = make(map[string]int64)
	}
	c.historyContacts[c.foldKey(target)] = conv.ID
	c.contactMu.Unlock()
}

func (c *IRCClient) historyContactReference(target string) string {
	c.contactMu.Lock()
	key := c.foldKey(target)
	id := c.historyContacts[key]
	delete(c.historyContacts, key)
	if id == 0 {
		id = c.contactSessions[key].ContactID
	}
	c.contactMu.Unlock()
	if id != 0 && !IsChannelName(target) {
		return storage.PMReference(id)
	}
	return target
}
