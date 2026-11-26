package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWithinRoot(t *testing.T) {
	root := filepath.FromSlash("/srv/share")
	cases := map[string]bool{
		root:                                  true,
		filepath.Join(root, "a"):              true,
		filepath.Join(root, "a", "b.txt"):     true,
		filepath.FromSlash("/srv/share2/a"):   false,
		filepath.FromSlash("/srv/other"):      false,
		filepath.FromSlash("/srv/share/../x"): false,
	}
	for path, want := range cases {
		if got := withinRoot(root, path); got != want {
			t.Fatalf("withinRoot(%q)=%v want %v", path, got, want)
		}
	}
}

func TestSafeUploadName(t *testing.T) {
	cases := map[string]string{
		"report.txt":       "report.txt",
		"../../etc/passwd": "passwd",
		`C:\temp\a.txt`:    "a.txt",
		"a\x00b.txt":       "a_b.txt",
		"a<b>c?.txt":       "a_b_c_.txt",
		"name...":          "name",
		"..":               "",
		".":                "",
		"":                 "",
	}
	for in, want := range cases {
		if got := safeUploadName(in); got != want {
			t.Fatalf("safeUploadName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestCreateUniqueNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(first, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, file, err := createUnique(dir, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if path == first {
		t.Fatal("createUnique overwrote existing file")
	}
	if !strings.Contains(filepath.Base(path), "(1)") {
		t.Fatalf("unexpected unique name: %s", path)
	}
}

func TestResolveExistingRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 创建符号链接通常需要管理员权限")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	fb := newFileBrowser([]string{root}, 0)
	if fb == nil {
		t.Fatal("nil file browser")
	}
	if _, ok := fb.resolveExisting(filepath.Join(root, "link", "secret.txt")); ok {
		t.Fatal("symlink escape accepted")
	}
	if real, ok := fb.resolveExisting(filepath.Join(root, "link")); ok {
		t.Fatalf("symlink itself accepted: %s", real)
	}
}

func TestUploadLimit(t *testing.T) {
	root := t.TempDir()
	fb := newFileBrowser([]string{root}, 10)
	if fb == nil {
		t.Fatal("nil file browser")
	}
	s := &session{
		agent:    &Agent{files: fb},
		uploads:  map[uint32]string{1: root, 2: root},
		incoming: map[uint32]*incomingUpload{},
	}
	s.beginUpload(1, "big.bin", 11)
	if s.incoming[1] != nil {
		t.Fatal("oversized upload accepted")
	}
	s.beginUpload(2, "ok.bin", 5)
	up := s.incoming[2]
	if up == nil {
		t.Fatal("valid upload rejected")
	}
	up.file.Close()
}
func TestFileBrowserRootsAndPagination(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		name := filepath.Join(root, "f"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fb := newFileBrowser([]string{root}, 0)
	if fb == nil {
		t.Fatal("nil file browser")
	}
	roots := fb.list("", "", 1)
	if roots["ok"] != true {
		t.Fatalf("roots failed: %v", roots)
	}
	entries, _ := roots["entries"].([]fileEntry)
	if len(entries) != 1 || entries[0].Path == "" {
		t.Fatalf("unexpected roots: %+v", entries)
	}
	listing := fb.list(root, "", 1)
	if listing["ok"] != true {
		t.Fatalf("list failed: %v", listing)
	}
	page, _ := listing["entries"].([]fileEntry)
	if len(page) != 20 || listing["truncated"] != false {
		t.Fatalf("unexpected page: %d truncated=%v", len(page), listing["truncated"])
	}
}
