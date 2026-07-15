package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func testFileTransfer(id string, finishedAt *time.Time) FileTransfer {
	createdAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	return FileTransfer{
		TransferID:       id,
		NetworkName:      "Libera.Chat",
		Peer:             "Alice",
		Direction:        "incoming",
		Filename:         id + ".txt",
		LocalPath:        filepath.Join("/tmp", id+".txt"),
		PartialPath:      filepath.Join("/tmp", id+".txt.part"),
		SizeBytes:        1 << 33,
		TransferredBytes: 1024,
		State:            "transferring",
		Resumable:        true,
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
		FinishedAt:       finishedAt,
	}
}

func TestFileTransferUpsertAndActiveRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	network := makeNetwork("TransferNet")
	if err := s.CreateNetwork(network); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	transfer := testFileTransfer("active-1", nil)
	transfer.NetworkID = &network.ID
	if err := s.UpsertFileTransfer(transfer); err != nil {
		t.Fatalf("UpsertFileTransfer: %v", err)
	}

	got, err := s.GetFileTransfer(transfer.TransferID)
	if err != nil {
		t.Fatalf("GetFileTransfer: %v", err)
	}
	if got == nil || got.NetworkID == nil || *got.NetworkID != network.ID {
		t.Fatalf("network ID did not round trip: %+v", got)
	}
	if got.SizeBytes != 1<<33 || !got.Resumable || got.LocalPath != transfer.LocalPath {
		t.Fatalf("unexpected transfer: %+v", got)
	}

	transfer.TransferredBytes = 4096
	transfer.State = "receiving"
	transfer.UpdatedAt = transfer.UpdatedAt.Add(time.Second)
	if err := s.UpsertFileTransfer(transfer); err != nil {
		t.Fatalf("update transfer: %v", err)
	}
	active, err := s.ListActiveFileTransfers()
	if err != nil {
		t.Fatalf("ListActiveFileTransfers: %v", err)
	}
	if len(active) != 1 || active[0].TransferredBytes != 4096 || active[0].State != "receiving" {
		t.Fatalf("unexpected active rows: %+v", active)
	}
	if active[0].CreatedAt.IsZero() || !active[0].CreatedAt.Equal(transfer.CreatedAt) {
		t.Fatalf("upsert changed created_at: got %s want %s", active[0].CreatedAt, transfer.CreatedAt)
	}
}

func TestFileTransferHistoryPaginationSearchAndDirection(t *testing.T) {
	s := newTestStorage(t)
	base := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)
	fixtures := []FileTransfer{
		testFileTransfer("transfer-a", timePtr(base)),
		testFileTransfer("transfer-b", timePtr(base)),
		testFileTransfer("transfer-c", timePtr(base.Add(-time.Minute))),
		testFileTransfer("transfer-d", timePtr(base.Add(-2*time.Minute))),
	}
	fixtures[1].Direction = "outgoing"
	fixtures[1].Peer = "Bob"
	fixtures[1].Filename = "Report.PDF"
	fixtures[2].NetworkName = "OFTC"
	for _, transfer := range fixtures {
		transfer.State = "completed"
		transfer.UpdatedAt = *transfer.FinishedAt
		if err := s.UpsertFileTransfer(transfer); err != nil {
			t.Fatalf("UpsertFileTransfer(%s): %v", transfer.TransferID, err)
		}
	}

	first, err := s.ListFileTransferHistory("", "", nil, 2)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Transfers) != 2 || first.Next == nil {
		t.Fatalf("unexpected first page: %+v", first)
	}
	if first.Transfers[0].TransferID != "transfer-b" || first.Transfers[1].TransferID != "transfer-a" {
		t.Fatalf("history tie ordering is not stable: %+v", first.Transfers)
	}
	second, err := s.ListFileTransferHistory("", "", first.Next, 2)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Transfers) != 2 || second.Next != nil || second.Transfers[0].TransferID != "transfer-c" {
		t.Fatalf("unexpected second page: %+v", second)
	}

	outgoing, err := s.ListFileTransferHistory("outgoing", "report", nil, 20)
	if err != nil {
		t.Fatalf("filtered history: %v", err)
	}
	if len(outgoing.Transfers) != 1 || outgoing.Transfers[0].TransferID != "transfer-b" {
		t.Fatalf("direction/filename filter mismatch: %+v", outgoing.Transfers)
	}
	networkSearch, err := s.ListFileTransferHistory("", "oftc", nil, 20)
	if err != nil {
		t.Fatalf("network search: %v", err)
	}
	if len(networkSearch.Transfers) != 1 || networkSearch.Transfers[0].TransferID != "transfer-c" {
		t.Fatalf("network search mismatch: %+v", networkSearch.Transfers)
	}
}

func TestFileTransferHistoryRemovalClearAndPruneKeepActive(t *testing.T) {
	s := newTestStorage(t)
	now := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)
	active := testFileTransfer("active", nil)
	old := testFileTransfer("old", timePtr(now.Add(-31*24*time.Hour)))
	recent := testFileTransfer("recent", timePtr(now.Add(-24*time.Hour)))
	for _, transfer := range []FileTransfer{active, old, recent} {
		if transfer.FinishedAt != nil {
			transfer.State = "completed"
		}
		if err := s.UpsertFileTransfer(transfer); err != nil {
			t.Fatalf("upsert %s: %v", transfer.TransferID, err)
		}
	}

	// Removing history must not delete an active row.
	if err := s.RemoveFileTransferHistory("active"); err != nil {
		t.Fatalf("remove active: %v", err)
	}
	if got, _ := s.GetFileTransfer("active"); got == nil {
		t.Fatal("active transfer was removed as history")
	}

	if err := s.PruneFileTransferHistory(now.Add(-30 * 24 * time.Hour)); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if got, _ := s.GetFileTransfer("old"); got != nil {
		t.Fatal("old terminal transfer was not pruned")
	}
	if got, _ := s.GetFileTransfer("recent"); got == nil {
		t.Fatal("recent terminal transfer was pruned")
	}

	if err := s.ClearFileTransferHistory(); err != nil {
		t.Fatalf("clear history: %v", err)
	}
	if got, _ := s.GetFileTransfer("recent"); got != nil {
		t.Fatal("terminal transfer survived clear")
	}
	if got, _ := s.GetFileTransfer("active"); got == nil {
		t.Fatal("active transfer was deleted by clear")
	}
}

func TestMigrateFileTransfersIsIdempotent(t *testing.T) {
	s := newTestStorage(t)
	if err := migrateFileTransfers(s.db); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := migrateFileTransfers(s.db); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	for _, name := range []string{"file_transfers", "idx_file_transfers_active", "idx_file_transfers_history"} {
		var count int
		if err := s.db.Get(&count, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name); err != nil {
			t.Fatalf("inspect %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("expected %s to exist once, count=%d", name, count)
		}
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}
