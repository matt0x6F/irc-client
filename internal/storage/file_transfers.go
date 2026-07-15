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

const (
	defaultFileTransferHistoryPageSize = 50
	maxFileTransferHistoryPageSize     = 200
)

// FileTransferHistoryCursor is the stable keyset cursor used for transfer
// history. FinishedAt is the primary sort key and TransferID breaks ties.
type FileTransferHistoryCursor struct {
	FinishedAt time.Time
	TransferID string
}

// FileTransferHistoryPage is one newest-first page of terminal transfers.
type FileTransferHistoryPage struct {
	Transfers []FileTransfer
	Next      *FileTransferHistoryCursor
}

// UpsertFileTransfer stores a new transfer or a state/progress checkpoint. The
// transfer manager owns transition validation; storage preserves its snapshot.
func (s *Storage) UpsertFileTransfer(transfer FileTransfer) error {
	s.closedMu.RLock()
	closed := s.closed
	s.closedMu.RUnlock()
	if closed {
		return fmt.Errorf("storage is closed")
	}
	if transfer.TransferID == "" {
		return fmt.Errorf("file transfer ID is required")
	}

	params := convertFileTransferToUpsertParams(transfer)
	if err := s.queries.UpsertFileTransfer(context.Background(), params); err != nil {
		return fmt.Errorf("upsert file transfer %q: %w", transfer.TransferID, err)
	}
	return nil
}

// GetFileTransfer returns nil when transferID is not present.
func (s *Storage) GetFileTransfer(transferID string) (*FileTransfer, error) {
	row, err := s.queries.GetFileTransfer(context.Background(), transferID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get file transfer %q: %w", transferID, err)
	}
	transfer := convertFileTransferFromDB(row)
	return &transfer, nil
}

// ListActiveFileTransfers returns pending and active rows oldest first. A row is
// active whenever finished_at is NULL; terminal state persistence must set it.
func (s *Storage) ListActiveFileTransfers() ([]FileTransfer, error) {
	rows, err := s.queries.ListActiveFileTransfers(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list active file transfers: %w", err)
	}
	return convertFileTransfersFromDB(rows), nil
}

// ListFileTransferHistory returns terminal rows newest first. direction is
// optional (empty, "incoming", or "outgoing") and search matches filename,
// peer, or the snapshotted network name case-insensitively.
func (s *Storage) ListFileTransferHistory(direction, search string, cursor *FileTransferHistoryCursor, limit int) (FileTransferHistoryPage, error) {
	if limit <= 0 {
		limit = defaultFileTransferHistoryPageSize
	}
	if limit > maxFileTransferHistoryPageSize {
		limit = maxFileTransferHistoryPageSize
	}
	direction = strings.TrimSpace(direction)
	search = strings.TrimSpace(search)

	queryLimit := int64(limit + 1)
	var (
		rows []db.FileTransfer
		err  error
	)
	if cursor == nil {
		rows, err = s.queries.ListFileTransferHistory(context.Background(), db.ListFileTransferHistoryParams{
			Direction: direction,
			Search:    search,
			PageLimit: queryLimit,
		})
	} else {
		if cursor.TransferID == "" || cursor.FinishedAt.IsZero() {
			return FileTransferHistoryPage{}, fmt.Errorf("invalid file transfer history cursor")
		}
		rows, err = s.queries.ListFileTransferHistoryAfter(context.Background(), db.ListFileTransferHistoryAfterParams{
			CursorFinishedAt: sql.NullTime{Time: cursor.FinishedAt.UTC(), Valid: true},
			CursorTransferID: cursor.TransferID,
			Direction:        direction,
			Search:           search,
			PageLimit:        queryLimit,
		})
	}
	if err != nil {
		return FileTransferHistoryPage{}, fmt.Errorf("list file transfer history: %w", err)
	}

	hasNext := len(rows) > limit
	if hasNext {
		rows = rows[:limit]
	}
	page := FileTransferHistoryPage{Transfers: convertFileTransfersFromDB(rows)}
	if hasNext && len(page.Transfers) > 0 {
		last := page.Transfers[len(page.Transfers)-1]
		if last.FinishedAt != nil {
			page.Next = &FileTransferHistoryCursor{
				FinishedAt: last.FinishedAt.UTC(),
				TransferID: last.TransferID,
			}
		}
	}
	return page, nil
}

// RemoveFileTransferHistory removes a terminal transfer's metadata only. It
// deliberately cannot remove an active row or any file from disk.
func (s *Storage) RemoveFileTransferHistory(transferID string) error {
	if err := s.queries.DeleteFileTransferHistoryEntry(context.Background(), transferID); err != nil {
		return fmt.Errorf("remove file transfer history %q: %w", transferID, err)
	}
	return nil
}

// ClearFileTransferHistory deletes all terminal transfer metadata. Active rows
// and files on disk are left untouched.
func (s *Storage) ClearFileTransferHistory() error {
	if err := s.queries.ClearFileTransferHistory(context.Background()); err != nil {
		return fmt.Errorf("clear file transfer history: %w", err)
	}
	return nil
}

// PruneFileTransferHistory deletes terminal metadata older than cutoff. Active
// rows and files on disk are left untouched.
func (s *Storage) PruneFileTransferHistory(cutoff time.Time) error {
	if cutoff.IsZero() {
		return fmt.Errorf("file transfer history cutoff is required")
	}
	if err := s.queries.PruneFileTransferHistory(context.Background(), sql.NullTime{
		Time: cutoff.UTC(), Valid: true,
	}); err != nil {
		return fmt.Errorf("prune file transfer history: %w", err)
	}
	return nil
}
