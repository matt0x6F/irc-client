package plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/matt0x6f/irc-client/internal/logger"
)

const (
	// Logs are written through a process-wide synchronous writer. Keep previews
	// small so one large plugin frame cannot monopolize that writer while the
	// IRC read callback is draining a server burst.
	maxPluginLogPreview        = 512
	maxPluginNotificationBytes = 48 * 1024
)

func pluginLogPreview(line string) string {
	if len(line) <= maxPluginLogPreview {
		return line
	}
	return line[:maxPluginLogPreview] + "…"
}

// IPC handles communication with a plugin process
type IPC struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stdoutReader *bufio.Reader
	mu           sync.Mutex
	requests     map[interface{}]chan *Response
	nextID       int64
	closed       bool
	pluginID     string
	manager      *Manager // Reference to manager for handling notifications

	// writeCh serializes all writes to the plugin's stdin through a single
	// writer goroutine (writeLoop). This is the critical decoupling that keeps a
	// slow or wedged plugin from blocking its callers: SendNotification is called
	// synchronously on the shared event-bus dispatcher goroutine, and a direct
	// stdin.Write there would park on a full pipe if the plugin stopped draining
	// — freezing the whole event bus (and, transitively, IRC sends, connection
	// teardown, and reconnect). Enqueueing instead means the dispatcher never
	// touches the pipe. Buffered so brief bursts don't drop; a sustained backlog
	// (a genuinely stuck plugin) drops best-effort notifications rather than
	// blocking. done closes on Close to stop the writer.
	writeCh   chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

// NewIPC creates a new IPC connection to a plugin
func NewIPC(pluginPath string, pluginID string, manager *Manager) (*IPC, error) {
	logger.Log.Info().
		Str("plugin", pluginID).
		Str("path", pluginPath).
		Msg("Creating IPC connection to plugin")
	cmd := exec.Command(pluginPath)
	// Create a minimal, clean environment for plugins
	// Only include essential variables to avoid interference from shell initialization,
	// IDE-specific variables, and other application-specific environment variables
	// This prevents plugins from trying to initialize shells, terminals, or other
	// unnecessary subsystems that could cause deadlocks or blocking behavior
	env := []string{
		"PATH=" + os.Getenv("PATH"), // Needed to find shared libraries
		"HOME=" + os.Getenv("HOME"), // Needed for user directory access (if needed)
		"USER=" + os.Getenv("USER"), // Basic user info
		"TERM=dumb",                 // Prevent terminal initialization attempts
	}
	// Add locale settings if available, otherwise use safe defaults
	if lang := os.Getenv("LANG"); lang != "" {
		env = append(env, "LANG="+lang)
	} else {
		env = append(env, "LANG=en_US.UTF-8")
	}
	// Add LC_* variables if they exist (for proper locale handling)
	if lcAll := os.Getenv("LC_ALL"); lcAll != "" {
		env = append(env, "LC_ALL="+lcAll)
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	ipc := &IPC{
		cmd:          cmd,
		stdin:        stdin,
		stdout:       stdout,
		stdoutReader: bufio.NewReader(stdout),
		requests:     make(map[interface{}]chan *Response),
		nextID:       1,
		pluginID:     pluginID,
		manager:      manager,
		writeCh:      make(chan []byte, 256),
		done:         make(chan struct{}),
	}

	// CRITICAL: Start reading from stdout and stderr BEFORE starting the process
	// If pipes aren't being read when the process starts, the child can block
	// when pipe buffers fill, causing deadlocks and zombie processes

	// Start reading stderr
	go func() {
		reader := bufio.NewReader(stderr)
		logger.Log.Info().
			Str("plugin", pluginID).
			Msg("Starting stderr reader (before process start)")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					logger.Log.Info().
						Str("plugin", pluginID).
						Msg("Plugin stderr closed (EOF)")
				} else {
					logger.Log.Warn().
						Err(err).
						Str("plugin", pluginID).
						Msg("Error reading plugin stderr")
				}
				break
			}
			// Remove trailing newline
			line = strings.TrimSuffix(line, "\n")
			logger.Log.Info().
				Str("plugin", pluginID).
				Str("stderr", pluginLogPreview(line)).
				Int("line_length", len(line)).
				Msg("Plugin stderr")
		}
	}()

	// Start reading stdout BEFORE starting the process
	// This is critical to prevent the child from blocking
	go ipc.readLoop()

	// Start the sole stdin writer. All writes funnel through writeCh so a plugin
	// that stops draining its stdin can only ever block THIS goroutine (which
	// Close unblocks by closing the pipe), never a caller on a shared goroutine.
	go ipc.writeLoop()

	// Start the process
	logger.Log.Info().
		Str("plugin", pluginID).
		Str("path", pluginPath).
		Msg("Starting plugin process")
	if err := cmd.Start(); err != nil {
		stdin.Close()
		logger.Log.Error().
			Err(err).
			Str("plugin", pluginID).
			Str("path", pluginPath).
			Msg("Failed to start plugin process")
		return nil, fmt.Errorf("failed to start plugin: %w", err)
	}
	logger.Log.Info().
		Str("plugin", pluginID).
		Int("pid", cmd.Process.Pid).
		Msg("Plugin process started successfully")

	// Start a goroutine to always wait for the process to prevent zombies
	// This must run regardless of how the process exits
	go func() {
		err := cmd.Wait()
		if err != nil {
			logger.Log.Debug().
				Err(err).
				Str("plugin", pluginID).
				Int("pid", cmd.Process.Pid).
				Msg("Plugin process exited")
		} else {
			logger.Log.Debug().
				Str("plugin", pluginID).
				Int("pid", cmd.Process.Pid).
				Msg("Plugin process exited successfully")
		}
	}()

	// Give the plugin a moment to initialize and set up its stdin/stdout handlers
	// This helps avoid race conditions where we send data before the plugin is ready
	time.Sleep(50 * time.Millisecond)

	return ipc, nil
}

