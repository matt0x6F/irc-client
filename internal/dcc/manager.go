package dcc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultNegotiationTimeout = 2 * time.Minute
	progressInterval          = 250 * time.Millisecond
)

type SendControlFunc func(networkID int64, peer, payload string) error
type PersistFunc func(Transfer) error
type RemoveFunc func(id string) error
type EmitFunc func(Event)

// Manager owns every direct TCP session independently of the IRC connection
// that negotiated it. It deliberately has no dependency on the IRC or storage
// packages; App supplies those capabilities as callbacks.
type Manager struct {
	mu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	settings Settings
	items    map[string]*Transfer
	cancels  map[string]context.CancelFunc
	offers   map[string]Offer

	outgoingQueue  []string
	outgoingActive string

	sendControl SendControlFunc
	persist     PersistFunc
	remove      RemoveFunc
	emit        EmitFunc
	lastEmit    map[string]time.Time
	lastPersist map[string]time.Time
}

func NewManager(settings Settings, send SendControlFunc, persist PersistFunc, remove RemoveFunc, emit EmitFunc) (*Manager, error) {
	if err := ValidateSettings(settings); err != nil {
		return nil, err
	}
	if send == nil {
		send = func(int64, string, string) error { return fmt.Errorf("IRC negotiation is unavailable") }
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		ctx: ctx, cancel: cancel, settings: settings,
		items: make(map[string]*Transfer), cancels: make(map[string]context.CancelFunc),
		offers: make(map[string]Offer), sendControl: send, persist: persist,
		remove: remove, emit: emit, lastEmit: make(map[string]time.Time), lastPersist: make(map[string]time.Time),
	}, nil
}

func ValidateSettings(s Settings) error {
	if s.ConnectionMode != ConnectionAutomatic && s.ConnectionMode != ConnectionClassic && s.ConnectionMode != ConnectionPassive {
		return fmt.Errorf("invalid connection mode")
	}
	if s.HistoryRetention != HistoryForever && s.HistoryRetention != History30Days && s.HistoryRetention != HistoryNone {
		return fmt.Errorf("invalid history retention")
	}
	if s.DownloadDirectory == "" {
		return fmt.Errorf("download directory is required")
	}
	if (s.PortMin == 0) != (s.PortMax == 0) || s.PortMin < 0 || s.PortMax < 0 || s.PortMin > 65535 || s.PortMax > 65535 || s.PortMin > s.PortMax {
		return fmt.Errorf("invalid port range")
	}
	if s.AdvertisedAddress != "" && net.ParseIP(strings.TrimSpace(s.AdvertisedAddress)) == nil {
		return fmt.Errorf("invalid advertised address")
	}
	if s.AdvertisedAddress != "" {
		if _, err := validateRemoteIP(net.ParseIP(strings.TrimSpace(s.AdvertisedAddress))); err != nil {
			return fmt.Errorf("unsafe advertised address")
		}
	}
	return nil
}

func (m *Manager) Restore(transfers []Transfer) {
	m.mu.Lock()
	changed := make([]Transfer, 0)
	for i := range transfers {
		t := transfers[i]
		if t.Status.Active() {
			now := time.Now()
			if t.Direction == DirectionIncoming && t.PartialPath != "" && t.TransferredBytes > 0 {
				t.Status, t.Resumable = StatusResumable, true
			} else {
				t.Status = StatusFailed
				t.Error = "Cascade closed before the transfer finished"
				t.CompletedAt = &now
			}
			t.UpdatedAt = now
			changed = append(changed, t)
		}
		m.items[t.ID] = &t
	}
	m.mu.Unlock()
	for _, transfer := range changed {
		m.changed(transfer, true)
	}
}

func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

