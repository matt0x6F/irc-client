package dcc

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
)

func sendFile(ctx context.Context, conn net.Conn, path string, offset, total int64, progress func(int64)) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("source file is unavailable")
	}
	if offset < 0 || offset > info.Size() {
		return fmt.Errorf("invalid resume position")
	}
	if info.Size() != total {
		return fmt.Errorf("source file changed after it was queued")
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	// Standard DCC receivers acknowledge their cumulative byte count as a
	// four-byte network-order integer. Drain acknowledgements concurrently so a
	// peer with a small socket buffer cannot block while we are still writing.
	ackDone := make(chan struct{})
	go func() {
		defer close(ackDone)
		ack := make([]byte, 4)
		for {
			if _, err := io.ReadFull(conn, ack); err != nil {
				return
			}
		}
	}()

	buf := make([]byte, 64*1024)
	written := offset
	for written < total {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := total - written
		readBuf := buf
		if int64(len(readBuf)) > remaining {
			readBuf = readBuf[:remaining]
		}
		n, readErr := f.Read(readBuf)
		if n > 0 {
			if err := writeAll(conn, readBuf[:n]); err != nil {
				return fmt.Errorf("send file: %w", err)
			}
			written += int64(n)
			progress(written)
		}
		if errors.Is(readErr, io.EOF) {
			return fmt.Errorf("source file became shorter during transfer")
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

func receiveFile(ctx context.Context, conn net.Conn, partialPath string, offset, total int64, progress func(int64)) error {
	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(partialPath, flags, 0o600)
	if err != nil {
		return fmt.Errorf("create partial file: %w", err)
	}
	defer f.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if offset > 0 {
		info, statErr := f.Stat()
		if statErr != nil || info.Size() != offset {
			return fmt.Errorf("partial file changed before resume")
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}

	buf := make([]byte, 64*1024)
	received := offset
	for received < total {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := total - received
		readBuf := buf
		if int64(len(readBuf)) > remaining {
			readBuf = readBuf[:remaining]
		}
		n, readErr := conn.Read(readBuf)
		if n > 0 {
			if _, err := f.Write(readBuf[:n]); err != nil {
				return fmt.Errorf("write partial file: %w", err)
			}
			received += int64(n)
			ack := make([]byte, 4)
			binary.BigEndian.PutUint32(ack, legacyAcknowledgement(received))
			if err := writeAll(conn, ack); err != nil {
				return fmt.Errorf("acknowledge transfer: %w", err)
			}
			progress(received)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return fmt.Errorf("sender closed the connection before the file was complete")
			}
			return readErr
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.Sync()
}

func legacyAcknowledgement(received int64) uint32 {
	return uint32(uint64(received) & 0xffffffff)
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

func finalizePartial(partial, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("a file already exists at the chosen destination")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Link creates the destination exclusively and atomically on the same volume,
	// avoiding os.Rename's overwrite behavior. The partial is removed only after
	// the completed name exists.
	if err := os.Link(partial, destination); err != nil {
		return fmt.Errorf("finish received file: %w", err)
	}
	if err := os.Remove(partial); err != nil {
		return fmt.Errorf("remove completed partial file: %w", err)
	}
	return nil
}
