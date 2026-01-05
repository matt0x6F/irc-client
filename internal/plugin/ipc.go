package plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/matt0x6f/irc-client/internal/logger"
)

// IPC handles communication with a plugin process
type IPC struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Scanner
	mu       sync.Mutex
	requests map[interface{}]chan *Response
	nextID   int64
	closed   bool
}

// NewIPC creates a new IPC connection to a plugin
func NewIPC(pluginPath string) (*IPC, error) {
	cmd := exec.Command(pluginPath)
	
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	ipc := &IPC{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewScanner(stdout),
		requests: make(map[interface{}]chan *Response),
		nextID:   1,
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to start plugin: %w", err)
	}

	// Start reading responses
	go ipc.readLoop()

	return ipc, nil
}

// readLoop reads responses from the plugin
func (ipc *IPC) readLoop() {
	for ipc.stdout.Scan() {
		line := ipc.stdout.Text()
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			logger.Log.Error().Err(err).Msg("Error parsing plugin response")
			continue
		}

		ipc.mu.Lock()
		ch, ok := ipc.requests[resp.ID]
		if ok {
			delete(ipc.requests, resp.ID)
		}
		ipc.mu.Unlock()

		if ok {
			ch <- &resp
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

	if _, err := ipc.stdin.Write(data); err != nil {
		ipc.mu.Lock()
		delete(ipc.requests, id)
		ipc.mu.Unlock()
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Wait for response
	resp := <-ch
	if resp == nil {
		return nil, fmt.Errorf("plugin closed connection")
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("plugin error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	return resp, nil
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

	if _, err := ipc.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write notification: %w", err)
	}

	return nil
}

// Close closes the IPC connection
func (ipc *IPC) Close() error {
	ipc.mu.Lock()
	defer ipc.mu.Unlock()

	if ipc.closed {
		return nil
	}

	ipc.closed = true
	ipc.stdin.Close()

	// Wait for process to exit
	if ipc.cmd.Process != nil {
		ipc.cmd.Process.Kill()
	}
	ipc.cmd.Wait()

	return nil
}

