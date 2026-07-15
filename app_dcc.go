package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/matt0x6f/irc-client/internal/dcc"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/notification"
	"github.com/matt0x6f/irc-client/internal/storage"
)

const (
	settingFileTransfersEnabled    = "fileTransfers.enabled"
	settingFileTransfersDirectory  = "fileTransfers.downloadDirectory"
	settingFileTransfersRetention  = "fileTransfers.historyRetention"
	settingFileTransfersMode       = "fileTransfers.connectionMode"
	settingFileTransfersAddress    = "fileTransfers.advertisedAddress"
	settingFileTransfersPortMin    = "fileTransfers.portMin"
	settingFileTransfersPortMax    = "fileTransfers.portMax"
	settingDirectChatEnabled       = "directChat.enabled"
	fileTransferHistoryDefaultPage = 50
	fileTransferHistoryMaxPage     = 200
)

// FileTransferPage is the paginated Wails response for the History tab.
type FileTransferPage struct {
	Transfers  []dcc.View `json:"transfers"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

type fileTransferCursor struct {
	FinishedAt time.Time `json:"finishedAt"`
	TransferID string    `json:"transferId"`
}

func (a *App) initializeFileTransfers() error {
	settings := a.loadFileTransferSettings()
	manager, err := dcc.NewManager(
		settings,
		func(networkID int64, peer, payload string) error {
			a.mu.RLock()
			client := a.ircClients[networkID]
			a.mu.RUnlock()
			if client == nil || !client.IsConnected() {
				return fmt.Errorf("the IRC network is not connected")
			}
			return client.SendCTCPRequest(peer, "DCC", payload)
		},
		func(t dcc.Transfer) error {
			current := a.dccManager
			if current != nil && current.Settings().HistoryRetention == dcc.HistoryNone && !t.Status.Active() && !t.Resumable {
				return a.storage.RemoveFileTransferHistory(t.ID)
			}
			return a.storage.UpsertFileTransfer(dccTransferToStorage(t))
		},
		func(id string) error { return a.storage.RemoveFileTransferHistory(id) },
		func(event dcc.Event) { a.handleFileTransferEvent(event) },
	)
	if err != nil {
		return err
	}
	a.dccManager = manager
	rows, err := a.storage.ListActiveFileTransfers()
	if err != nil {
		return err
	}
	restored := make([]dcc.Transfer, 0, len(rows))
	for _, row := range rows {
		restored = append(restored, storageTransferToDCC(row))
	}
	manager.Restore(restored)
	if settings.HistoryRetention == dcc.History30Days {
		_ = a.storage.PruneFileTransferHistory(time.Now().Add(-30 * 24 * time.Hour))
	}
	return nil
}

func (a *App) loadFileTransferSettings() dcc.Settings {
	downloads := ""
	if home, err := os.UserHomeDir(); err == nil {
		downloads = filepath.Join(home, "Downloads", "Cascade")
	}
	if downloads == "" {
		downloads = filepath.Join(a.dataDir, "Downloads")
	}
	get := func(key string) string {
		value, _ := a.storage.GetSetting(key)
		return strings.TrimSpace(value)
	}
	settings := dcc.Settings{
		Enabled: true, DownloadDirectory: downloads, HistoryRetention: dcc.HistoryForever,
		ConnectionMode: dcc.ConnectionAutomatic,
	}
	if get(settingFileTransfersEnabled) == "false" {
		settings.Enabled = false
	}
	if value := get(settingFileTransfersDirectory); value != "" {
		settings.DownloadDirectory = value
	}
	if value := dcc.HistoryRetention(get(settingFileTransfersRetention)); value == dcc.History30Days || value == dcc.HistoryNone {
		settings.HistoryRetention = value
	}
	if value := dcc.ConnectionMode(get(settingFileTransfersMode)); value == dcc.ConnectionClassic || value == dcc.ConnectionPassive {
		settings.ConnectionMode = value
	}
	settings.AdvertisedAddress = get(settingFileTransfersAddress)
	settings.PortMin, _ = strconv.Atoi(get(settingFileTransfersPortMin))
	settings.PortMax, _ = strconv.Atoi(get(settingFileTransfersPortMax))
	if err := dcc.ValidateSettings(settings); err != nil {
		logger.Log.Warn().Err(err).Msg("Ignoring invalid file transfer settings")
		settings = dcc.Settings{Enabled: true, DownloadDirectory: downloads, HistoryRetention: dcc.HistoryForever, ConnectionMode: dcc.ConnectionAutomatic}
	}
	return settings
}

func (a *App) handleFileTransferEvent(event dcc.Event) {
	a.emit("file-transfer:event", event)
	if event.Type != "upsert" || event.Transfer == nil {
		return
	}
	t := event.Transfer
	switch t.Status {
	case dcc.StatusOffered:
		a.sendNotification(notification.Notification{
			ID: newNotificationID(), Title: fmt.Sprintf("%s wants to send you a file", t.Peer),
			Body:       fmt.Sprintf("%s · %s", t.Filename, formatByteCount(t.TotalBytes)),
			CategoryID: notifyCategoryTransfer,
			Data:       map[string]any{"networkId": strconv.FormatInt(t.NetworkID, 10), "target": "file-transfers", "kind": "file-transfer"},
		})
	case dcc.StatusCompleted:
		a.sendNotification(notification.Notification{
			ID: newNotificationID(), Title: "File transfer complete", Body: t.Filename,
			CategoryID: notifyCategoryTransfer,
			Data:       map[string]any{"networkId": strconv.FormatInt(t.NetworkID, 10), "target": "file-transfers", "kind": "file-transfer"},
		})
	case dcc.StatusFailed, dcc.StatusResumable:
		a.sendNotification(notification.Notification{
			ID: newNotificationID(), Title: "File transfer needs attention", Body: t.Filename,
			CategoryID: notifyCategoryTransfer,
			Data:       map[string]any{"networkId": strconv.FormatInt(t.NetworkID, 10), "target": "file-transfers", "kind": "file-transfer"},
		})
	}
}

func (a *App) GetActiveFileTransfers() []dcc.View {
	if a.dccManager == nil {
		return []dcc.View{}
	}
	return a.dccManager.Snapshot(true)
}

func (a *App) ListFileTransferHistory(direction, search, cursor string, limit int) (FileTransferPage, error) {
	if limit <= 0 {
		limit = fileTransferHistoryDefaultPage
	}
	if limit > fileTransferHistoryMaxPage {
		limit = fileTransferHistoryMaxPage
	}
	var storageCursor *storage.FileTransferHistoryCursor
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return FileTransferPage{}, fmt.Errorf("invalid history cursor")
		}
		var parsed fileTransferCursor
		if err := json.Unmarshal(decoded, &parsed); err != nil || parsed.TransferID == "" || parsed.FinishedAt.IsZero() {
			return FileTransferPage{}, fmt.Errorf("invalid history cursor")
		}
		storageCursor = &storage.FileTransferHistoryCursor{FinishedAt: parsed.FinishedAt, TransferID: parsed.TransferID}
	}
	page, err := a.storage.ListFileTransferHistory(direction, search, storageCursor, limit)
	if err != nil {
		return FileTransferPage{}, err
	}
	out := FileTransferPage{Transfers: make([]dcc.View, 0, len(page.Transfers))}
	for _, row := range page.Transfers {
		t := storageTransferToDCC(row)
		if _, err := os.Stat(t.LocalPath); err == nil {
			t.FileAvailable = true
		}
		out.Transfers = append(out.Transfers, t.View())
	}
	if page.Next != nil {
		raw, _ := json.Marshal(fileTransferCursor{FinishedAt: page.Next.FinishedAt, TransferID: page.Next.TransferID})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}

func (a *App) ChooseAndSendFiles(networkID int64, peer string) ([]dcc.View, error) {
	if a.app == nil {
		return nil, fmt.Errorf("application is not ready")
	}
	peer = strings.TrimSpace(peer)
	if peer == "" {
		return nil, fmt.Errorf("recipient is required")
	}
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return nil, err
	}
	paths, err := a.app.Dialog.OpenFile().SetTitle("Send files to " + peer).SetButtonText("Choose files").PromptForMultipleSelection()
	if err != nil || len(paths) == 0 {
		return []dcc.View{}, err
	}
	return a.dccManager.QueueOutgoing(networkID, network.Name, peer, paths)
}

func (a *App) AcceptFileTransfer(id string) error {
	t, ok := a.dccManager.Get(id)
	if !ok {
		return fmt.Errorf("file transfer was not found")
	}
	if t.PartialPath != "" && t.LocalPath != "" {
		return a.dccManager.Accept(id, t.LocalPath)
	}
	if a.app == nil {
		return fmt.Errorf("application is not ready")
	}
	settings := a.dccManager.Settings()
	path, err := a.app.Dialog.SaveFile().SetDirectory(settings.DownloadDirectory).SetFilename(t.Filename).SetButtonText("Save file from " + t.Peer).PromptForSingleSelection()
	if err != nil || path == "" {
		return err
	}
	return a.dccManager.Accept(id, path)
}

func (a *App) DeclineFileTransfer(id string) error { return a.dccManager.Decline(id) }
func (a *App) CancelFileTransfer(id string) error  { return a.dccManager.Cancel(id) }
func (a *App) RetryFileTransfer(id string) error   { return a.dccManager.Retry(id) }
func (a *App) DiscardPartialFileTransfer(id string) error {
	return a.dccManager.DiscardPartial(id)
}

func (a *App) RemoveFileTransferHistory(id string) error {
	if _, ok := a.dccManager.Get(id); ok {
		return a.dccManager.RemoveHistory(id)
	}
	return a.storage.RemoveFileTransferHistory(id)
}

func (a *App) ClearFileTransferHistory() error {
	if err := a.storage.ClearFileTransferHistory(); err != nil {
		return err
	}
	a.dccManager.ClearHistory()
	a.emit("file-transfer:event", dcc.Event{Type: "reset"})
	return nil
}

func (a *App) GetFileTransferSettings() dcc.Settings { return a.dccManager.Settings() }

func (a *App) UpdateFileTransferSettings(settings dcc.Settings, cancelActive bool) error {
	if err := dcc.ValidateSettings(settings); err != nil {
		return err
	}
	if err := os.MkdirAll(settings.DownloadDirectory, 0o755); err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}
	if err := a.dccManager.UpdateSettings(settings, cancelActive); err != nil {
		return err
	}
	values := map[string]string{
		settingFileTransfersEnabled: boolString(settings.Enabled), settingFileTransfersDirectory: settings.DownloadDirectory,
		settingFileTransfersRetention: string(settings.HistoryRetention), settingFileTransfersMode: string(settings.ConnectionMode),
		settingFileTransfersAddress: settings.AdvertisedAddress, settingFileTransfersPortMin: strconv.Itoa(settings.PortMin),
		settingFileTransfersPortMax: strconv.Itoa(settings.PortMax),
	}
	for key, value := range values {
		if err := a.storage.SetSetting(key, value); err != nil {
			return err
		}
		a.emit("setting:changed", map[string]string{"key": key, "value": value})
	}
	if settings.HistoryRetention == dcc.History30Days {
		return a.storage.PruneFileTransferHistory(time.Now().Add(-30 * 24 * time.Hour))
	}
	if settings.HistoryRetention == dcc.HistoryNone {
		return a.ClearFileTransferHistory()
	}
	return nil
}

func (a *App) ChooseFileTransferDirectory() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("application is not ready")
	}
	current := a.dccManager.Settings().DownloadDirectory
	return a.app.Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).SetTitle("Choose where received files are saved").SetDirectory(current).SetButtonText("Choose folder").PromptForSingleSelection()
}

func (a *App) OpenFileTransfer(id string) error {
	if a.app == nil {
		return fmt.Errorf("application is not ready")
	}
	t, err := a.lookupFileTransfer(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(t.LocalPath); err != nil {
		return fmt.Errorf("file no longer found")
	}
	return a.app.Browser.OpenFile(t.LocalPath)
}

func (a *App) RevealFileTransfer(id string) error {
	t, err := a.lookupFileTransfer(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(t.LocalPath); err != nil {
		return fmt.Errorf("file no longer found")
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", t.LocalPath).Start()
	case "windows":
		return exec.Command("explorer.exe", "/select,", t.LocalPath).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(t.LocalPath)).Start()
	}
}

func (a *App) LocateFileTransfer(id string) error {
	if a.app == nil {
		return fmt.Errorf("application is not ready")
	}
	path, err := a.app.Dialog.OpenFile().SetTitle("Locate transferred file").SetButtonText("Locate file").PromptForSingleSelection()
	if err != nil || path == "" {
		return err
	}
	if _, ok := a.dccManager.Get(id); ok {
		return a.dccManager.Locate(id, path)
	}
	row, err := a.storage.GetFileTransfer(id)
	if err != nil || row == nil {
		return fmt.Errorf("file transfer was not found")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != row.SizeBytes {
		return fmt.Errorf("selected file does not match the transferred size")
	}
	row.LocalPath = path
	row.UpdatedAt = time.Now()
	return a.storage.UpsertFileTransfer(*row)
}

func (a *App) lookupFileTransfer(id string) (dcc.Transfer, error) {
	if t, ok := a.dccManager.Get(id); ok {
		return t, nil
	}
	row, err := a.storage.GetFileTransfer(id)
	if err != nil || row == nil {
		return dcc.Transfer{}, fmt.Errorf("file transfer was not found")
	}
	return storageTransferToDCC(*row), nil
}

func dccTransferToStorage(t dcc.Transfer) storage.FileTransfer {
	var networkID *int64
	if t.NetworkID > 0 {
		n := t.NetworkID
		networkID = &n
	}
	return storage.FileTransfer{
		TransferID: t.ID, NetworkID: networkID, NetworkName: t.NetworkName, Peer: t.Peer,
		Direction: string(t.Direction), Filename: t.Filename, LocalPath: t.LocalPath,
		PartialPath: t.PartialPath, SizeBytes: t.TotalBytes, TransferredBytes: t.TransferredBytes,
		State: string(t.Status), Error: t.Error, Resumable: t.Resumable,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, FinishedAt: t.CompletedAt,
	}
}

func storageTransferToDCC(t storage.FileTransfer) dcc.Transfer {
	networkID := int64(0)
	if t.NetworkID != nil {
		networkID = *t.NetworkID
	}
	return dcc.Transfer{
		ID: t.TransferID, NetworkID: networkID, NetworkName: t.NetworkName, Peer: t.Peer,
		Direction: dcc.Direction(t.Direction), Filename: t.Filename, LocalPath: t.LocalPath,
		PartialPath: t.PartialPath, TotalBytes: t.SizeBytes, TransferredBytes: t.TransferredBytes,
		Status: dcc.Status(t.State), Error: t.Error, Resumable: t.Resumable,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, CompletedAt: t.FinishedAt,
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func formatByteCount(size int64) string {
	const unit = int64(1024)
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := unit, 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}
