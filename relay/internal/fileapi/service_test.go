package fileapi

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"golang.org/x/text/encoding/japanese"
)

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "note.txt"), []byte("hello\nworld\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Config{
		Roots: []RootConfig{{ID: "main", Name: "Main", Path: root}},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc, root
}

func TestList(t *testing.T) {
	svc, _ := newTestService(t)
	got, err := svc.List("main", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got.Root != "main" || got.Path != "" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if len(got.Entries) != 1 || got.Entries[0].Name != "dir" || got.Entries[0].Type != "directory" {
		t.Fatalf("entries = %+v", got.Entries)
	}
}

func TestReadUTF8(t *testing.T) {
	svc, _ := newTestService(t)
	got, err := svc.Read("main", "dir/note.txt")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Encoding != "utf-8" || got.Content != "hello\nworld\n" {
		t.Fatalf("document = %+v", got)
	}
}

func TestReadShiftJIS(t *testing.T) {
	root := t.TempDir()
	encoded, err := japanese.ShiftJIS.NewEncoder().Bytes([]byte("日本語"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sjis.txt"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Config{
		Roots: []RootConfig{{ID: "main", Name: "Main", Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Read("main", "sjis.txt")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Encoding != "shift_jis" || got.Content != "日本語" {
		t.Fatalf("document = %+v", got)
	}
}

func TestReadUTF16LE(t *testing.T) {
	root := t.TempDir()
	units := utf16.Encode([]rune("hello"))
	data := []byte{0xff, 0xfe}
	for _, u := range units {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], u)
		data = append(data, b[:]...)
	}
	if err := os.WriteFile(filepath.Join(root, "utf16.txt"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Config{
		Roots: []RootConfig{{ID: "main", Name: "Main", Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Read("main", "utf16.txt")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Encoding != "utf-16le" || got.Content != "hello" {
		t.Fatalf("document = %+v", got)
	}
}

func TestRejectsRootEscape(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.List("main", "../"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err=%v want ErrForbidden", err)
	}
}

func TestRejectsAbsolutePathOutsideRoot(t *testing.T) {
	svc, _ := newTestService(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Read("main", outside); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err=%v want ErrForbidden", err)
	}
}

func TestRejectsMissingPathOutsideRoot(t *testing.T) {
	svc, _ := newTestService(t)
	outside := filepath.Join(t.TempDir(), "missing.txt")
	if _, err := svc.Read("main", outside); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err=%v want ErrForbidden", err)
	}
}

func TestReadTooLarge(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Config{
		Roots:        []RootConfig{{ID: "main", Name: "Main", Path: root}},
		MaxFileBytes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Read("main", "big.txt"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err=%v want ErrTooLarge", err)
	}
}

func TestReadRejectsBinary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bin.dat"), []byte{0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Config{
		Roots: []RootConfig{{ID: "main", Name: "Main", Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Read("main", "bin.dat"); !errors.Is(err, ErrNotText) {
		t.Fatalf("err=%v want ErrNotText", err)
	}
}

func TestInvalidConfig(t *testing.T) {
	root := t.TempDir()
	cases := []Config{
		{Roots: []RootConfig{{ID: "", Name: "Main", Path: root}}},
		{Roots: []RootConfig{{ID: "main", Name: "", Path: root}}},
		{Roots: []RootConfig{{ID: "main", Name: "Main", Path: ""}}},
		{Roots: []RootConfig{{ID: "main", Name: "Main", Path: root}, {ID: "main", Name: "Other", Path: root}}},
		{Roots: []RootConfig{{ID: "main", Name: "Main", Path: root}}, MaxFileBytes: -1},
	}
	for _, cfg := range cases {
		if _, err := NewService(cfg); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("cfg=%+v err=%v want ErrInvalidConfig", cfg, err)
		}
	}
}
