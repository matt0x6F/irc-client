package storage

import (
	"path/filepath"
	"testing"
	"time"
)

// newTestStorage creates a Storage backed by a temp-dir SQLite database.
// The caller does NOT need to close it; cleanup is handled by t.Cleanup.
func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ---------- helpers ----------

func makeNetwork(name string) *Network {
	now := time.Now()
	return &Network{
		Name:      name,
		Address:   "irc.example.com",
		Port:      6697,
		TLS:       true,
		Nickname:  "testuser",
		Username:  "testuser",
		Realname:  "Test User",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// ---------- Network CRUD ----------

func TestCreateAndGetNetwork(t *testing.T) {
	s := newTestStorage(t)

	net := makeNetwork("TestNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if net.ID == 0 {
		t.Fatal("expected network ID to be set after create")
	}

	// GetNetwork by ID
	got, err := s.GetNetwork(net.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if got.Name != "TestNet" {
		t.Errorf("expected name 'TestNet', got '%s'", got.Name)
	}
	if got.Address != "irc.example.com" {
		t.Errorf("expected address 'irc.example.com', got '%s'", got.Address)
	}
	if got.Port != 6697 {
		t.Errorf("expected port 6697, got %d", got.Port)
	}
	if !got.TLS {
		t.Error("expected TLS=true")
	}
}

func TestGetNetworks(t *testing.T) {
	s := newTestStorage(t)

	for _, name := range []string{"NetA", "NetB"} {
		net := makeNetwork(name)
		if err := s.CreateNetwork(net); err != nil {
			t.Fatalf("CreateNetwork(%s): %v", name, err)
		}
	}

	nets, err := s.GetNetworks()
	if err != nil {
		t.Fatalf("GetNetworks: %v", err)
	}
	if len(nets) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(nets))
	}
}

func TestUpdateNetwork(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("Original")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	net.Name = "Updated"
	net.Port = 6667
	net.TLS = false
	net.UpdatedAt = time.Now()
	if err := s.UpdateNetwork(net); err != nil {
		t.Fatalf("UpdateNetwork: %v", err)
	}

	got, err := s.GetNetwork(net.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if got.Name != "Updated" {
		t.Errorf("expected name 'Updated', got '%s'", got.Name)
	}
	if got.Port != 6667 {
		t.Errorf("expected port 6667, got %d", got.Port)
	}
	if got.TLS {
		t.Error("expected TLS=false after update")
	}
}

func TestDeleteNetwork(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("ToDelete")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	if err := s.DeleteNetwork(net.ID); err != nil {
		t.Fatalf("DeleteNetwork: %v", err)
	}

	nets, err := s.GetNetworks()
	if err != nil {
		t.Fatalf("GetNetworks: %v", err)
	}
	if len(nets) != 0 {
		t.Errorf("expected 0 networks after delete, got %d", len(nets))
	}
}

func TestUpdateNetworkAutoConnect(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("AutoConn")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	if err := s.UpdateNetworkAutoConnect(net.ID, true); err != nil {
		t.Fatalf("UpdateNetworkAutoConnect: %v", err)
	}

	got, err := s.GetNetwork(net.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if !got.AutoConnect {
		t.Error("expected AutoConnect=true")
	}
}

func TestNetworkWithSASL(t *testing.T) {
	s := newTestStorage(t)
	mech := "PLAIN"
	user := "sasluser"
	pass := "saslpass"
	net := makeNetwork("SASLNet")
	net.SASLEnabled = true
	net.SASLMechanism = &mech
	net.SASLUsername = &user
	net.SASLPassword = &pass

	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	got, err := s.GetNetwork(net.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if !got.SASLEnabled {
		t.Error("expected SASLEnabled=true")
	}
	if got.SASLMechanism == nil || *got.SASLMechanism != "PLAIN" {
		t.Errorf("expected SASLMechanism='PLAIN', got %v", got.SASLMechanism)
	}
	if got.SASLUsername == nil || *got.SASLUsername != "sasluser" {
		t.Errorf("expected SASLUsername='sasluser', got %v", got.SASLUsername)
	}
}

// ---------- Server CRUD ----------

func TestCreateAndGetServers(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("ServerNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	srv := &Server{
		NetworkID: net.ID,
		Address:   "irc1.example.com",
		Port:      6697,
		TLS:       true,
		Order:     0,
		CreatedAt: time.Now(),
	}
	if err := s.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if srv.ID == 0 {
		t.Fatal("expected server ID to be set")
	}

	srv2 := &Server{
		NetworkID: net.ID,
		Address:   "irc2.example.com",
		Port:      6667,
		TLS:       false,
		Order:     1,
		CreatedAt: time.Now(),
	}
	if err := s.CreateServer(srv2); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	servers, err := s.GetServers(net.ID)
	if err != nil {
		t.Fatalf("GetServers: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	// Should be ordered by "order"
	if servers[0].Address != "irc1.example.com" {
		t.Errorf("expected first server irc1, got '%s'", servers[0].Address)
	}
}

func TestDeleteAllServers(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("DelServers")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	for _, addr := range []string{"a.com", "b.com"} {
		srv := &Server{NetworkID: net.ID, Address: addr, Port: 6667, CreatedAt: time.Now()}
		if err := s.CreateServer(srv); err != nil {
			t.Fatalf("CreateServer: %v", err)
		}
	}

	if err := s.DeleteAllServers(net.ID); err != nil {
		t.Fatalf("DeleteAllServers: %v", err)
	}

	servers, err := s.GetServers(net.ID)
	if err != nil {
		t.Fatalf("GetServers: %v", err)
	}
	if len(servers) != 0 {
		t.Errorf("expected 0 servers after DeleteAll, got %d", len(servers))
	}
}

func TestDeleteServer(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("DelOneSrv")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	srv := &Server{NetworkID: net.ID, Address: "delete.me", Port: 6667, CreatedAt: time.Now()}
	if err := s.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	if err := s.DeleteServer(srv.ID); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}

	servers, err := s.GetServers(net.ID)
	if err != nil {
		t.Fatalf("GetServers: %v", err)
	}
	if len(servers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(servers))
	}
}

// ---------- Channel operations ----------

func TestCreateAndGetChannel(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("ChanNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	ch := &Channel{
		NetworkID: net.ID,
		Name:      "#test",
		AutoJoin:  true,
		IsOpen:    true,
		CreatedAt: time.Now(),
	}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if ch.ID == 0 {
		t.Fatal("expected channel ID to be set")
	}

	channels, err := s.GetChannels(net.ID)
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].Name != "#test" {
		t.Errorf("expected channel '#test', got '%s'", channels[0].Name)
	}
	if !channels[0].AutoJoin {
		t.Error("expected AutoJoin=true")
	}
}

func TestGetChannelByName(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("ChanByName")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	ch := &Channel{NetworkID: net.ID, Name: "#MyChannel", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// Case-insensitive lookup
	got, err := s.GetChannelByName(net.ID, "#mychannel")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if got.Name != "#MyChannel" {
		t.Errorf("expected '#MyChannel', got '%s'", got.Name)
	}
}

func TestUpdateChannelTopic(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("TopicNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#topic", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if err := s.UpdateChannelTopic(ch.ID, "New topic here"); err != nil {
		t.Fatalf("UpdateChannelTopic: %v", err)
	}

	got, err := s.GetChannelByName(net.ID, "#topic")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if got.Topic != "New topic here" {
		t.Errorf("expected topic 'New topic here', got '%s'", got.Topic)
	}
}

func TestUpdateChannelAutoJoin(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("AJNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#autojoin", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if err := s.UpdateChannelAutoJoin(ch.ID, true); err != nil {
		t.Fatalf("UpdateChannelAutoJoin: %v", err)
	}

	got, err := s.GetChannelByName(net.ID, "#autojoin")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if !got.AutoJoin {
		t.Error("expected AutoJoin=true")
	}
}

func TestUpdateChannelIsOpen(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("OpenNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#opentest", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if err := s.UpdateChannelIsOpen(ch.ID, true); err != nil {
		t.Fatalf("UpdateChannelIsOpen: %v", err)
	}

	got, err := s.GetChannelByName(net.ID, "#opentest")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if !got.IsOpen {
		t.Error("expected IsOpen=true")
	}
}

// ---------- Channel users ----------

func TestChannelUsers(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("UserNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#users", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// Add users
	if err := s.AddChannelUser(ch.ID, "alice", "@"); err != nil {
		t.Fatalf("AddChannelUser(alice): %v", err)
	}
	if err := s.AddChannelUser(ch.ID, "bob", "+"); err != nil {
		t.Fatalf("AddChannelUser(bob): %v", err)
	}

	users, err := s.GetChannelUsers(ch.ID)
	if err != nil {
		t.Fatalf("GetChannelUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	// Remove one user
	if err := s.RemoveChannelUser(ch.ID, "alice"); err != nil {
		t.Fatalf("RemoveChannelUser: %v", err)
	}
	users, err = s.GetChannelUsers(ch.ID)
	if err != nil {
		t.Fatalf("GetChannelUsers: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("expected 1 user after remove, got %d", len(users))
	}

	// Clear all users
	if err := s.ClearChannelUsers(ch.ID); err != nil {
		t.Fatalf("ClearChannelUsers: %v", err)
	}
	users, err = s.GetChannelUsers(ch.ID)
	if err != nil {
		t.Fatalf("GetChannelUsers: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users after clear, got %d", len(users))
	}
}

func TestUpdateChannelUserNickname(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("NickNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#nickchange", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if err := s.AddChannelUser(ch.ID, "oldnick", "@"); err != nil {
		t.Fatalf("AddChannelUser: %v", err)
	}

	if err := s.UpdateChannelUserNickname(net.ID, "oldnick", "newnick"); err != nil {
		t.Fatalf("UpdateChannelUserNickname: %v", err)
	}

	users, err := s.GetChannelUsers(ch.ID)
	if err != nil {
		t.Fatalf("GetChannelUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Nickname != "newnick" {
		t.Errorf("expected nickname 'newnick', got '%s'", users[0].Nickname)
	}
}

// ---------- Messages ----------

func TestWriteMessageSyncAndGetMessages(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("MsgNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#messages", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// Write messages synchronously so they're immediately available
	for i := 0; i < 5; i++ {
		msg := Message{
			NetworkID:   net.ID,
			ChannelID:   &ch.ID,
			User:        "sender",
			Message:     "hello " + time.Now().String(),
			MessageType: "privmsg",
			Timestamp:   time.Now(),
		}
		if err := s.WriteMessageSync(msg); err != nil {
			t.Fatalf("WriteMessageSync %d: %v", i, err)
		}
	}

	msgs, err := s.GetMessages(net.ID, &ch.ID, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}

	// Verify chronological order (earliest first)
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Timestamp.Before(msgs[i-1].Timestamp) {
			t.Errorf("messages not in chronological order at index %d", i)
		}
	}
}

func TestWriteMessageBuffered(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("BufferNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#buffered", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// Write messages to the buffer (async)
	for i := 0; i < 3; i++ {
		msg := Message{
			NetworkID:   net.ID,
			ChannelID:   &ch.ID,
			User:        "bufferer",
			Message:     "buffered msg",
			MessageType: "privmsg",
			Timestamp:   time.Now(),
		}
		if err := s.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage %d: %v", i, err)
		}
	}

	// Wait for the flush interval to trigger
	time.Sleep(200 * time.Millisecond)

	msgs, err := s.GetMessages(net.ID, &ch.ID, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages after flush, got %d", len(msgs))
	}
}

func TestWriteMessageDirect(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("DirectNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#direct", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	msg := Message{
		NetworkID:   net.ID,
		ChannelID:   &ch.ID,
		User:        "directuser",
		Message:     "direct message",
		MessageType: "privmsg",
		Timestamp:   time.Now(),
	}
	if err := s.WriteMessageDirect(msg); err != nil {
		t.Fatalf("WriteMessageDirect: %v", err)
	}

	msgs, err := s.GetMessages(net.ID, &ch.ID, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].User != "directuser" {
		t.Errorf("expected user 'directuser', got '%s'", msgs[0].User)
	}
}

func TestGetMessagesWithoutChannel(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("NoChanMsg")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// Write a message without channel (server/status message)
	msg := Message{
		NetworkID:   net.ID,
		ChannelID:   nil,
		User:        "*",
		Message:     "server notice",
		MessageType: "notice",
		Timestamp:   time.Now(),
	}
	if err := s.WriteMessageDirect(msg); err != nil {
		t.Fatalf("WriteMessageDirect: %v", err)
	}

	msgs, err := s.GetMessages(net.ID, nil, 10)
	if err != nil {
		t.Fatalf("GetMessages(nil channel): %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ChannelID != nil {
		t.Errorf("expected nil ChannelID, got %v", msgs[0].ChannelID)
	}
}

func TestGetMessagesLimit(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("LimitNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#limit", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	for i := 0; i < 10; i++ {
		msg := Message{
			NetworkID:   net.ID,
			ChannelID:   &ch.ID,
			User:        "sender",
			Message:     "msg",
			MessageType: "privmsg",
			Timestamp:   time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := s.WriteMessageDirect(msg); err != nil {
			t.Fatalf("WriteMessageDirect: %v", err)
		}
	}

	msgs, err := s.GetMessages(net.ID, &ch.ID, 3)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages with limit, got %d", len(msgs))
	}
}

// ---------- PM Conversations ----------

func TestGetOrCreatePMConversation(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("PMNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	conv, err := s.GetOrCreatePMConversation(net.ID, "OtherUser", "testuser")
	if err != nil {
		t.Fatalf("GetOrCreatePMConversation: %v", err)
	}
	if conv == nil {
		t.Fatal("expected conversation, got nil")
	}
	if conv.TargetUser != "otheruser" {
		t.Errorf("expected target_user 'otheruser' (lowercase), got '%s'", conv.TargetUser)
	}
	if !conv.IsOpen {
		t.Error("expected new PM conversation to be open")
	}

	// Getting the same conversation should return the existing one
	conv2, err := s.GetOrCreatePMConversation(net.ID, "OtherUser", "testuser")
	if err != nil {
		t.Fatalf("GetOrCreatePMConversation (second): %v", err)
	}
	if conv2.ID != conv.ID {
		t.Errorf("expected same ID %d, got %d", conv.ID, conv2.ID)
	}
}

func TestUpdatePMConversationIsOpen(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("PMOpenNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	conv, err := s.GetOrCreatePMConversation(net.ID, "friend", "testuser")
	if err != nil {
		t.Fatalf("GetOrCreatePMConversation: %v", err)
	}
	if !conv.IsOpen {
		t.Fatal("expected initially open")
	}

	if err := s.UpdatePMConversationIsOpen(net.ID, "friend", false); err != nil {
		t.Fatalf("UpdatePMConversationIsOpen: %v", err)
	}

	conv2, err := s.GetOrCreatePMConversation(net.ID, "friend", "testuser")
	if err != nil {
		t.Fatalf("GetOrCreatePMConversation: %v", err)
	}
	if conv2.IsOpen {
		t.Error("expected IsOpen=false after update")
	}
}

func TestGetOpenPMConversations(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("OpenPMNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// Create some conversations
	for _, user := range []string{"alice", "bob", "charlie"} {
		if _, err := s.GetOrCreatePMConversation(net.ID, user, "testuser"); err != nil {
			t.Fatalf("GetOrCreatePMConversation(%s): %v", user, err)
		}
	}
	// Close one
	if err := s.UpdatePMConversationIsOpen(net.ID, "bob", false); err != nil {
		t.Fatalf("UpdatePMConversationIsOpen: %v", err)
	}

	convs, err := s.GetOpenPMConversations(net.ID, "testuser")
	if err != nil {
		t.Fatalf("GetOpenPMConversations: %v", err)
	}
	if len(convs) != 2 {
		t.Errorf("expected 2 open conversations, got %d", len(convs))
	}
}

// ---------- Plugin configs ----------

func TestPluginConfig(t *testing.T) {
	s := newTestStorage(t)

	// Getting a non-existent plugin should return defaults
	cfg, err := s.GetPluginConfig("test-plugin")
	if err != nil {
		t.Fatalf("GetPluginConfig: %v", err)
	}
	if cfg.Name != "test-plugin" {
		t.Errorf("expected name 'test-plugin', got '%s'", cfg.Name)
	}
	if !cfg.Enabled {
		t.Error("expected default Enabled=true")
	}

	// Create the plugin config row first via SetPluginEnabled,
	// then set config and schema so the row has non-NULL JSON columns.
	if err := s.SetPluginEnabled("test-plugin", true); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}

	config := map[string]interface{}{"key": "value", "count": float64(42)}
	if err := s.SetPluginConfig("test-plugin", config); err != nil {
		t.Fatalf("SetPluginConfig: %v", err)
	}

	schema := map[string]interface{}{"type": "object"}
	if err := s.SetPluginConfigSchema("test-plugin", schema); err != nil {
		t.Fatalf("SetPluginConfigSchema: %v", err)
	}

	cfg, err = s.GetPluginConfig("test-plugin")
	if err != nil {
		t.Fatalf("GetPluginConfig after set: %v", err)
	}
	if cfg.Config["key"] != "value" {
		t.Errorf("expected config key='value', got '%v'", cfg.Config["key"])
	}
}

func TestSetPluginEnabled(t *testing.T) {
	s := newTestStorage(t)

	// Create the row with non-NULL config columns first
	if err := s.SetPluginEnabled("my-plugin", true); err != nil {
		t.Fatalf("SetPluginEnabled (create): %v", err)
	}
	if err := s.SetPluginConfig("my-plugin", map[string]interface{}{}); err != nil {
		t.Fatalf("SetPluginConfig: %v", err)
	}
	if err := s.SetPluginConfigSchema("my-plugin", map[string]interface{}{}); err != nil {
		t.Fatalf("SetPluginConfigSchema: %v", err)
	}

	// Now disable and verify
	if err := s.SetPluginEnabled("my-plugin", false); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}

	cfg, err := s.GetPluginConfig("my-plugin")
	if err != nil {
		t.Fatalf("GetPluginConfig: %v", err)
	}
	if cfg.Enabled {
		t.Error("expected Enabled=false")
	}
}

// ---------- Write buffer flush ----------

func TestWriteBufferFlushViaTicker(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "flush_test.db")

	// Use a very short flush interval so the ticker fires quickly
	s, err := NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	net := makeNetwork("FlushNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#flush", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// Write buffered messages (async, not sync)
	for i := 0; i < 5; i++ {
		msg := Message{
			NetworkID:   net.ID,
			ChannelID:   &ch.ID,
			User:        "flusher",
			Message:     "buffered",
			MessageType: "privmsg",
			Timestamp:   time.Now(),
		}
		if err := s.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage: %v", err)
		}
	}

	// Wait for the flush ticker to fire
	time.Sleep(200 * time.Millisecond)

	msgs, err := s.GetMessages(net.ID, &ch.ID, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages after ticker flush, got %d", len(msgs))
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and verify messages survived
	s2, err := NewStorage(dbPath, 100, time.Second)
	if err != nil {
		t.Fatalf("NewStorage (reopen): %v", err)
	}
	defer s2.Close()

	msgs, err = s2.GetMessages(net.ID, &ch.ID, 10)
	if err != nil {
		t.Fatalf("GetMessages after reopen: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages after reopen, got %d", len(msgs))
	}
}

func TestWriteMessageAfterClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "afterclose.db")

	s, err := NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	// Close explicitly -- no t.Cleanup wrapper to avoid double-close panic
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Writing after close should return an error
	msg := Message{
		NetworkID:   1,
		User:        "test",
		Message:     "should fail",
		MessageType: "privmsg",
		Timestamp:   time.Now(),
	}
	err = s.WriteMessage(msg)
	if err == nil {
		t.Error("expected error when writing after close")
	}
}

// ---------- Delete network with servers ----------

func TestDeleteNetworkAndServers(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("CleanupNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	srv := &Server{NetworkID: net.ID, Address: "cleanup.com", Port: 6667, CreatedAt: time.Now()}
	if err := s.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	// Manually delete servers first, then the network (the app does this in practice)
	if err := s.DeleteAllServers(net.ID); err != nil {
		t.Fatalf("DeleteAllServers: %v", err)
	}
	if err := s.DeleteNetwork(net.ID); err != nil {
		t.Fatalf("DeleteNetwork: %v", err)
	}

	servers, err := s.GetServers(net.ID)
	if err != nil {
		t.Fatalf("GetServers: %v", err)
	}
	if len(servers) != 0 {
		t.Errorf("expected 0 servers after cleanup, got %d", len(servers))
	}
	nets, err := s.GetNetworks()
	if err != nil {
		t.Fatalf("GetNetworks: %v", err)
	}
	if len(nets) != 0 {
		t.Errorf("expected 0 networks after delete, got %d", len(nets))
	}
}

// ---------- Private message routing (pm_target) ----------

// writePM is a helper that synchronously writes a private-message row with an
// explicit conversation peer (pm_target), as the IRC client does at write time.
func writePM(t *testing.T, s *Storage, netID int64, user, peer, body, raw string) {
	t.Helper()
	if err := s.WriteMessageSync(Message{
		NetworkID:   netID,
		ChannelID:   nil,
		User:        user,
		Message:     body,
		MessageType: "privmsg",
		Timestamp:   time.Now(),
		RawLine:     raw,
		PMTarget:    peer,
	}); err != nil {
		t.Fatalf("WriteMessageSync(%s->%s): %v", user, peer, err)
	}
}

// TestGetPrivateMessagesRouting is the regression guard for the echo-message
// routing bug: a PM sent to chanserv must land in the chanserv pane (not the
// self-PM pane), received PMs route to the sender, and a genuine self-PM stays
// isolated. Matching is keyed by pm_target and is case-insensitive.
func TestGetPrivateMessagesRouting(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("PMRoute") // Nickname: "testuser"
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// Sent to chanserv (echoed back: stored with User = our nick, peer = chanserv).
	writePM(t, s, net.ID, "testuser", "chanserv", "help", ":testuser PRIVMSG chanserv :help")
	// Received from alice (peer = sender).
	writePM(t, s, net.ID, "alice", "alice", "hi", ":alice PRIVMSG testuser :hi")
	// Genuine self-PM (we messaged ourselves; peer == our nick).
	writePM(t, s, net.ID, "testuser", "testuser", "note-to-self", ":testuser PRIVMSG testuser :note-to-self")

	expectOne := func(target, wantBody string) {
		t.Helper()
		msgs, err := s.GetPrivateMessages(net.ID, target, "testuser", 50)
		if err != nil {
			t.Fatalf("GetPrivateMessages(%s): %v", target, err)
		}
		if len(msgs) != 1 {
			t.Fatalf("GetPrivateMessages(%s): expected 1 message, got %d", target, len(msgs))
		}
		if msgs[0].Message != wantBody {
			t.Fatalf("GetPrivateMessages(%s): expected body %q, got %q", target, wantBody, msgs[0].Message)
		}
	}

	// The chanserv pane shows the sent message and nothing else.
	expectOne("chanserv", "help")
	// The self-PM pane shows only the genuine self-message, NOT the chanserv one.
	expectOne("testuser", "note-to-self")
	// The alice pane shows the received message.
	expectOne("alice", "hi")
	// Matching is case-insensitive on the peer.
	expectOne("ChanServ", "help")
}

// TestGetMessagesWithoutChannelExcludesPMs verifies the Status/Server pane no
// longer leaks private messages: it returns only null-channel rows that have no
// conversation peer (server notices, status messages).
func TestGetMessagesWithoutChannelExcludesPMs(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("StatusNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// A server/status message (no pm_target).
	if err := s.WriteMessageSync(Message{
		NetworkID:   net.ID,
		ChannelID:   nil,
		User:        "*",
		Message:     "server notice",
		MessageType: "notice",
		Timestamp:   time.Now(),
	}); err != nil {
		t.Fatalf("WriteMessageSync(status): %v", err)
	}
	// A private message (has a peer) must NOT appear in the status pane.
	writePM(t, s, net.ID, "alice", "alice", "psst", ":alice PRIVMSG testuser :psst")

	msgs, err := s.GetMessages(net.ID, nil, 50)
	if err != nil {
		t.Fatalf("GetMessages(nil channel): %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 status message, got %d", len(msgs))
	}
	if msgs[0].Message != "server notice" {
		t.Fatalf("expected only the server notice, got %q", msgs[0].Message)
	}
}

// TestServiceNoticeRoutesToQueryPane is the regression guard for ChanServ/
// NickServ replies leaking into the Status log. Services reply via NOTICE; a
// notice carrying a conversation peer (pm_target) must appear in that peer's
// query pane and must NOT appear in the Status/Server pane. A genuine server
// notice (no peer) stays in Status.
func TestServiceNoticeRoutesToQueryPane(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("NoticeRoute") // Nickname: "testuser"
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// A ChanServ service notice, stored as a 'notice' row keyed to the ChanServ peer.
	if err := s.WriteMessageSync(Message{
		NetworkID:   net.ID,
		ChannelID:   nil,
		User:        "ChanServ",
		Message:     "Invalid ChanServ command.",
		MessageType: "notice",
		Timestamp:   time.Now(),
		RawLine:     ":ChanServ!ChanServ@services. NOTICE testuser :Invalid ChanServ command.",
		PMTarget:    "ChanServ",
	}); err != nil {
		t.Fatalf("WriteMessageSync(notice): %v", err)
	}
	// A genuine server notice (no peer) belongs in Status.
	if err := s.WriteMessageSync(Message{
		NetworkID:   net.ID,
		ChannelID:   nil,
		User:        "irc.example.net",
		Message:     "*** Looking up your hostname...",
		MessageType: "notice",
		Timestamp:   time.Now(),
	}); err != nil {
		t.Fatalf("WriteMessageSync(server notice): %v", err)
	}

	// The ChanServ query pane shows the service notice (case-insensitive peer).
	pm, err := s.GetPrivateMessages(net.ID, "chanserv", "testuser", 50)
	if err != nil {
		t.Fatalf("GetPrivateMessages(chanserv): %v", err)
	}
	if len(pm) != 1 || pm[0].Message != "Invalid ChanServ command." {
		t.Fatalf("ChanServ pane: expected the service notice, got %+v", pm)
	}

	// The Status pane shows only the genuine server notice, not the ChanServ reply.
	status, err := s.GetMessages(net.ID, nil, 50)
	if err != nil {
		t.Fatalf("GetMessages(status): %v", err)
	}
	if len(status) != 1 || status[0].Message != "*** Looking up your hostname..." {
		t.Fatalf("Status pane: expected only the server notice, got %+v", status)
	}
}

// TestChannelNoticeRoutesToChannel verifies a NOTICE addressed to a channel is
// stored in that channel's buffer (channel_id set, message_type 'notice') and
// does NOT leak into the Status pane.
func TestChannelNoticeRoutesToChannel(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("ChanNotice")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#chan", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// A channel notice (e.g. a bot announcement) keyed to the channel.
	if err := s.WriteMessageSync(Message{
		NetworkID:   net.ID,
		ChannelID:   &ch.ID,
		User:        "AnnounceBot",
		Message:     "build #42 passed",
		MessageType: "notice",
		Timestamp:   time.Now(),
	}); err != nil {
		t.Fatalf("WriteMessageSync(channel notice): %v", err)
	}

	// The channel buffer shows the notice.
	chanMsgs, err := s.GetMessages(net.ID, &ch.ID, 50)
	if err != nil {
		t.Fatalf("GetMessages(channel): %v", err)
	}
	if len(chanMsgs) != 1 || chanMsgs[0].Message != "build #42 passed" {
		t.Fatalf("channel pane: expected the channel notice, got %+v", chanMsgs)
	}

	// The Status pane shows nothing (the notice belongs to the channel).
	status, err := s.GetMessages(net.ID, nil, 50)
	if err != nil {
		t.Fatalf("GetMessages(status): %v", err)
	}
	if len(status) != 0 {
		t.Fatalf("status pane: expected no messages, got %+v", status)
	}
}

// TestMigratePMTargetBackfill verifies the best-effort backfill of legacy rows
// that predate the pm_target column (or whose backfill previously rolled back),
// simulated by inserting null-channel PM rows with pm_target left NULL.
func TestMigratePMTargetBackfill(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("BackfillNet") // Nickname: "testuser"
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	insertLegacy := func(user, raw string) {
		t.Helper()
		_, err := s.db.Exec(
			`INSERT INTO messages (network_id, channel_id, user, message, message_type, timestamp, raw_line, pm_target)
			 VALUES (?, NULL, ?, '', 'privmsg', CURRENT_TIMESTAMP, ?, NULL)`,
			net.ID, user, raw)
		if err != nil {
			t.Fatalf("insert legacy row: %v", err)
		}
	}
	// Legacy sent row (peer derived from raw_line) and received row (peer = sender).
	insertLegacy("testuser", ":testuser PRIVMSG chanserv :help")
	insertLegacy("alice", ":alice PRIVMSG testuser :hi")

	// Before backfill these are invisible to the new peer-keyed query.
	if msgs, _ := s.GetPrivateMessages(net.ID, "chanserv", "testuser", 50); len(msgs) != 0 {
		t.Fatalf("expected 0 messages before backfill, got %d", len(msgs))
	}

	if err := migratePMTarget(s.db); err != nil {
		t.Fatalf("migratePMTarget: %v", err)
	}

	if msgs, _ := s.GetPrivateMessages(net.ID, "chanserv", "testuser", 50); len(msgs) != 1 {
		t.Fatalf("sent PM not backfilled: expected 1, got %d", len(msgs))
	}
	if msgs, _ := s.GetPrivateMessages(net.ID, "alice", "testuser", 50); len(msgs) != 1 {
		t.Fatalf("received PM not backfilled: expected 1, got %d", len(msgs))
	}
}

// ---------- Settings (durable key/value prefs) ----------

// TestSettingsPersistAcrossReopen is the regression test for theme/accent not
// surviving an app restart. The UI used to persist these in the WKWebView's
// localStorage, which macOS does not retain across launches, so the theme reset
// on every relaunch. They now live in SQLite, so a value written in one session
// must be readable after the database is closed and reopened — the storage-layer
// equivalent of relaunching the app.
func TestSettingsPersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "settings.db")

	s, err := NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	// A missing key reads back as empty (not an error) so the frontend can fall
	// back to its built-in defaults.
	if got, err := s.GetSetting("theme.mode"); err != nil {
		t.Fatalf("GetSetting(missing): %v", err)
	} else if got != "" {
		t.Errorf("expected empty string for missing key, got %q", got)
	}

	if err := s.SetSetting("theme.mode", "dark"); err != nil {
		t.Fatalf("SetSetting(theme.mode): %v", err)
	}
	if err := s.SetSetting("theme.accent", "rose"); err != nil {
		t.Fatalf("SetSetting(theme.accent): %v", err)
	}
	// Upsert: writing the same key again replaces the value.
	if err := s.SetSetting("theme.mode", "light"); err != nil {
		t.Fatalf("SetSetting(theme.mode update): %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen the same database file — the storage-layer equivalent of relaunching.
	s2, err := NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage(reopen): %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	if mode, err := s2.GetSetting("theme.mode"); err != nil {
		t.Fatalf("GetSetting(theme.mode): %v", err)
	} else if mode != "light" {
		t.Errorf("theme.mode: expected %q after reopen, got %q", "light", mode)
	}
	if accent, err := s2.GetSetting("theme.accent"); err != nil {
		t.Fatalf("GetSetting(theme.accent): %v", err)
	} else if accent != "rose" {
		t.Errorf("theme.accent: expected %q after reopen, got %q", "rose", accent)
	}
}

func TestNetwork_IdentifyAsBot_RoundTrips(t *testing.T) {
	s := newTestStorage(t)
	now := time.Now()
	n := &Network{
		Name: "libera", Address: "irc.libera.chat", Port: 6697, TLS: true,
		Nickname: "robodan", Username: "robodan", Realname: "Robo Dan",
		IdentifyAsBot: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateNetwork(n); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	got, err := s.GetNetwork(n.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if !got.IdentifyAsBot {
		t.Fatalf("IdentifyAsBot did not persist: got false, want true")
	}
}
