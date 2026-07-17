package nativecodec

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

const MaxSSEEventBytes = 8 << 20

type SSEOptions struct {
	RewriteData func([]byte) ([]byte, error)
	ObserveData func([]byte)
	Flush       func() error
}

func CopySSE(dst io.Writer, src io.Reader, options SSEOptions) (int64, error) {
	reader := bufio.NewReaderSize(src, 32<<10)
	event := make([]byte, 0, 4096)
	var written int64
	for {
		fragment, readErr := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			if len(event)+len(fragment) > MaxSSEEventBytes {
				return written, errors.New("upstream SSE event exceeds size limit")
			}
			event = append(event, fragment...)
			if readErr == nil && blankLine(fragment) {
				n, writeErr := writeEvent(dst, event, options)
				written += int64(n)
				event = event[:0]
				if writeErr != nil {
					return written, writeErr
				}
			}
		}
		if readErr == nil || errors.Is(readErr, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			if len(event) > 0 {
				n, writeErr := writeEvent(dst, event, options)
				written += int64(n)
				if writeErr != nil {
					return written, writeErr
				}
			}
			return written, nil
		}
		return written, readErr
	}
}

func writeEvent(dst io.Writer, event []byte, options SSEOptions) (int, error) {
	data, hasData := eventData(event)
	output := event
	if hasData && !bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
		if options.ObserveData != nil {
			options.ObserveData(data)
		}
		if options.RewriteData != nil {
			rewritten, err := options.RewriteData(data)
			if err != nil {
				return 0, err
			}
			output = replaceEventData(event, rewritten)
		}
	}
	n, err := dst.Write(output)
	if err != nil {
		return n, err
	}
	if options.Flush != nil {
		if err := options.Flush(); err != nil {
			return n, err
		}
	}
	return n, nil
}

func eventData(event []byte) ([]byte, bool) {
	var values [][]byte
	for _, rawLine := range bytes.Split(event, []byte{'\n'}) {
		line := bytes.TrimSuffix(rawLine, []byte{'\r'})
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		value := bytes.TrimPrefix(line, []byte("data:"))
		value = bytes.TrimPrefix(value, []byte{' '})
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, false
	}
	return bytes.Join(values, []byte{'\n'}), true
}

func replaceEventData(event, data []byte) []byte {
	lines := bytes.Split(event, []byte{'\n'})
	result := make([]byte, 0, len(event)+len(data))
	inserted := false
	for _, rawLine := range lines {
		line := bytes.TrimSuffix(rawLine, []byte{'\r'})
		if bytes.HasPrefix(line, []byte("data:")) {
			if !inserted {
				result = append(result, "data: "...)
				result = append(result, data...)
				result = append(result, '\n')
				inserted = true
			}
			continue
		}
		if len(rawLine) == 0 && len(result) > 0 && result[len(result)-1] == '\n' {
			result = append(result, '\n')
			continue
		}
		result = append(result, rawLine...)
		result = append(result, '\n')
	}
	if !bytes.HasSuffix(result, []byte("\n\n")) {
		result = bytes.TrimRight(result, "\n")
		result = append(result, '\n', '\n')
	}
	return result
}

func blankLine(line []byte) bool {
	return bytes.Equal(line, []byte("\n")) || bytes.Equal(line, []byte("\r\n"))
}