// normalizeID converts an interface{} ID to int64 for consistent map lookups
// JSON numbers are unmarshaled as float64, but we store int64 in the map
func normalizeID(id interface{}) (int64, bool) {
	switch v := id.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		// JSON numbers are unmarshaled as float64
		return int64(v), true
	case int32:
		return int64(v), true
	case int16:
		return int64(v), true
	case int8:
		return int64(v), true
	default:
		return 0, false
	}
}

// readLoop reads responses and notifications from the plugin
func (ipc *IPC) readLoop() {
	logger.Log.Info().
		Str("plugin", ipc.pluginID).
		Msg("IPC read loop started, waiting for plugin stdout")

	for {
		line, err := ipc.stdoutReader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				logger.Log.Info().
					Str("plugin", ipc.pluginID).
					Msg("Plugin stdout closed (EOF)")
			} else {
				logger.Log.Error().
					Err(err).
					Str("plugin", ipc.pluginID).
					Msg("Error reading from plugin stdout")
			}
			break
		}

		// Remove trailing newline
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}

		logger.Log.Info().
			Str("plugin", ipc.pluginID).
			Str("line", pluginLogPreview(line)).
			Int("line_length", len(line)).
			Msg("Received line from plugin stdout")

		// Try to parse as Response first (responses have "result" or "error" field)
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err == nil {
			// Check if this looks like a response (has result or error field)
			if resp.Result != nil || resp.Error != nil {
				// This is a response - normalize ID for map lookup
				normalizedID, valid := normalizeID(resp.ID)
				if !valid {
					logger.Log.Warn().
						Str("plugin", ipc.pluginID).
						Interface("id", resp.ID).
						Msg("Received response with invalid ID type")
					continue
				}

				ipc.mu.Lock()
				ch, ok := ipc.requests[normalizedID]
				if ok {
					delete(ipc.requests, normalizedID)
				}
				ipc.mu.Unlock()

				if ok {
					ch <- &resp
				} else {
					logger.Log.Warn().
						Str("plugin", ipc.pluginID).
						Interface("id", resp.ID).
						Int64("normalized_id", normalizedID).
						Msg("Received response with unknown ID")
				}
				continue
			}
		}

		// Try to parse as Request (notifications have method but no ID, or method with ID)
		var msg Request
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			logger.Log.Error().
				Err(err).
				Str("plugin", ipc.pluginID).
				Str("line", line).
				Msg("Error parsing plugin message (not a valid Request or Response)")
			continue
		}

		// Check if this is a notification (no ID) or a response (has ID)
		if msg.ID == nil {
			// This is a notification - handle it
			ipc.handleNotification(&msg)
		} else {
			// This might be a response that was parsed as Request - try Response again
			var resp2 Response
			if err := json.Unmarshal([]byte(line), &resp2); err == nil {
				// Normalize ID for map lookup
				normalizedID, valid := normalizeID(resp2.ID)
				if !valid {
					logger.Log.Warn().
						Str("plugin", ipc.pluginID).
						Interface("id", resp2.ID).
						Str("method", msg.Method).
						Msg("Received response with invalid ID type")
					continue
				}

				ipc.mu.Lock()
				ch, ok := ipc.requests[normalizedID]
				if ok {
					delete(ipc.requests, normalizedID)
				}
				ipc.mu.Unlock()

				if ok {
					ch <- &resp2
				} else {
					logger.Log.Warn().
						Str("plugin", ipc.pluginID).
						Interface("id", resp2.ID).
						Int64("normalized_id", normalizedID).
						Msg("Received response with unknown ID")
				}
			} else {
				logger.Log.Warn().
					Str("plugin", ipc.pluginID).
					Interface("id", msg.ID).
					Str("method", msg.Method).
					Msg("Received request with ID but couldn't parse as response")
			}
		}
	}

	// Cleanup on close
	ipc.mu.Lock()
	ipc.closed = true
	for _, ch := range ipc.requests {
		close(ch)
	}
	ipc.requests = make(map[interface{}]chan *Response)
	ipc.mu.Unlock()
}