func (m *Manager) UpdateSettings(settings Settings, cancelActive bool) error {
	if err := ValidateSettings(settings); err != nil {
		return err
	}
	if !settings.Enabled {
		m.mu.RLock()
		active := len(m.cancels) > 0 || m.outgoingActive != "" || len(m.outgoingQueue) > 0
		if !active {
			for _, transfer := range m.items {
				if transfer.Status.Active() {
					active = true
					break
				}
			}
		}
		m.mu.RUnlock()
		if active && !cancelActive {
			return fmt.Errorf("file transfers are still active")
		}
		if cancelActive {
			// Publish the feature gate before canceling so an outgoing cancellation
			// cannot advance the sequential queue and negotiate the next file.
			m.mu.Lock()
			m.settings = settings
			m.mu.Unlock()
			m.CancelAll()
			return nil
		}
	}
	m.mu.Lock()
	m.settings = settings
	m.mu.Unlock()
	return nil
}

func (m *Manager) Snapshot(activeOnly bool) []View {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]View, 0, len(m.items))
	for _, t := range m.items {
		if activeOnly && !t.Status.Active() && t.Status != StatusResumable && t.Status != StatusFailed {
			continue
		}
		v := t.View()
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func (m *Manager) Get(id string) (Transfer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.items[id]
	if !ok {
		return Transfer{}, false
	}
	return *t, true
}

// HandleControl consumes the arguments after the CTCP DCC verb.
func (m *Manager) HandleControl(networkID int64, networkName, peer, payload string) error {
	m.mu.RLock()
	enabled := m.settings.Enabled
	m.mu.RUnlock()
	if !enabled {
		return nil
	}
	offer, err := Parse(payload)
	if err != nil {
		return err
	}
	switch offer.Command {
	case CommandSend:
		if offer.Token != "" && offer.Port > 0 {
			if m.handlePassiveResponse(networkID, peer, offer) {
				return nil
			}
		}
		return m.addIncomingOffer(networkID, networkName, peer, offer)
	case CommandResume:
		return m.handleResumeRequest(networkID, peer, offer)
	case CommandAccept:
		return m.handleResumeAccept(networkID, peer, offer)
	case CommandChat:
		// Direct Chat is intentionally feature-gated for its follow-up phase.
		return nil
	default:
		return fmt.Errorf("unsupported DCC control message")
	}
}

func (m *Manager) addIncomingOffer(networkID int64, networkName, peer string, offer Offer) error {
	m.mu.Lock()
	for _, existing := range m.items {
		if existing.Direction == DirectionIncoming && existing.Status == StatusResumable &&
			existing.NetworkID == networkID && strings.EqualFold(existing.Peer, peer) &&
			existing.Filename == offer.Filename && existing.TotalBytes == offer.Size {
			existing.Address, existing.Port, existing.Token = offer.Address, offer.Port, offer.Token
			existing.Status, existing.Error, existing.UpdatedAt = StatusOffered, "", time.Now()
			m.offers[existing.ID] = offer
			t := *existing
			m.mu.Unlock()
			m.changed(t, true)
			return nil
		}
	}
	now := time.Now()
	id := newID()
	t := Transfer{
		ID: id, NetworkID: networkID, NetworkName: networkName, Peer: peer,
		Direction: DirectionIncoming, Filename: offer.Filename, TotalBytes: offer.Size,
		Status: StatusOffered, Address: offer.Address, Port: offer.Port, Token: offer.Token,
		CreatedAt: now, UpdatedAt: now,
	}
	m.items[id], m.offers[id] = &t, offer
	m.mu.Unlock()
	m.changed(t, true)
	return nil
}

func (m *Manager) QueueOutgoing(networkID int64, networkName, peer string, paths []string) ([]View, error) {
	m.mu.RLock()
	enabled := m.settings.Enabled
	m.mu.RUnlock()
	if !enabled {
		return nil, fmt.Errorf("file transfers are turned off")
	}
	if len(paths) == 0 {
		return []View{}, nil
	}
	type selectedFile struct {
		path string
		name string
		size int64
	}
	selected := make([]selectedFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("selected file is unavailable")
		}
		name, err := safeFilename(filepath.Base(path))
		if err != nil {
			return nil, fmt.Errorf("selected filename is not compatible with file transfers")
		}
		selected = append(selected, selectedFile{path: path, name: name, size: info.Size()})
	}
	created := make([]View, 0, len(paths))
	m.mu.Lock()
	for _, file := range selected {
		now, id := time.Now(), newID()
		t := &Transfer{
			ID: id, NetworkID: networkID, NetworkName: networkName, Peer: peer,
			Direction: DirectionOutgoing, Filename: file.name, LocalPath: file.path,
			TotalBytes: file.size, Status: StatusQueued, CreatedAt: now, UpdatedAt: now,
			FileAvailable: true,
		}
		m.items[id] = t
		m.outgoingQueue = append(m.outgoingQueue, id)
		created = append(created, t.View())
	}
	m.mu.Unlock()
	for _, v := range created {
		if t, ok := m.Get(v.ID); ok {
			m.changed(t, true)
		}
	}
	m.startNextOutgoing()
	return created, nil
}

