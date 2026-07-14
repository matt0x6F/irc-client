package main

import (
	"bufio"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// paneOpenIRCServer is just enough of an IRC server to register a client while
// recording every command the client puts on the wire.
type paneOpenIRCServer struct {
	listener net.Listener
	mu       sync.Mutex
	lines    []string
}

type paneOpenEventSignal chan struct{}

func (s paneOpenEventSignal) OnEvent(events.Event) {
	select {
	case s <- struct{}{}:
	default:
	}
}

func newPaneOpenIRCServer(t *testing.T) *paneOpenIRCServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &paneOpenIRCServer{listener: listener}
	t.Cleanup(func() { _ = listener.Close() })
	go server.serve()
	return server
}

func (s *paneOpenIRCServer) serve() {
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.lines = append(s.lines, line)
		s.mu.Unlock()

		switch {
		case strings.HasPrefix(strings.ToUpper(line), "CAP LS"):
			write(":mock CAP * LS :multi-prefix")
		case strings.HasPrefix(strings.ToUpper(line), "CAP REQ"):
			caps := line[strings.Index(line, ":")+1:]
			write(":mock CAP * ACK :" + caps)
		case strings.HasPrefix(strings.ToUpper(line), "CAP END"):
			write(":mock 001 tester :Welcome")
			write(":mock 376 tester :End of /MOTD command")
		case strings.HasPrefix(strings.ToUpper(line), "QUIT"):
			return
		}
	}
}

func (s *paneOpenIRCServer) hasLine(want string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range s.lines {
		if line == want {
			return true
		}
	}
	return false
}

// Selecting a joined channel is a local UI operation. JOIN already caused the
// server to send NAMES, and JOIN/PART/QUIT/NICK keep that roster current. Sending
// another NAMES here can make a large channel overflow the server's SendQ while
// registration and auto-join responses are still arriving.
func TestSetPaneFocusDoesNotRequestNames(t *testing.T) {
	server := newPaneOpenIRCServer(t)
	host, portText, err := net.SplitHostPort(server.listener.Addr().String())
	if err != nil {
		t.Fatalf("split server address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}

	store, err := storage.NewStorage(filepath.Join(t.TempDir(), "test.db"), 100, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	network := &storage.Network{
		Name: "Test", Address: host, Port: port, Nickname: "tester",
		Username: "tester", Realname: "Tester", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.CreateNetwork(network); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	channel := &storage.Channel{NetworkID: network.ID, Name: "#busy", IsOpen: false, CreatedAt: time.Now()}
	if err := store.CreateChannel(channel); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	storedChannel, err := store.GetChannelByName(network.ID, channel.Name)
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	established := make(paneOpenEventSignal, 1)
	bus.Subscribe(irc.EventConnectionEstablished, established)
	client := irc.NewIRCClient(network, bus, store)
	client.SetNetworkID(network.ID)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect() })
	select {
	case <-established:
	case <-time.After(time.Second):
		t.Fatal("connection-established event timed out")
	}
	// onConnect clears stale rosters, so record the self-membership after the
	// connection has completed, as the subsequent JOIN handler would.
	if err := store.AddChannelUser(storedChannel.ID, network.Nickname, ""); err != nil {
		t.Fatalf("AddChannelUser: %v", err)
	}

	app := &App{
		storage:    store,
		eventBus:   bus,
		ircClients: map[int64]*irc.IRCClient{network.ID: client},
	}
	joined, err := store.GetJoinedChannels(network.ID, network.Nickname)
	if err != nil {
		t.Fatalf("GetJoinedChannels: %v", err)
	}
	if len(joined) != 1 {
		t.Fatalf("joined channel fixture has %d channels, want 1", len(joined))
	}
	dbNetwork, err := store.GetNetwork(network.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if dbNetwork.Nickname != network.Nickname {
		t.Fatalf("stored nickname = %q, want %q", dbNetwork.Nickname, network.Nickname)
	}
	if err := app.SetPaneFocus(network.ID, "channel", channel.Name); err != nil {
		t.Fatalf("SetPaneFocus: %v", err)
	}
	if !client.IsConnected() {
		t.Fatal("client disconnected during SetPaneFocus")
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if server.hasLine("NAMES " + channel.Name) {
			t.Fatalf("selecting %s sent a redundant NAMES request", channel.Name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
