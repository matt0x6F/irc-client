package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/security"
	"github.com/matt0x6f/irc-client/internal/storage"
	"github.com/zalando/go-keyring"
)

func newCredsTestApp(t *testing.T) *App {
	t.Helper()
	keyring.MockInit() // in-memory keychain
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := storage.NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &App{
		storage:  s,
		eventBus: events.NewEventBus(),
		creds:    security.NewCredentialStore(security.NewKeychain()),
	}
}

// Saving a network must route secrets into the keychain and leave the SQLite
// columns blank, and the bound getter must mask secrets while exposing Has*
// flags (S1 + S2).
func TestSaveNetworkStoresSecretsInKeychainNotDB(t *testing.T) {
	a := newCredsTestApp(t)
	err := a.SaveNetwork(NetworkConfig{
		Name: "libera", Nickname: "me", Username: "me", Realname: "Me",
		Address: "irc.libera.chat", Port: 6697, TLS: true,
		Password:    "serverpw",
		SASLEnabled: true, SASLMechanism: "PLAIN", SASLUsername: "me", SASLPassword: "saslpw",
	})
	if err != nil {
		t.Fatalf("SaveNetwork: %v", err)
	}

	raw, err := a.storage.GetNetworks()
	if err != nil || len(raw) != 1 {
		t.Fatalf("GetNetworks: err=%v n=%d", err, len(raw))
	}
	if raw[0].Password != "" {
		t.Errorf("server password persisted in DB column: %q", raw[0].Password)
	}
	if raw[0].SASLPassword != nil && *raw[0].SASLPassword != "" {
		t.Errorf("sasl password persisted in DB column: %q", *raw[0].SASLPassword)
	}

	if got := a.creds.Resolve(raw[0].ID, security.FieldPassword, ""); got != "serverpw" {
		t.Errorf("keychain server pw = %q, want serverpw", got)
	}
	if got := a.creds.Resolve(raw[0].ID, security.FieldSASLPassword, ""); got != "saslpw" {
		t.Errorf("keychain sasl pw = %q, want saslpw", got)
	}

	nets, err := a.GetNetworks()
	if err != nil {
		t.Fatal(err)
	}
	if nets[0].Password != "" || nets[0].SASLPassword != nil {
		t.Errorf("bound GetNetworks leaked secrets: pw=%q sasl=%v", nets[0].Password, nets[0].SASLPassword)
	}
	if !nets[0].HasPassword || !nets[0].HasSASLPassword {
		t.Errorf("Has* flags = %v/%v, want true/true", nets[0].HasPassword, nets[0].HasSASLPassword)
	}
	if nets[0].CredentialStorageInsecure {
		t.Errorf("keychain available → CredentialStorageInsecure should be false")
	}
}

// An empty secret on re-save means "unchanged" — it must not wipe the stored
// secret (masked field left untouched by the user).
func TestSaveNetworkEmptyPasswordPreservesExisting(t *testing.T) {
	a := newCredsTestApp(t)
	cfg := NetworkConfig{
		Name: "libera", Nickname: "me", Username: "me", Realname: "Me",
		Address: "irc.libera.chat", Port: 6697, TLS: true, Password: "serverpw",
	}
	if err := a.SaveNetwork(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Realname = "New Name"
	cfg.Password = "" // untouched masked field
	if err := a.SaveNetwork(cfg); err != nil {
		t.Fatal(err)
	}

	raw, _ := a.storage.GetNetworks()
	if got := a.creds.Resolve(raw[0].ID, security.FieldPassword, raw[0].Password); got != "serverpw" {
		t.Errorf("empty re-save wiped the password; got %q want serverpw", got)
	}
}

// Deleting a network must remove its keychain secrets.
func TestDeleteNetworkClearsKeychain(t *testing.T) {
	a := newCredsTestApp(t)
	if err := a.SaveNetwork(NetworkConfig{
		Name: "x", Nickname: "me", Username: "me", Realname: "Me",
		Address: "h", Port: 6697, TLS: true, Password: "pw",
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := a.storage.GetNetworks()
	id := raw[0].ID
	if err := a.DeleteNetwork(id); err != nil {
		t.Fatal(err)
	}
	if got := a.creds.Resolve(id, security.FieldPassword, ""); got != "" {
		t.Errorf("keychain secret survived DeleteNetwork: %q", got)
	}
}