func (m *Manager) startNextOutgoing() {
	m.mu.Lock()
	if m.outgoingActive != "" || len(m.outgoingQueue) == 0 || !m.settings.Enabled {
		m.mu.Unlock()
		return
	}
	id := m.outgoingQueue[0]
	m.outgoingQueue = m.outgoingQueue[1:]
	t := m.items[id]
	if t == nil || t.Status != StatusQueued {
		m.mu.Unlock()
		m.startNextOutgoing()
		return
	}
	m.outgoingActive = id
	settings := m.settings
	t.Status, t.UpdatedAt = StatusNegotiating, time.Now()
	copyT := *t
	m.mu.Unlock()
	m.changed(copyT, true)

	mode := settings.ConnectionMode
	if mode == ConnectionAutomatic {
		if settings.AdvertisedAddress == "" {
			mode = ConnectionPassive
		} else {
			mode = ConnectionClassic
		}
	}
	if mode == ConnectionPassive {
		m.beginPassiveOutgoing(id)
		return
	}
	m.beginClassicOutgoing(id)
}

func (m *Manager) beginClassicOutgoing(id string) {
	m.mu.RLock()
	t, settings := m.items[id], m.settings
	m.mu.RUnlock()
	if t == nil || t.Status != StatusNegotiating {
		return
	}
	if settings.AdvertisedAddress == "" {
		m.fail(id, errors.New("Set an advertised address or use Automatic compatibility mode"))
		return
	}
	listener, err := listen(settings)
	if err != nil {
		m.fail(id, err)
		return
	}
	m.mu.RLock()
	stillEnabled := m.settings.Enabled
	current := m.items[id]
	stillNegotiating := current != nil && current.Status == StatusNegotiating
	m.mu.RUnlock()
	if !stillEnabled || !stillNegotiating {
		_ = listener.Close()
		return
	}
	address, err := FormatAddress(settings.AdvertisedAddress)
	if err != nil {
		_ = listener.Close()
		m.fail(id, err)
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port
	m.mu.Lock()
	if current := m.items[id]; current != nil {
		current.Port, current.Address, current.Status, current.UpdatedAt = port, address, StatusNegotiating, time.Now()
	}
	m.mu.Unlock()
	if err := m.sendControl(t.NetworkID, t.Peer, FormatSend(t.Filename, address, port, t.TotalBytes, "")); err != nil {
		_ = listener.Close()
		m.fail(id, err)
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.setCancel(id, cancel)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer listener.Close()
		go func() { <-ctx.Done(); _ = listener.Close() }()
		_ = listener.SetDeadline(time.Now().Add(defaultNegotiationTimeout))
		conn, err := listener.AcceptTCP()
		if err != nil {
			m.finishWithError(id, ctx, err)
			return
		}
		defer conn.Close()
		m.runSend(ctx, id, conn)
	}()
}

func (m *Manager) beginPassiveOutgoing(id string) {
	m.mu.Lock()
	t := m.items[id]
	if t == nil || t.Status != StatusNegotiating {
		m.mu.Unlock()
		return
	}
	t.Token = passiveToken()
	t.Port, t.Address, t.Status, t.UpdatedAt = 0, "0", StatusNegotiating, time.Now()
	copyT := *t
	m.mu.Unlock()
	if err := m.sendControl(t.NetworkID, t.Peer, FormatSend(t.Filename, "0", 0, t.TotalBytes, t.Token)); err != nil {
		m.fail(id, err)
		return
	}
	m.changed(copyT, true)
	ctx, cancel := context.WithTimeout(m.ctx, defaultNegotiationTimeout)
	m.setCancel(id, cancel)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		<-ctx.Done()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			m.fail(id, fmt.Errorf("the other client did not accept the passive transfer"))
		}
	}()
}

