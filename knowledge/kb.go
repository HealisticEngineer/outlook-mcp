package knowledge

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type Document struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Store resolves local Markdown overrides before embedded defaults.
type Store struct {
	embeddedFS fs.FS
	diskDir    string
}

func NewStore(embeddedFS fs.FS, diskDir string) (*Store, error) {
	defaults, err := fs.Sub(embeddedFS, "defaults")
	if err != nil {
		return nil, fmt.Errorf("open embedded defaults: %w", err)
	}
	return &Store{embeddedFS: defaults, diskDir: diskDir}, nil
}

func (s *Store) ListDocs() ([]string, error) {
	documents := make(map[string]struct{})
	if err := addMarkdownPaths(s.embeddedFS, ".", documents); err != nil {
		return nil, fmt.Errorf("list embedded documents: %w", err)
	}
	if s.diskDir != "" {
		if err := addDiskMarkdownPaths(s.diskDir, documents); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("list disk documents: %w", err)
		}
	}
	paths := make([]string, 0, len(documents))
	for documentPath := range documents {
		paths = append(paths, documentPath)
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *Store) ReadDoc(relPath string) (*Document, error) {
	relPath, err := validRelativePath(relPath)
	if err != nil {
		return nil, err
	}
	if s.diskDir != "" {
		content, diskErr := os.ReadFile(filepath.Join(s.diskDir, filepath.FromSlash(relPath)))
		if diskErr == nil {
			return &Document{Path: relPath, Content: string(content)}, nil
		}
		if !os.IsNotExist(diskErr) {
			return nil, fmt.Errorf("read disk document %q: %w", relPath, diskErr)
		}
	}
	content, err := fs.ReadFile(s.embeddedFS, relPath)
	if err != nil {
		return nil, fmt.Errorf("read embedded document %q: %w", relPath, err)
	}
	return &Document{Path: relPath, Content: string(content)}, nil
}

// WriteDoc safely writes a Markdown document to the physical disk KB directory.
// It explicitly rejects paths that could resolve outside that directory.
func (s *Store) WriteDoc(relPath string, content string) error {
	if s.diskDir == "" {
		return fmt.Errorf("disk directory is not configured; cannot write documents")
	}
	relPath, err := validRelativePath(relPath)
	if err != nil {
		return fmt.Errorf("unauthorized path traversal attempt: %w", err)
	}
	if !strings.EqualFold(filepath.Ext(relPath), ".md") {
		return fmt.Errorf("document path must have a .md extension")
	}

	baseDir, err := filepath.Abs(s.diskDir)
	if err != nil {
		return fmt.Errorf("resolve knowledge base directory: %w", err)
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("create knowledge base directory: %w", err)
	}
	baseDir, err = filepath.EvalSymlinks(baseDir)
	if err != nil {
		return fmt.Errorf("resolve knowledge base directory links: %w", err)
	}

	fullPath, err := filepath.Abs(filepath.Join(baseDir, filepath.FromSlash(relPath)))
	if err != nil {
		return fmt.Errorf("resolve document path: %w", err)
	}
	if err := ensurePathWithin(baseDir, fullPath); err != nil {
		return err
	}
	if err := createSafeParentDirectories(baseDir, filepath.Dir(fullPath)); err != nil {
		return err
	}
	if info, err := os.Lstat(fullPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			return fmt.Errorf("document path must not be a symbolic link or directory")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect document path: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write document: %w", err)
	}
	return nil
}

func (s *Store) Search(query string) ([]Document, error) {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil, fmt.Errorf("search query is required")
	}
	paths, err := s.ListDocs()
	if err != nil {
		return nil, err
	}
	results := make([]Document, 0)
	for _, documentPath := range paths {
		document, err := s.ReadDoc(documentPath)
		if err != nil {
			return nil, err
		}
		if strings.Contains(strings.ToLower(document.Path), query) || strings.Contains(strings.ToLower(document.Content), query) {
			results = append(results, *document)
		}
	}
	return results, nil
}

func addMarkdownPaths(fileSystem fs.FS, root string, documents map[string]struct{}) error {
	return fs.WalkDir(fileSystem, root, func(documentPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(path.Ext(documentPath), ".md") {
			documents[strings.TrimPrefix(documentPath, "./")] = struct{}{}
		}
		return nil
	})
}

func addDiskMarkdownPaths(root string, documents map[string]struct{}) error {
	return filepath.WalkDir(root, func(documentPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(documentPath), ".md") {
			relative, err := filepath.Rel(root, documentPath)
			if err != nil {
				return err
			}
			documents[filepath.ToSlash(relative)] = struct{}{}
		}
		return nil
	})
}

func validRelativePath(relPath string) (string, error) {
	relPath = filepath.Clean(strings.TrimSpace(relPath))
	if relPath == "." || filepath.IsAbs(relPath) || filepath.VolumeName(relPath) != "" || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("document path must be a relative path within the knowledge base")
	}
	return filepath.ToSlash(relPath), nil
}

func ensurePathWithin(baseDir, fullPath string) error {
	relative, err := filepath.Rel(baseDir, fullPath)
	if err != nil {
		return fmt.Errorf("resolve document path boundary: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("resolved path is outside the permitted knowledge base directory")
	}
	return nil
}

func createSafeParentDirectories(baseDir, parentDir string) error {
	relativeParent, err := filepath.Rel(baseDir, parentDir)
	if err != nil {
		return fmt.Errorf("resolve parent directory boundary: %w", err)
	}
	if relativeParent == "." {
		return nil
	}
	current := baseDir
	for _, component := range strings.Split(relativeParent, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0755); err != nil {
				return fmt.Errorf("create document directory: %w", err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect document directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("document directory must not be a symbolic link or file")
		}
	}
	return nil
}