// SendRequest sends a JSON-RPC request to the plugin
// writeLoop is the sole writer of the plugin's stdin. It drains writeCh in FIFO
// order (so JSON-RPC lines are never interleaved) and blocks here — and only
// here — when a plugin stops reading its stdin. On a write error or Close (done
// closed, which also closes stdin to unblock an in-progress Write) it exits.
func (ipc *IPC) writeLoop() {
	for {
		select {
		case data := <-ipc.writeCh:
			if _, err := ipc.stdin.Write(data); err != nil {
				logger.Log.Warn().Err(err).Str("plugin", ipc.pluginID).Msg("Plugin stdin write failed; stopping writer")
				return
			}
		case <-ipc.done:
			return
		}
	}
}

// enqueueWrite hands a pre-marshaled line to the writer goroutine. block=false
// (notifications) makes it best-effort: if the buffer is full the line is
// dropped rather than parking the caller — vital on the event-bus dispatcher.
// block=true (requests) waits up to a bounded time so a wedged plugin can't
// stall the caller indefinitely.
func (ipc *IPC) enqueueWrite(data []byte, block bool) error {
	if !block {
		select {
		case ipc.writeCh <- data:
			return nil
		case <-ipc.done:
			return fmt.Errorf("IPC closed")
		default:
			logger.Log.Warn().Str("plugin", ipc.pluginID).Msg("Plugin write buffer full; dropping notification (plugin not draining stdin)")
			return nil
		}
	}
	select {
	case ipc.writeCh <- data:
		return nil
	case <-ipc.done:
		return fmt.Errorf("IPC closed")
	case <-time.After(2 * time.Second):
		return fmt.Errorf("timeout enqueuing write (plugin not draining stdin)")
	}
}

