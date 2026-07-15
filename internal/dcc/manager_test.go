package dcc

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testSettings(t *testing.T) Settings {
	return Settings{Enabled: true, DownloadDirectory: t.TempDir(), HistoryRetention: HistoryForever, ConnectionMode: ConnectionAutomatic}
}

func TestManagerDisabledIgnoresOffers(t *testing.T) {
	s := testSettings(t)
	s.Enabled = false
	m, err := NewManager(s, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.HandleControl(1, "net", "alice", `SEND file.zip 3405803781 5000 42`); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot(false); len(got) != 0 {
		t.Fatalf("disabled manager recorded %d offers", len(got))
	}
}

func TestDisableRequiresExplicitCancellationForPendingOffer(t *testing.T) {
	m, err := NewManager(testSettings(t), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.HandleControl(1, "net", "alice", `SEND file.zip 3405803781 5000 42`); err != nil {
		t.Fatal(err)
	}
	disabled := testSettings(t)
	disabled.Enabled = false
	if err := m.UpdateSettings(disabled, false); err == nil {
		t.Fatal("disabling with a pending offer should require cancelActive")
	}
	if err := m.UpdateSettings(disabled, true); err != nil {
		t.Fatal(err)
	}
	views := m.Snapshot(false)
	if len(views) != 1 || views[0].Status != StatusCanceled {
		t.Fatalf("pending offer was not canceled: %+v", views)
	}
}

func TestQueueOutgoingSequential(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "one.txt"), filepath.Join(dir, "two.txt")}
	for i, p := range paths {
		if err := os.WriteFile(p, []byte{byte(i + 1)}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var controls []string
	m, err := NewManager(testSettings(t), func(_ int64, _ string, payload string) error {
		mu.Lock()
		controls = append(controls, payload)
		mu.Unlock()
		return nil
	}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	views, err := m.QueueOutgoing(1, "net", "alice", paths)
	if err != nil || len(views) != 2 {
		t.Fatalf("QueueOutgoing = %d, %v", len(views), err)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(controls) != 1 {
		t.Fatalf("got %d offers, want one active offer", len(controls))
	}
}

func TestTransportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	partial := filepath.Join(dir, "received.part")
	want := make([]byte, 256*1024+17)
	for i := range want {
		want[i] = byte(i % 251)
	}
	if err := os.WriteFile(source, want, 0o600); err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errCh := make(chan error, 2)
	go func() { errCh <- sendFile(ctx, left, source, 0, int64(len(want)), func(int64) {}) }()
	go func() { errCh <- receiveFile(ctx, right, partial, 0, int64(len(want)), func(int64) {}) }()
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(partial)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("received bytes differ")
	}
}

func TestReceiveRejectsShortFile(t *testing.T) {
	left, right := net.Pipe()
	go func() {
		_, _ = io.WriteString(left, "short")
		_ = left.Close()
	}()
	err := receiveFile(context.Background(), right, filepath.Join(t.TempDir(), "x.part"), 0, 100, func(int64) {})
	if err == nil {
		t.Fatal("expected short transfer error")
	}
}

func TestSendRejectsChangedSourceSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changed.bin")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	if err := sendFile(context.Background(), left, path, 0, 99, func(int64) {}); err == nil {
		t.Fatal("expected changed source size to be rejected")
	}
}

func TestFinalizePartialNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "file.part")
	destination := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(partial, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := finalizePartial(partial, destination); err == nil {
		t.Fatal("expected overwrite to be rejected")
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != "old" {
		t.Fatalf("destination changed: %q, %v", got, err)
	}
}

func TestRestorePersistsTerminalState(t *testing.T) {
	var persisted []Transfer
	m, err := NewManager(testSettings(t), nil, func(transfer Transfer) error {
		persisted = append(persisted, transfer)
		return nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	now := time.Now().Add(-time.Minute)
	m.Restore([]Transfer{{ID: "out", Direction: DirectionOutgoing, Status: StatusTransferring, CreatedAt: now, UpdatedAt: now}})
	if len(persisted) != 1 || persisted[0].Status != StatusFailed || persisted[0].CompletedAt == nil {
		t.Fatalf("restored transfer not made durable: %+v", persisted)
	}
}

func TestProgressEventsAreThrottled(t *testing.T) {
	var mu sync.Mutex
	events := 0
	m, err := NewManager(testSettings(t), nil, nil, nil, func(event Event) {
		if event.Type == "upsert" {
			mu.Lock()
			events++
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	now := time.Now()
	m.items["x"] = &Transfer{ID: "x", Status: StatusTransferring, TotalBytes: 1000, UpdatedAt: now}
	for i := int64(1); i <= 100; i++ {
		m.progress("x", i)
	}
	mu.Lock()
	got := events
	mu.Unlock()
	if got > 1 {
		t.Fatalf("100 immediate progress samples emitted %d events", got)
	}
}
