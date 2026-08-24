package authority

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRelativeContextHonorsCancellationBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	file, err := OpenRelativeContext(ctx, t.TempDir(), "missing")
	if file != nil {
		file.Close()
		t.Fatal("cancelled open returned a file")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestOpenRelativeAcceptsRegularFile(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "nested", "ok.txt")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := OpenRelative(root, "nested/ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
}

func TestOpenRelativeRejectsEscapeForms(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{"absolute": filepath.Join(root, "ok.txt"), "parent": "../ok.txt", "uri": "file:///etc/passwd", "directory": "."}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if file, err := OpenRelative(root, value); err == nil {
				file.Close()
				t.Fatalf("accepted %q", value)
			}
		})
	}
}

func TestOpenRelativeRejectsEverySymlinkComponent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "file-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dir-link")); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"file-link", "dir-link/secret.txt"} {
		if file, err := OpenRelative(root, value); err == nil {
			file.Close()
			t.Fatalf("accepted symlink %q", value)
		} else if code, _ := Code(err); code != "E_PATH_SYMLINK" {
			t.Fatalf("wrong code for %q: %s", value, code)
		}
	}
}

func TestWorkspaceIsRequired(t *testing.T) {
	if file, err := OpenRelative("", "x"); err == nil {
		file.Close()
		t.Fatal("missing workspace accepted")
	} else if code, _ := Code(err); code != "E_WORKSPACE_REQUIRED" {
		t.Fatalf("unexpected code: %s", code)
	}
}
