// Package linereader reads length-bounded newline-delimited request lines. A
// line that exceeds the budget is drained so the next request still parses
// instead of desynchronizing or killing the session.
package linereader

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// ErrTooLarge marks a request line that exceeded the transport budget.
var ErrTooLarge = errors.New("request line exceeds the transport budget")

// ReadRequestLine returns one trimmed line, ErrTooLarge when the line exceeded
// limit (after draining its remainder), or io.EOF at end of input.
func ReadRequestLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	overflow := false
	for {
		chunk, err := reader.ReadSlice('\n')
		if !overflow {
			// Keep room for CRLF while applying the declared limit only to
			// the JSON document, not its transport delimiter.
			if len(line)+len(chunk) > limit+2 {
				overflow = true
				line = nil
			} else {
				line = append(line, chunk...)
			}
		}
		if err == nil {
			line = TrimLineEnding(line)
			if overflow || len(line) > limit {
				return nil, ErrTooLarge
			}
			return line, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			line = TrimLineEnding(line)
			if overflow || len(line) > limit {
				return nil, ErrTooLarge
			}
			return line, io.EOF
		}
		return nil, err
	}
}

func TrimLineEnding(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte("\n"))
	return bytes.TrimSuffix(line, []byte("\r"))
}
