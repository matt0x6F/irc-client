package plugin

import (
	"io"
	"sync"
	"testing"
	"time"
)

// blockingWriter models a plugin that has stopped reading its stdin: every Write
// parks until Close(), exactly like a real pipe whose buffer has filled because
// the child process is wedged or too slow.
type blockingWriter struct {
	closed chan struct{}
	once   sync.Once
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *blockingWriter) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

// newTestIPC builds an IPC around a caller-supplied stdin without spawning a
// real plugin process, and starts the writer goroutine — enough to exercise the
// stdin write path in isolation.
func newTestIPC(stdin io.WriteCloser, bufSize int) *IPC {
	ipc := &IPC{
		stdin:    stdin,
		requests: make(map[interface{}]chan *Response),
		nextID:   1,
		pluginID: "test-plugin",
		writeCh:  make(chan []byte, bufSize),
		done:     make(chan struct{}),
	}
	go ipc.writeLoop()
	return ipc
}

// TestSendNotificationDoesNotBlockOnStuckPlugin is the regression test for the
// event-bus freeze diagnosed from a live goroutine dump: SendNotification is
// called synchronously on the single event-bus dispatcher, and it used to write
// the plugin's stdin inline. A plugin that stopped draining its stdin therefore
// parked the dispatcher forever — which filled the event queue and cascaded into
// frozen IRC sends, stuck connection teardown, and no reconnect (the UI showing
// "Connected" over a client that returned ClientDisconnected on every send).
//
// With writes funneled through the writer goroutine, a stuck plugin can only
// block that one goroutine; SendNotification must return promptly regardless,
// dropping notifications once the buffer fills.
func TestSendNotificationDoesNotBlockOnStuckPlugin(t *testing.T) {
	bw := &blockingWriter{closed: make(chan struct{})}
	ipc := newTestIPC(bw, 8)
	t.Cleanup(func() { _ = ipc.Close() })

	done := make(chan struct{})
	go func() {
		// Far more than the buffer can hold; the writer is wedged on the first
		// Write, so all of these must either buffer or drop — never block.
		for range 5000 {
			_ = ipc.SendNotification("event", map[string]any{})
		}
		close(done)
	}()

	select {
	case <-done:
		// Returned promptly despite a plugin that never drains stdin.
	case <-time.After(3 * time.Second):
		t.Fatal("SendNotification blocked on a plugin that stopped draining stdin — this is the event-bus-freezing deadlock")
	}
}

// TestEnqueueWriteBoundedWhenBufferFull guards the enqueue primitive both ways
// against a plugin that never drains stdin: a blocking enqueue (the request
// path) must time out rather than hang forever, and a non-blocking enqueue (the
// notification path) must drop immediately.
func TestEnqueueWriteBoundedWhenBufferFull(t *testing.T) {
	bw := &blockingWriter{closed: make(chan struct{})}
	ipc := newTestIPC(bw, 1)
	t.Cleanup(func() { _ = ipc.Close() })

	// Wedge the writer on its first Write, then fill the single buffer slot so
	// the next enqueue has nowhere to go.
	_ = ipc.enqueueWrite([]byte("a\n"), false)
	time.Sleep(50 * time.Millisecond) // let writeLoop pick it up and park in Write
	_ = ipc.enqueueWrite([]byte("b\n"), false)

	start := time.Now()
	if err := ipc.enqueueWrite([]byte("c\n"), true); err == nil {
		t.Fatal("blocking enqueue should time out when buffer is full and plugin is stuck")
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("blocking enqueue took %v — bounded wait not enforced", d)
	}

	if err := ipc.enqueueWrite([]byte("d\n"), false); err != nil {
		t.Fatalf("non-blocking enqueue should drop, not error: %v", err)
	}
}
