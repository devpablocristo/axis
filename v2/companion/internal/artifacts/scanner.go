package artifacts

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// DevelopmentScanner is explicit fail-open infrastructure for local fixtures.
// Production wiring rejects it and requires a real scanner adapter.
type DevelopmentScanner struct{}

func (DevelopmentScanner) Scan(context.Context, Manifest, Blob) error { return nil }

// ClamAVScanner implements clamd's INSTREAM protocol without shelling out or
// putting product-controlled filenames on a command line.
type ClamAVScanner struct {
	Address string
	Timeout time.Duration
}

func (s ClamAVScanner) Scan(ctx context.Context, _ Manifest, blob Blob) error {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", strings.TrimSpace(s.Address))
	if err != nil {
		return fmt.Errorf("connect clamd: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(conn, "zINSTREAM\x00"); err != nil {
		return fmt.Errorf("start clamd stream: %w", err)
	}

	r, err := blob.Open()
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	buf := make([]byte, 32<<10)
	var length [4]byte
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			binary.BigEndian.PutUint32(length[:], uint32(n))
			if _, err := conn.Write(length[:]); err != nil {
				return fmt.Errorf("write clamd chunk length: %w", err)
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return fmt.Errorf("write clamd chunk: %w", err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return fmt.Errorf("finish clamd stream: %w", err)
	}
	response, err := bufio.NewReader(io.LimitReader(conn, 4<<10)).ReadString(0)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read clamd response: %w", err)
	}
	response = strings.TrimSpace(strings.TrimSuffix(response, "\x00"))
	if strings.HasSuffix(response, "OK") {
		return nil
	}
	if strings.Contains(response, "FOUND") {
		return errors.New("malware detected")
	}
	return fmt.Errorf("clamd rejected stream")
}