func (m *Manager) handlePassiveResponse(networkID int64, peer string, offer Offer) bool {
	m.mu.Lock()
	if !m.settings.Enabled {
		m.mu.Unlock()
		return false
	}
	var id string
	for candidate, t := range m.items {
		if t.Direction == DirectionOutgoing && t.NetworkID == networkID && strings.EqualFold(t.Peer, peer) && t.Token == offer.Token && t.Filename == offer.Filename && t.Status == StatusNegotiating {
			id = candidate
			t.Address, t.Port = offer.Address, offer.Port
			break
		}
	}
	m.mu.Unlock()
	if id == "" {
		return false
	}
	m.cancelWorker(id)
	ctx, cancel := context.WithCancel(m.ctx)
	m.setCancel(id, cancel)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ip, err := ParseAddress(offer.Address)
		if err != nil {
			m.fail(id, err)
			return
		}
		conn, err := (&net.Dialer{Timeout: 20 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(offer.Port)))
		if err != nil {
			m.finishWithError(id, ctx, err)
			return
		}
		defer conn.Close()
		m.runSend(ctx, id, conn)
	}()
	return true
}

func (m *Manager) Accept(id, destination string) error {
	m.mu.Lock()
	if !m.settings.Enabled {
		m.mu.Unlock()
		return fmt.Errorf("file transfers are turned off")
	}
	t, ok := m.items[id]
	if !ok || t.Direction != DirectionIncoming || (t.Status != StatusOffered && t.Status != StatusResumable) {
		m.mu.Unlock()
		return fmt.Errorf("transfer is not available to accept")
	}
	offer := m.offers[id]
	if destination == "" {
		m.mu.Unlock()
		return fmt.Errorf("destination is required")
	}
	if t.PartialPath == "" {
		if _, err := os.Lstat(destination); err == nil {
			m.mu.Unlock()
			return fmt.Errorf("a file already exists at that location")
		} else if !errors.Is(err, os.ErrNotExist) {
			m.mu.Unlock()
			return fmt.Errorf("cannot use that destination")
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			m.mu.Unlock()
			return fmt.Errorf("create destination directory: %w", err)
		}
		t.LocalPath = destination
		t.PartialPath = destination + ".cascade-part-" + t.ID[:8]
	}
	offset := int64(0)
	if info, err := os.Stat(t.PartialPath); err == nil {
		if linkInfo, linkErr := os.Lstat(t.PartialPath); linkErr != nil || !linkInfo.Mode().IsRegular() || linkInfo.Mode()&os.ModeSymlink != 0 {
			m.mu.Unlock()
			return fmt.Errorf("partial file is not a regular file")
		}
		offset = info.Size()
		if offset > t.TotalBytes {
			m.mu.Unlock()
			return fmt.Errorf("partial file is larger than the offered file")
		}
	}
	available, err := availableDiskSpace(filepath.Dir(t.PartialPath))
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("could not verify available disk space")
	}
	if available < uint64(t.TotalBytes-offset) {
		m.mu.Unlock()
		return fmt.Errorf("not enough free space to receive this file")
	}
	t.ResumeOffset, t.TransferredBytes, t.Status, t.Error, t.UpdatedAt = offset, offset, StatusConnecting, "", time.Now()
	copyT := *t
	m.mu.Unlock()
	m.changed(copyT, true)

	if offset > 0 {
		if err := m.sendControl(t.NetworkID, t.Peer, FormatResume(CommandResume, t.Filename, offer.Port, offset, offer.Token)); err != nil {
			m.fail(id, err)
			return err
		}
		return nil
	}
	if err := m.beginReceive(id, offer); err != nil {
		m.fail(id, err)
		return err
	}
	return nil
}

