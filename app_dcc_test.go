package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/dcc"
	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/notification"
	"github.com/matt0x6f/irc-client/internal/storage"
)

func newFileTransferTestApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	a.dataDir = t.TempDir()
	a.ircClients = make(map[int64]*irc.IRCClient)
	a.notifier = notification.NewNotifier()
	if err := a.initializeFileTransfers(); err != nil {
		t.Fatalf("initializeFileTransfers: %v", err)
	}
	t.Cleanup(a.dccManager.Close)
	return a
}

func TestFileTransferSettingsDefaultAndDisabledGate(t *testing.T) {
	a := newFileTransferTestApp(t)
	settings := a.GetFileTransferSettings()
	if !settings.Enabled || settings.HistoryRetention != dcc.HistoryForever || settings.ConnectionMode != dcc.ConnectionAutomatic {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
	settings.Enabled = false
	if err := a.UpdateFileTransferSettings(settings, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := a.dccManager.HandleControl(1, "Test", "alice", `SEND photo.jpg 3405803786 5000 12`); err != nil {
		t.Fatalf("disabled offers should be ignored: %v", err)
	}
	if got := a.GetActiveFileTransfers(); len(got) != 0 {
		t.Fatalf("disabled feature retained offer: %+v", got)
	}
}

func TestClearFileTransferHistoryLeavesCompletedFile(t *testing.T) {
	a := newFileTransferTestApp(t)
	path := filepath.Join(t.TempDir(), "received.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := a.storage.UpsertFileTransfer(storage.FileTransfer{
		TransferID: "done", NetworkName: "Test", Peer: "alice", Direction: "incoming",
		Filename: "received.txt", LocalPath: path, SizeBytes: 5, TransferredBytes: 5,
		State: "completed", CreatedAt: now, UpdatedAt: now, FinishedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.ClearFileTransferHistory(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("completed file was removed with metadata: %v", err)
	}
	page, err := a.ListFileTransferHistory("", "", "", 50)
	if err != nil || len(page.Transfers) != 0 {
		t.Fatalf("history was not cleared: page=%+v err=%v", page, err)
	}
}
