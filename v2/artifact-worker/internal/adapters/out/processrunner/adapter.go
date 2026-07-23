package processrunner

import (
	"bytes"
	"context"
	"errors"
	"os/exec"

	"github.com/devpablocristo/artifact-worker-v2/internal/extractor"
)

const maxCommandOutputBytes = 16 << 20

type Adapter struct{}

func (Adapter) Run(ctx context.Context, workDir, name string, arguments ...string) ([]byte, error) {
	var command *exec.Cmd
	switch name {
	case "soffice":
		command = exec.CommandContext(ctx, "soffice", arguments...)
	case "pdftotext":
		command = exec.CommandContext(ctx, "pdftotext", arguments...)
	case "pdftoppm":
		command = exec.CommandContext(ctx, "pdftoppm", arguments...)
	case "tesseract":
		command = exec.CommandContext(ctx, "tesseract", arguments...)
	case "convert":
		command = exec.CommandContext(ctx, "convert", arguments...)
	case "ffmpeg":
		command = exec.CommandContext(ctx, "ffmpeg", arguments...)
	case "dcmdump":
		command = exec.CommandContext(ctx, "dcmdump", arguments...)
	case "dcmj2pnm":
		command = exec.CommandContext(ctx, "dcmj2pnm", arguments...)
	case "whisper-cli":
		command = exec.CommandContext(ctx, "whisper-cli", arguments...)
	case "/usr/local/bin/whisper-cli":
		command = exec.CommandContext(ctx, "/usr/local/bin/whisper-cli", arguments...)
	default:
		return nil, extractor.ErrUnsupported
	}
	command.Dir = workDir
	output := &limitedBuffer{remaining: maxCommandOutputBytes}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, extractor.ErrUnavailable
		}
		return nil, errors.New("extractor command failed")
	}
	return output.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	remaining int
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	if len(data) > buffer.remaining {
		data = data[:buffer.remaining]
	}
	written, err := buffer.Buffer.Write(data)
	buffer.remaining -= written
	return originalLength, err
}