func (m *Manager) beginReceive(id string, offer Offer) error {
	ctx, cancel := context.WithCancel(m.ctx)
	m.setCancel(id, cancel)
	if offer.Passive {
		return m.beginPassiveReceive(ctx, id, offer)
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ip, err := ParseAddress(offer.Address)
		if err != nil {
			m.fail(id, err)
			return
		}
		conn, err := (&net.Dialer{Timeout: 20 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(offer.Port)))
		if err != nil {
			m.finishWithError(id, ctx, err)
			return
		}
		defer conn.Close()
		m.runReceive(ctx, id, conn)
	}()
	return nil
}

func (m *Manager) beginPassiveReceive(ctx context.Context, id string, offer Offer) error {
	m.mu.RLock()
	settings := m.settings
	t := m.items[id]
	m.mu.RUnlock()
	if settings.AdvertisedAddress == "" {
		return fmt.Errorf("an advertised address is required to accept this passive transfer")
	}
	listener, err := listen(settings)
	if err != nil {
		return err
	}
	address, err := FormatAddress(settings.AdvertisedAddress)
	if err != nil {
		listener.Close()
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := m.sendControl(t.NetworkID, t.Peer, FormatSend(t.Filename, address, port, t.TotalBytes, offer.Token)); err != nil {
		listener.Close()
		return err
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer listener.Close()
		go func() { <-ctx.Done(); _ = listener.Close() }()
		_ = listener.SetDeadline(time.Now().Add(defaultNegotiationTimeout))
		conn, err := listener.AcceptTCP()
		if err != nil {
			m.finishWithError(id, ctx, err)
			return
		}
		defer conn.Close()
		m.runReceive(ctx, id, conn)
	}()
	return nil
}

func (m *Manager) handleResumeRequest(networkID int64, peer string, offer Offer) error {
	m.mu.Lock()
	var found *Transfer
	for _, t := range m.items {
		if t.Direction == DirectionOutgoing && t.NetworkID == networkID && strings.EqualFold(t.Peer, peer) && t.Filename == offer.Filename && (t.Port == offer.Port || (offer.Token != "" && t.Token == offer.Token)) {
			if offer.Position > t.TotalBytes {
				m.mu.Unlock()
				return fmt.Errorf("resume position exceeds file size")
			}
			t.ResumeOffset, t.TransferredBytes = offer.Position, offer.Position
			found = t
			break
		}
	}
	m.mu.Unlock()
	if found == nil {
		return fmt.Errorf("no matching outgoing transfer")
	}
	return m.sendControl(networkID, peer, FormatResume(CommandAccept, found.Filename, offer.Port, offer.Position, offer.Token))
}

func (m *Manager) handleResumeAccept(networkID int64, peer string, offer Offer) error {
	m.mu.RLock()
	var id string
	var saved Offer
	for candidate, t := range m.items {
		if t.Direction == DirectionIncoming && t.NetworkID == networkID && strings.EqualFold(t.Peer, peer) && t.Filename == offer.Filename && t.ResumeOffset == offer.Position && t.Status == StatusConnecting {
			id, saved = candidate, m.offers[candidate]
			break
		}
	}
	m.mu.RUnlock()
	if id == "" {
		return fmt.Errorf("no matching incoming transfer")
	}
	if err := m.beginReceive(id, saved); err != nil {
		m.fail(id, err)
		return err
	}
	return nil
}

func (m *Manager) runSend(ctx context.Context, id string, conn net.Conn) {
	t, ok := m.Get(id)
	if !ok {
		return
	}
	if !m.markTransferring(id) {
		return
	}
	stopWatching := watchConnectionCancellation(ctx, conn)
	defer stopWatching()
	err := sendFile(ctx, conn, t.LocalPath, t.ResumeOffset, t.TotalBytes, func(n int64) { m.progress(id, n) })
	if err != nil {
		m.finishWithError(id, ctx, err)
		return
	}
	m.complete(id)
}

func (m *Manager) runReceive(ctx context.Context, id string, conn net.Conn) {
	t, ok := m.Get(id)
	if !ok {
		return
	}
	if !m.markTransferring(id) {
		return
	}
	stopWatching := watchConnectionCancellation(ctx, conn)
	defer stopWatching()
	err := receiveFile(ctx, conn, t.PartialPath, t.ResumeOffset, t.TotalBytes, func(n int64) { m.progress(id, n) })
	if err != nil {
		m.finishWithError(id, ctx, err)
		return
	}
	if err := finalizePartial(t.PartialPath, t.LocalPath); err != nil {
		m.fail(id, err)
		return
	}
	m.complete(id)
}

func watchConnectionCancellation(ctx context.Context, conn net.Conn) func() {
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.SetDeadline(time.Now())
		case <-stopped:
		}
	}()
	return func() { close(stopped) }
}

