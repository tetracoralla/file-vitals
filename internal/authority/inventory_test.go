package authority

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCollectRegularFilesIsStableBoundedAndDoesNotFollowLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"z.txt": "z", "a/b.txt": "b", "a/deep/c.txt": "c"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "z.txt"), filepath.Join(root, "linked.txt")); err != nil {
		t.Fatal(err)
	}

	collection, err := CollectRegularFilesContext(context.Background(), root, ".", 1, 32, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer collection.Close()
	paths := make([]string, 0, len(collection.Files))
	for _, item := range collection.Files {
		paths = append(paths, item.Path)
	}
	if !reflect.DeepEqual(paths, []string{"z.txt", "a/b.txt"}) {
		t.Fatalf("stable breadth-first paths: %#v", paths)
	}
	if collection.SymlinksSkipped != 1 || !collection.Truncated || collection.DirectoriesScanned != 2 {
		t.Fatalf("collection facts: %#v", collection)
	}
}

func TestCollectRegularFilesHonorsFileLimit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	collection, err := CollectRegularFilesContext(context.Background(), root, ".", 0, 2, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer collection.Close()
	if len(collection.Files) != 2 || !collection.Truncated {
		t.Fatalf("file limit not enforced: %#v", collection)
	}
}

func TestCollectRegularFilesRejectsAuthorityEscapesAndSymlinkRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, requested := range []string{"../escape", filepath.Join(root, "linked"), "file:///tmp"} {
		if collection, err := CollectRegularFilesContext(context.Background(), root, requested, 1, 32, 256); err == nil {
			collection.Close()
			t.Fatalf("accepted authority escape %q", requested)
		}
	}
	if collection, err := CollectRegularFilesContext(context.Background(), root, "linked", 1, 32, 256); err == nil {
		collection.Close()
		t.Fatal("accepted a symlink inventory root")
	} else if code, _ := Code(err); code != "E_PATH_SYMLINK" {
		t.Fatalf("symlink root code: %s", code)
	}
}

func TestCollectRegularFilesDistinguishesMissingRoot(t *testing.T) {
	if _, err := CollectRegularFilesContext(context.Background(), filepath.Join(t.TempDir(), "absent"), ".", 1, 32, 256); err == nil {
		t.Fatal("accepted a missing inventory root")
	} else if code, _ := Code(err); code != "E_FILE_NOT_FOUND" {
		t.Fatalf("missing inventory root code: %s", code)
	}
	regular := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CollectRegularFilesContext(context.Background(), regular, ".", 1, 32, 256); err == nil {
		t.Fatal("accepted a regular file as the inventory root")
	} else if code, _ := Code(err); code != "E_NOT_DIRECTORY" {
		t.Fatalf("file-as-root code: %s", code)
	}
}
