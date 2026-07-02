package storage

import (
	"testing"
	"time"
)

func channelKeyTestNetwork(t *testing.T, s *Storage) *Network {
	t.Helper()
	net := &Network{Name: "Libera", Address: "irc.libera.chat", Nickname: "nick", CreatedAt: time.Now()}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	return net
}

// A channel key set at creation round-trips through every channel read path,
// so channelsToJoin can rejoin +k channels after a reconnect.
func TestChannelKeyRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	net := channelKeyTestNetwork(t, s)

	ch := &Channel{NetworkID: net.ID, Name: "#secret", Key: "hunter2", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	got, err := s.GetChannelByName(net.ID, "#secret")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if got.Key != "hunter2" {
		t.Errorf("GetChannelByName Key = %q, want %q", got.Key, "hunter2")
	}

	all, err := s.GetChannels(net.ID)
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(all) != 1 || all[0].Key != "hunter2" {
		t.Errorf("GetChannels Key = %+v, want one channel with key hunter2", all)
	}
}

// UpdateChannelKey stores a newly-learned key (and can clear one).
func TestUpdateChannelKey(t *testing.T) {
	s := newTestStorage(t)
	net := channelKeyTestNetwork(t, s)

	ch := &Channel{NetworkID: net.ID, Name: "#secret", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if err := s.UpdateChannelKey(ch.ID, "swordfish"); err != nil {
		t.Fatalf("UpdateChannelKey: %v", err)
	}
	got, err := s.GetChannelByName(net.ID, "#secret")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if got.Key != "swordfish" {
		t.Errorf("Key = %q, want %q", got.Key, "swordfish")
	}

	if err := s.UpdateChannelKey(ch.ID, ""); err != nil {
		t.Fatalf("UpdateChannelKey(clear): %v", err)
	}
	got, _ = s.GetChannelByName(net.ID, "#secret")
	if got.Key != "" {
		t.Errorf("Key = %q after clear, want empty", got.Key)
	}
}

// migrateChannelKey adds the key column to a legacy DB and is idempotent.
func TestMigrateChannelKeyAddsColumn(t *testing.T) {
	s := newTestStorage(t)
	_, _ = s.db.Exec(`ALTER TABLE channels DROP COLUMN "key"`)

	if err := migrateChannelKey(s.db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	var n int
	if err := s.db.Get(&n, "SELECT COUNT(*) FROM pragma_table_info('channels') WHERE name='key'"); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected key column to exist, got count %d", n)
	}
	if err := migrateChannelKey(s.db); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
}
