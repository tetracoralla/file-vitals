package linereader

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func readAll(t *testing.T, input string, limit int) [][]byte {
	t.Helper()
	reader := bufio.NewReaderSize(strings.NewReader(input), 64)
	var lines [][]byte
	for {
		line, err := ReadRequestLine(reader, limit)
		if len(line) > 0 {
			lines = append(lines, line)
		}
		if errors.Is(err, io.EOF) {
			return lines
		}
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("unexpected error: %v", err)
		}
		lines = append(lines, []byte("<too-large>"))
	}
}

func TestOversizedLineIsDrainedAndSessionContinues(t *testing.T) {
	pad := strings.Repeat("x", 1000)
	input := "first\n" + pad + "\nlast\r\n"
	lines := readAll(t, input, 64)
	expected := []string{"first", "<too-large>", "last"}
	if len(lines) != len(expected) {
		t.Fatalf("line count %d: %q", len(lines), lines)
	}
	for index, want := range expected {
		if string(lines[index]) != want {
			t.Fatalf("line %d = %q, want %q", index, lines[index], want)
		}
	}
}

func TestTrimsCRLFAndKeepsEOFOnlyLine(t *testing.T) {
	lines := readAll(t, "a\r\nb", 64)
	if len(lines) != 2 || string(lines[0]) != "a" || string(lines[1]) != "b" {
		t.Fatalf("unexpected lines: %q", lines)
	}
}

func TestExactLimitIsAccepted(t *testing.T) {
	line, err := ReadRequestLine(bufio.NewReader(strings.NewReader("12345\n6")), 5)
	if err != nil || !bytes.Equal(line, []byte("12345")) {
		t.Fatalf("exact-limit line rejected: %q %v", line, err)
	}
}
