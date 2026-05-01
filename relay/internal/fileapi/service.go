// Package fileapi provides the relay's read-only file browser backend.
package fileapi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

const DefaultMaxFileBytes = 1 << 20

var (
	ErrDisabled      = errors.New("fileapi: disabled")
	ErrRootNotFound  = errors.New("fileapi: root not found")
	ErrForbidden     = errors.New("fileapi: path outside allowed root")
	ErrNotFound      = errors.New("fileapi: path not found")
	ErrNotDirectory  = errors.New("fileapi: path is not a directory")
	ErrNotText       = errors.New("fileapi: file is not text")
	ErrTooLarge      = errors.New("fileapi: file is too large")
	ErrInvalidConfig = errors.New("fileapi: invalid config")
	ErrInvalidBase   = errors.New("fileapi: invalid base path")
)

type RootConfig struct {
	ID   string
	Name string
	Path string
}

type Config struct {
	Roots        []RootConfig
	MaxFileBytes int
}

type Service struct {
	roots        []Root
	byID         map[string]Root
	maxFileBytes int
}

type Root struct {
	ID   string
	Name string
	Path string
}

type RootInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

type ListResult struct {
	Root    string  `json:"root"`
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

type Document struct {
	Root       string    `json:"root"`
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
	Encoding   string    `json:"encoding"`
	Content    string    `json:"content"`
}

func NewService(cfg Config) (*Service, error) {
	maxBytes := cfg.MaxFileBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxFileBytes
	}
	if maxBytes < 0 {
		return nil, fmt.Errorf("%w: max_file_bytes must be > 0", ErrInvalidConfig)
	}

	s := &Service{
		byID:         map[string]Root{},
		maxFileBytes: maxBytes,
	}
	for _, r := range cfg.Roots {
		if strings.TrimSpace(r.ID) == "" {
			return nil, fmt.Errorf("%w: root id must be set", ErrInvalidConfig)
		}
		if strings.TrimSpace(r.Name) == "" {
			return nil, fmt.Errorf("%w: root name must be set", ErrInvalidConfig)
		}
		if strings.TrimSpace(r.Path) == "" {
			return nil, fmt.Errorf("%w: root path must be set", ErrInvalidConfig)
		}
		if _, ok := s.byID[r.ID]; ok {
			return nil, fmt.Errorf("%w: duplicate root id %q", ErrInvalidConfig, r.ID)
		}
		abs, err := filepath.Abs(r.Path)
		if err != nil {
			return nil, fmt.Errorf("%w: root %q path: %v", ErrInvalidConfig, r.ID, err)
		}
		realPath, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("%w: root %q path: %v", ErrInvalidConfig, r.ID, err)
		}
		info, err := os.Stat(realPath)
		if err != nil {
			return nil, fmt.Errorf("%w: root %q path: %v", ErrInvalidConfig, r.ID, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%w: root %q is not a directory", ErrInvalidConfig, r.ID)
		}
		root := Root{
			ID:   r.ID,
			Name: r.Name,
			Path: filepath.Clean(realPath),
		}
		s.roots = append(s.roots, root)
		s.byID[root.ID] = root
	}
	return s, nil
}

func (s *Service) Enabled() bool {
	return s != nil && len(s.roots) > 0
}

func (s *Service) Roots() []RootInfo {
	if s == nil {
		return nil
	}
	out := make([]RootInfo, 0, len(s.roots))
	for _, r := range s.roots {
		out = append(out, RootInfo{ID: r.ID, Name: r.Name})
	}
	return out
}

// ListAt is the root-less variant of List used by session-scoped file
// browsing: basePath plays the role of an ad-hoc root (the session's
// current working directory) and is treated as the jail. The caller is
// responsible for supplying an absolute base path; relative paths are
// rejected with ErrInvalidBase. The returned ListResult has an empty
// Root field; the caller is expected to fill it in with whatever
// session-scoped identifier its API surface uses.
func (s *Service) ListAt(basePath, path string) (ListResult, error) {
	base, err := normalizeBase(basePath)
	if err != nil {
		return ListResult{}, err
	}
	abs, rel, err := resolveBase(base, path)
	if err != nil {
		return ListResult{}, err
	}
	return s.listResolved(base, abs, rel)
}

// ReadAt is the root-less variant of Read; see ListAt for semantics.
func (s *Service) ReadAt(basePath, path string) (Document, error) {
	base, err := normalizeBase(basePath)
	if err != nil {
		return Document{}, err
	}
	abs, rel, err := resolveBase(base, path)
	if err != nil {
		return Document{}, err
	}
	return s.readResolved(abs, rel)
}

func (s *Service) List(rootID, path string) (ListResult, error) {
	root, abs, rel, err := s.resolve(rootID, path)
	if err != nil {
		return ListResult{}, err
	}
	result, err := s.listResolved(root.Path, abs, rel)
	if err != nil {
		return ListResult{}, err
	}
	result.Root = root.ID
	return result, nil
}

