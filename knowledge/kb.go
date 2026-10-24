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
	relPath = path.Clean(strings.TrimSpace(relPath))
	if relPath == "." || path.IsAbs(relPath) || relPath == ".." || strings.HasPrefix(relPath, "../") {
		return "", fmt.Errorf("document path must be a relative path within the knowledge base")
	}
	return relPath, nil
}