func (m *Manager) markTransferring(id string) bool {
	m.mu.Lock()
	if t := m.items[id]; t != nil && (t.Status == StatusNegotiating || t.Status == StatusConnecting) {
		t.Status, t.Error, t.UpdatedAt = StatusTransferring, "", time.Now()
		copyT := *t
		m.mu.Unlock()
		m.changed(copyT, true)
		return true
	}
	m.mu.Unlock()
	return false
}

func (m *Manager) progress(id string, transferred int64) {
	m.mu.Lock()
	t := m.items[id]
	if t == nil || t.Status != StatusTransferring {
		m.mu.Unlock()
		return
	}
	now := time.Now()
	deltaBytes := transferred - t.TransferredBytes
	deltaTime := now.Sub(t.UpdatedAt)
	if deltaTime > 0 && deltaBytes >= 0 {
		t.SpeedBPS = int64(float64(deltaBytes) / deltaTime.Seconds())
		if t.SpeedBPS > 0 && t.TotalBytes >= transferred {
			t.ETASeconds = (t.TotalBytes - transferred) / t.SpeedBPS
		}
	}
	t.TransferredBytes, t.UpdatedAt = transferred, now
	copyT := *t
	force := now.Sub(m.lastEmit[id]) >= progressInterval
	checkpoint := now.Sub(m.lastPersist[id]) >= time.Second
	if force {
		m.lastEmit[id] = now
	}
	if checkpoint {
		m.lastPersist[id] = now
	}
	m.mu.Unlock()
	if checkpoint && m.persist != nil {
		_ = m.persist(copyT)
	}
	if force && m.emit != nil {
		view := copyT.View()
		m.emit(Event{Type: "upsert", Transfer: &view})
	}
}

func (m *Manager) complete(id string) {
	now := time.Now()
	m.mu.Lock()
	t := m.items[id]
	if t == nil || t.Status != StatusTransferring {
		m.mu.Unlock()
		return
	}
	t.Status, t.Error, t.UpdatedAt, t.CompletedAt = StatusCompleted, "", now, &now
	t.TransferredBytes, t.SpeedBPS, t.ETASeconds, t.Resumable = t.TotalBytes, 0, 0, false
	if t.Direction == DirectionIncoming {
		t.FileAvailable = true
	}
	copyT := *t
	cancel := m.cancels[id]
	m.clearWorkerLocked(id)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.changed(copyT, true)
	m.advanceOutgoing(id)
}

func (m *Manager) fail(id string, err error) { m.finishWithError(id, context.Background(), err) }

