package main

import (
	"os/exec"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDoctorDoesNotReportFailingExecutableAvailable(t *testing.T) {
	falsePath, err := exec.LookPath("false")
	if err != nil {
		t.Skip("false executable unavailable")
	}
	probe := probePathVersion("false", falsePath)
	if probe.Available {
		t.Fatalf("failing executable reported available: %#v", probe)
	}
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true executable unavailable")
	}
	probe = probePathVersion("true", truePath)
	if !probe.Available {
		t.Fatalf("successful executable reported unavailable: %#v", probe)
	}
}

func TestBoundedCLIPreservesUTF8AtByteLimit(t *testing.T) {
	value := boundedCLI(strings.Repeat("界", 60), 160)
	if !utf8.ValidString(value) || len(value) > 160 {
		t.Fatalf("bounded CLI text is invalid: bytes=%d value=%q", len(value), value)
	}
}
