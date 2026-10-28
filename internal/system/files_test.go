package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteRejectsSymlinkAndKeepsTarget(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	os.WriteFile(target, []byte("original"), 0600)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if AtomicWrite(link, []byte("overwrite"), 0600, -1, -1) == nil {
		t.Fatal("wrote through symlink")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "original" {
		t.Fatal("modified target")
	}
	if err := AtomicWrite(target, []byte("updated"), 0640, -1, -1); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0640 {
		t.Fatal("wrong permissions")
	}
}

func TestCleanRemovesTerminalControlSequences(t *testing.T) {
	got := Clean("\x1b[31merror\x1b[0m\x1b]52;c;secret\a\x00\nnext")
	if got != "error\nnext" {
		t.Fatalf("unexpected sanitized output: %q", got)
	}
}