func (s *Service) listResolved(base, abs, rel string) (ListResult, error) {
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ListResult{}, ErrNotFound
		}
		return ListResult{}, err
	}
	if !info.IsDir() {
		return ListResult{}, ErrNotDirectory
	}

	items, err := os.ReadDir(abs)
	if err != nil {
		return ListResult{}, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		entryPath := joinRelative(rel, item.Name())
		childAbs, _, err := resolveBase(base, entryPath)
		if err != nil {
			if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
				continue
			}
			return ListResult{}, err
		}
		childInfo, err := os.Stat(childAbs)
		if err != nil {
			continue
		}
		entryType := "file"
		if childInfo.IsDir() {
			entryType = "directory"
		}
		entries = append(entries, Entry{
			Name:       item.Name(),
			Path:       filepath.ToSlash(entryPath),
			Type:       entryType,
			Size:       childInfo.Size(),
			ModifiedAt: childInfo.ModTime().UTC(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "directory"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return ListResult{
		Path:    filepath.ToSlash(rel),
		Entries: entries,
	}, nil
}

func (s *Service) Read(rootID, path string) (Document, error) {
	root, abs, rel, err := s.resolve(rootID, path)
	if err != nil {
		return Document{}, err
	}
	doc, err := s.readResolved(abs, rel)
	if err != nil {
		return Document{}, err
	}
	doc.Root = root.ID
	return doc, nil
}

func (s *Service) readResolved(abs, rel string) (Document, error) {
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Document{}, ErrNotFound
		}
		return Document{}, err
	}
	if info.IsDir() {
		return Document{}, ErrNotText
	}
	if info.Size() > int64(s.maxFileBytes) {
		return Document{}, ErrTooLarge
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return Document{}, err
	}
	content, encoding, err := decodeText(data)
	if err != nil {
		return Document{}, err
	}
	return Document{
		Path:       filepath.ToSlash(rel),
		Name:       filepath.Base(abs),
		Size:       info.Size(),
		ModifiedAt: info.ModTime().UTC(),
		Encoding:   encoding,
		Content:    content,
	}, nil
}

func (s *Service) resolve(rootID, requested string) (Root, string, string, error) {
	if s == nil || len(s.roots) == 0 {
		return Root{}, "", "", ErrDisabled
	}
	root, ok := s.byID[rootID]
	if !ok {
		return Root{}, "", "", ErrRootNotFound
	}
	abs, rel, err := resolveBase(root.Path, requested)
	if err != nil {
		return Root{}, "", "", err
	}
	return root, abs, rel, nil
}

// normalizeBase returns the absolute, symlink-resolved, cleaned form of
// the supplied base path. The base must already be absolute; relative
// paths are rejected with ErrInvalidBase so callers never silently get
// CWD-relative behavior on the relay process.
func normalizeBase(basePath string) (string, error) {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return "", ErrInvalidBase
	}
	native := filepath.FromSlash(basePath)
	if !filepath.IsAbs(native) {
		return "", ErrInvalidBase
	}
	abs := filepath.Clean(native)
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	if !info.IsDir() {
		return "", ErrNotDirectory
	}
	return filepath.Clean(real), nil
}

// resolveBase computes the absolute target for a request that names
// `requested` relative to base, then enforces that the resolved target
// (after symlink eval) stays under base.
func resolveBase(base, requested string) (string, string, error) {
	nativePath := filepath.FromSlash(strings.TrimSpace(requested))
	var target string
	switch {
	case nativePath == "" || nativePath == ".":
		target = base
	case filepath.IsAbs(nativePath):
		target = filepath.Clean(nativePath)
	default:
		target = filepath.Join(base, filepath.Clean(nativePath))
	}
	target = filepath.Clean(target)
	if !insideRoot(base, target) {
		return "", "", ErrForbidden
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	realTarget = filepath.Clean(realTarget)
	if !insideRoot(base, realTarget) {
		return "", "", ErrForbidden
	}
	rel, err := filepath.Rel(base, realTarget)
	if err != nil {
		return "", "", err
	}
	if rel == "." {
		rel = ""
	}
	return realTarget, rel, nil
}

func insideRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(rel))
}

func joinRelative(base, name string) string {
	if base == "" {
		return name
	}
	return filepath.Join(base, name)
}

func decodeText(data []byte) (string, string, error) {
	if len(data) == 0 {
		return "", "utf-8", nil
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		data = data[3:]
		if !utf8.Valid(data) {
			return "", "", ErrNotText
		}
		text := string(data)
		if !looksLikeText(text) {
			return "", "", ErrNotText
		}
		return text, "utf-8", nil
	}
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) {
		text, ok := decodeUTF16(data[2:], binary.LittleEndian)
		if !ok || !looksLikeText(text) {
			return "", "", ErrNotText
		}
		return text, "utf-16le", nil
	}
	if bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		text, ok := decodeUTF16(data[2:], binary.BigEndian)
		if !ok || !looksLikeText(text) {
			return "", "", ErrNotText
		}
		return text, "utf-16be", nil
	}
	if !bytes.Contains(data, []byte{0}) && utf8.Valid(data) {
		text := string(data)
		if !looksLikeText(text) {
			return "", "", ErrNotText
		}
		return text, "utf-8", nil
	}
	if bytes.Contains(data, []byte{0}) {
		return "", "", ErrNotText
	}
	reader := transform.NewReader(bytes.NewReader(data), japanese.ShiftJIS.NewDecoder())
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", "", ErrNotText
	}
	text := string(decoded)
	if !looksLikeText(text) {
		return "", "", ErrNotText
	}
	return text, "shift_jis", nil
}

func decodeUTF16(data []byte, order binary.ByteOrder) (string, bool) {
	if len(data)%2 != 0 {
		return "", false
	}
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		units = append(units, order.Uint16(data[i:i+2]))
	}
	return string(utf16.Decode(units)), true
}

func looksLikeText(text string) bool {
	for _, r := range text {
		switch r {
		case '\t', '\n', '\r', '\f':
			continue
		}
		if r == utf8.RuneError || r == 0 || r < 0x20 {
			return false
		}
	}
	return true
}