func (ipc *IPC) SendRequest(method string, params interface{}) (*Response, error) {
	ipc.mu.Lock()
	if ipc.closed {
		ipc.mu.Unlock()
		return nil, fmt.Errorf("IPC closed")
	}

	id := ipc.nextID
	ipc.nextID++
	ch := make(chan *Response, 1)
	ipc.requests[id] = ch
	ipc.mu.Unlock()

	logger.Log.Info().
		Str("plugin", ipc.pluginID).
		Str("method", method).
		Interface("id", id).
		Msg("Preparing to send request to plugin")

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		ipc.mu.Lock()
		delete(ipc.requests, id)
		ipc.mu.Unlock()
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	data = append(data, '\n')

	logger.Log.Info().
		Str("plugin", ipc.pluginID).
		Str("method", method).
		Interface("id", id).
		Str("data", string(data)).
		Msg("Writing request to plugin stdin")

	// Hand off to the writer goroutine (bounded wait) instead of writing stdin
	// inline, so a plugin that has stopped draining its stdin can't park this
	// caller forever — it fails fast and the request is cleaned up.
	if err := ipc.enqueueWrite(data, true); err != nil {
		ipc.mu.Lock()
		delete(ipc.requests, id)
		ipc.mu.Unlock()
		logger.Log.Error().
			Err(err).
			Str("plugin", ipc.pluginID).
			Str("method", method).
			Msg("Failed to enqueue request to plugin")
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	logger.Log.Info().
		Str("plugin", ipc.pluginID).
		Str("method", method).
		Int("data_length", len(data)).
		Msg("Enqueued request to plugin, waiting for response")

	// Wait for response with timeout
	logger.Log.Debug().
		Str("plugin", ipc.pluginID).
		Str("method", method).
		Interface("id", id).
		Msg("Waiting for response from plugin")

	// Use a shorter timeout for initialization to fail fast
	timeout := 10 * time.Second
	if method == "initialize" {
		timeout = 5 * time.Second
	}

	select {
	case resp := <-ch:
		if resp == nil {
			logger.Log.Error().
				Str("plugin", ipc.pluginID).
				Str("method", method).
				Msg("Response channel closed")
			return nil, fmt.Errorf("plugin closed connection")
		}
		logger.Log.Debug().
			Str("plugin", ipc.pluginID).
			Str("method", method).
			Interface("response", resp).
			Msg("Received response from plugin")
		if resp.Error != nil {
			return nil, fmt.Errorf("plugin error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
		}
		return resp, nil
	case <-time.After(timeout):
		ipc.mu.Lock()
		delete(ipc.requests, id)
		ipc.mu.Unlock()
		logger.Log.Error().
			Str("plugin", ipc.pluginID).
			Str("method", method).
			Dur("timeout", timeout).
			Msg("Timeout waiting for plugin response")
		// Don't close IPC here - let the caller decide what to do
		return nil, fmt.Errorf("timeout waiting for plugin response")
	}
}

// SendNotification sends a JSON-RPC notification (no response expected)
func (ipc *IPC) SendNotification(method string, params interface{}) error {
	ipc.mu.Lock()
	if ipc.closed {
		ipc.mu.Unlock()
		return fmt.Errorf("IPC closed")
	}
	ipc.mu.Unlock()

	req := Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	data = append(data, '\n')
	if len(data) > maxPluginNotificationBytes {
		return fmt.Errorf("notification exceeds plugin IPC frame limit: %d > %d bytes", len(data), maxPluginNotificationBytes)
	}

	// Best-effort, non-blocking hand-off to the writer goroutine. Notifications
	// are called synchronously on the shared event-bus dispatcher; a direct pipe
	// write here would freeze the entire bus if the plugin stopped draining its
	// stdin (the observed deadlock). Dropping a notification a stuck plugin was
	// never going to process is strictly preferable to that.
	return ipc.enqueueWrite(data, false)
}

// handleNotification handles JSON-RPC notifications from plugins
func (ipc *IPC) handleNotification(req *Request) {
	if ipc.manager == nil {
		return
	}

	// Handle ui_metadata.set notifications
	if req.Method == "ui_metadata.set" {
		params, ok := req.Params.(map[string]interface{})
		if !ok {
			// Try to unmarshal if it's raw JSON
			if paramsBytes, err := json.Marshal(req.Params); err == nil {
				var parsedParams map[string]interface{}
				if err := json.Unmarshal(paramsBytes, &parsedParams); err == nil {
					params = parsedParams
					ok = true
				}
			}
		}
		if ok {
			logger.Log.Info().
				Str("plugin", ipc.pluginID).
				Interface("params", params).
				Msg("Received ui_metadata.set notification")
			if err := ipc.manager.HandleMetadataRequest(ipc.pluginID, params); err != nil {
				logger.Log.Error().Err(err).Str("plugin", ipc.pluginID).Msg("Error handling metadata request")
			}
		} else {
			logger.Log.Warn().
				Str("plugin", ipc.pluginID).
				Interface("params", req.Params).
				Msg("Failed to parse ui_metadata.set params")
		}
	}

	// Handle batched UI metadata snapshots. A single notification and event-bus
	// entry keeps large nickname rosters from flooding connection startup.
	if req.Method == "ui_metadata.set_batch" {
		params, ok := req.Params.(map[string]interface{})
		if ok {
			if err := ipc.manager.HandleMetadataBatchRequest(ipc.pluginID, params); err != nil {
				logger.Log.Error().Err(err).Str("plugin", ipc.pluginID).Msg("Error handling metadata batch request")
			}
		} else {
			logger.Log.Warn().Str("plugin", ipc.pluginID).Msg("Failed to parse ui_metadata.set_batch params")
		}
	}

	// Handle generic action notifications (e.g. plugins sending a message).
	if req.Method == "action" {
		params, ok := req.Params.(map[string]interface{})
		if !ok {
			if b, err := json.Marshal(req.Params); err == nil {
				var p map[string]interface{}
				if json.Unmarshal(b, &p) == nil {
					params, ok = p, true
				}
			}
		}
		if ok {
			atype, _ := params["type"].(string)
			data, _ := params["data"].(map[string]interface{})
			ipc.manager.EnqueueAction(Action{PluginID: ipc.pluginID, Type: atype, Data: data})
		} else {
			logger.Log.Warn().
				Str("plugin", ipc.pluginID).
				Interface("params", req.Params).
				Msg("Failed to parse action params")
		}
		return
	}
}

// Close closes the IPC connection
func (ipc *IPC) Close() error {
	ipc.mu.Lock()
	if ipc.closed {
		ipc.mu.Unlock()
		return nil
	}
	ipc.closed = true
	ipc.mu.Unlock()

	// Stop the writer goroutine. Closing done makes it return from its select;
	// closing stdin below additionally unblocks it if it is parked mid-Write on a
	// plugin that stopped reading.
	ipc.closeOnce.Do(func() { close(ipc.done) })

	// Close stdin first to signal the plugin to exit (should trigger EOF)
	// This gives the plugin a chance to exit gracefully
	if ipc.stdin != nil {
		ipc.stdin.Close()
	}

	// Try to kill the process if it's still running
	// Note: We already have a goroutine running cmd.Wait() from NewIPC()
	// so the process will be reaped even if we can't kill it here
	if ipc.cmd != nil && ipc.cmd.Process != nil {
		// Give process a moment to exit naturally after stdin close
		time.Sleep(50 * time.Millisecond)

		// Try graceful termination first
		ipc.cmd.Process.Signal(os.Interrupt)
		time.Sleep(100 * time.Millisecond)

		// Force kill if still running
		ipc.cmd.Process.Kill()
		// The Wait() goroutine started in NewIPC() will reap the process
	}
	return nil
}