func (m *Manager) finishWithError(id string, ctx context.Context, err error) {
	m.mu.Lock()
	t := m.items[id]
	if t == nil {
		m.mu.Unlock()
		return
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		t.Status = StatusCanceled
		t.Error = ""
	} else {
		t.Status = StatusFailed
		t.Error = userError(err)
	}
	if t.Direction == DirectionIncoming && t.PartialPath != "" && t.TransferredBytes > 0 {
		if _, statErr := os.Stat(t.PartialPath); statErr == nil {
			t.Status, t.Resumable = StatusResumable, true
		}
	}
	now := time.Now()
	t.UpdatedAt, t.SpeedBPS, t.ETASeconds = now, 0, 0
	if t.Status != StatusResumable {
		t.CompletedAt = &now
	}
	cleanupPartial := ""
	if t.Direction == DirectionIncoming && !t.Resumable {
		cleanupPartial = t.PartialPath
	}
	copyT := *t
	cancel := m.cancels[id]
	m.clearWorkerLocked(id)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cleanupPartial != "" {
		_ = os.Remove(cleanupPartial)
	}
	m.changed(copyT, true)
	m.advanceOutgoing(id)
}

func (m *Manager) Decline(id string) error {
	m.mu.Lock()
	t := m.items[id]
	if t == nil || t.Status != StatusOffered {
		m.mu.Unlock()
		return fmt.Errorf("transfer offer was not found")
	}
	now := time.Now()
	t.Status, t.UpdatedAt, t.CompletedAt = StatusDeclined, now, &now
	copyT := *t
	m.mu.Unlock()
	m.changed(copyT, true)
	return nil
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	t := m.items[id]
	if t == nil || (!t.Status.Active() && t.Status != StatusResumable) {
		m.mu.Unlock()
		return fmt.Errorf("active transfer was not found")
	}
	for i, queued := range m.outgoingQueue {
		if queued == id {
			m.outgoingQueue = append(m.outgoingQueue[:i], m.outgoingQueue[i+1:]...)
			break
		}
	}
	cancel := m.cancels[id]
	now := time.Now()
	t.Status, t.UpdatedAt = StatusCanceled, now
	if t.Direction == DirectionIncoming && t.PartialPath != "" && t.TransferredBytes > 0 {
		t.Status, t.Resumable = StatusResumable, true
	} else {
		t.CompletedAt = &now
	}
	copyT := *t
	cleanupPartial := ""
	if t.Direction == DirectionIncoming && !t.Resumable {
		cleanupPartial = t.PartialPath
	}
	m.clearWorkerLocked(id)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cleanupPartial != "" {
		_ = os.Remove(cleanupPartial)
	}
	m.changed(copyT, true)
	m.advanceOutgoing(id)
	return nil
}

func (m *Manager) CancelAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.items))
	for id, t := range m.items {
		if t.Status.Active() {
			ids = append(ids, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range ids {
		_ = m.Cancel(id)
	}
}

func (m *Manager) Retry(id string) error {
	m.mu.Lock()
	if !m.settings.Enabled {
		m.mu.Unlock()
		return fmt.Errorf("file transfers are turned off")
	}
	t := m.items[id]
	if t == nil || t.Direction != DirectionOutgoing || (t.Status != StatusFailed && t.Status != StatusCanceled) {
		m.mu.Unlock()
		return fmt.Errorf("transfer cannot be retried")
	}
	info, err := os.Lstat(t.LocalPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		m.mu.Unlock()
		return fmt.Errorf("source file is unavailable")
	}
	t.Status, t.Error, t.TransferredBytes, t.ResumeOffset, t.UpdatedAt = StatusQueued, "", 0, 0, time.Now()
	t.CompletedAt, t.Resumable = nil, false
	m.outgoingQueue = append(m.outgoingQueue, id)
	copyT := *t
	m.mu.Unlock()
	m.changed(copyT, true)
	m.startNextOutgoing()
	return nil
}

func (m *Manager) DiscardPartial(id string) error {
	m.mu.Lock()
	t := m.items[id]
	if t == nil || !t.Resumable || t.PartialPath == "" {
		m.mu.Unlock()
		return fmt.Errorf("partial transfer was not found")
	}
	partial := t.PartialPath
	delete(m.items, id)
	delete(m.offers, id)
	m.mu.Unlock()
	if err := os.Remove(partial); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if m.remove != nil {
		_ = m.remove(id)
	}
	if m.emit != nil {
		m.emit(Event{Type: "remove", ID: id})
	}
	return nil
}

func (m *Manager) RemoveHistory(id string) error {
	m.mu.Lock()
	t := m.items[id]
	if t == nil || t.Status.Active() || t.Resumable {
		m.mu.Unlock()
		return fmt.Errorf("active or resumable transfers cannot be removed from history")
	}
	delete(m.items, id)
	m.mu.Unlock()
	if m.remove != nil {
		if err := m.remove(id); err != nil {
			return err
		}
	}
	if m.emit != nil {
		m.emit(Event{Type: "remove", ID: id})
	}
	return nil
}

func (m *Manager) ClearHistory() {
	m.mu.Lock()
	removed := make([]string, 0)
	for id, t := range m.items {
		if !t.Status.Active() && !t.Resumable {
			delete(m.items, id)
			removed = append(removed, id)
		}
	}
	m.mu.Unlock()
	if m.emit != nil {
		for _, id := range removed {
			m.emit(Event{Type: "remove", ID: id})
		}
	}
}

func (m *Manager) Locate(id, path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("selected file is unavailable")
	}
	m.mu.Lock()
	t := m.items[id]
	if t == nil || t.Status.Active() || t.Resumable {
		m.mu.Unlock()
		return fmt.Errorf("transfer history entry was not found")
	}
	if t.TotalBytes >= 0 && info.Size() != t.TotalBytes {
		m.mu.Unlock()
		return fmt.Errorf("selected file does not match the transferred size")
	}
	t.LocalPath, t.FileAvailable, t.UpdatedAt = path, true, time.Now()
	copyT := *t
	m.mu.Unlock()
	m.changed(copyT, true)
	return nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.settings.Enabled = false
	m.mu.Unlock()
	m.cancel()
	m.CancelAll()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

func (m *Manager) setCancel(id string, cancel context.CancelFunc) {
	m.mu.Lock()
	if old := m.cancels[id]; old != nil {
		old()
	}
	m.cancels[id] = cancel
	m.mu.Unlock()
}

func (m *Manager) cancelWorker(id string) {
	m.mu.Lock()
	cancel := m.cancels[id]
	delete(m.cancels, id)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) clearWorkerLocked(id string) {
	delete(m.cancels, id)
	delete(m.lastEmit, id)
	delete(m.lastPersist, id)
}

func (m *Manager) advanceOutgoing(id string) {
	m.mu.Lock()
	if m.outgoingActive == id {
		m.outgoingActive = ""
	}
	m.mu.Unlock()
	m.startNextOutgoing()
}

func (m *Manager) changed(t Transfer, force bool) {
	if m.persist != nil && force {
		_ = m.persist(t)
	}
	if m.emit != nil {
		v := t.View()
		m.emit(Event{Type: "upsert", Transfer: &v})
	}
}

func listen(settings Settings) (*net.TCPListener, error) {
	if settings.PortMin == 0 {
		return net.ListenTCP("tcp", &net.TCPAddr{Port: 0})
	}
	for port := settings.PortMin; port <= settings.PortMax; port++ {
		listener, err := net.ListenTCP("tcp", &net.TCPAddr{Port: port})
		if err == nil {
			return listener, nil
		}
	}
	return nil, fmt.Errorf("no available port in the configured range")
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

func passiveToken() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano()&0x7fffffff, 10)
	}
	n := uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
	return strconv.FormatUint(n, 10)
}

func userError(err error) string {
	if err == nil {
		return "The transfer failed"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "The connection timed out"
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return "A local file could not be read or written"
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return "The received file could not be saved"
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return "The direct connection failed"
	}
	return err.Error()
}
