package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreWriteDocWritesNestedMarkdownDocument(t *testing.T) {
	root := t.TempDir()
	store := &Store{diskDir: root}

	if err := store.WriteDoc("profiles/persona.md", "# Persona\n"); err != nil {
		t.Fatalf("WriteDoc() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "profiles", "persona.md"))
	if err != nil {
		t.Fatalf("read written document: %v", err)
	}
	if got, want := string(content), "# Persona\n"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestStoreWriteDocRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	store := &Store{diskDir: root}
	absolutePath := filepath.Join(root, "outside.md")
	tests := []string{
		"../outside.md",
		`..\outside.md`,
		absolutePath,
		"notes.txt",
	}

	for _, documentPath := range tests {
		t.Run(documentPath, func(t *testing.T) {
			if err := store.WriteDoc(documentPath, "content"); err == nil {
				t.Errorf("WriteDoc(%q) succeeded, want error", documentPath)
			}
		})
	}
}

func TestStoreWriteDocRequiresDiskDirectory(t *testing.T) {
	store := &Store{}
	if err := store.WriteDoc("persona.md", "content"); err == nil {
		t.Error("WriteDoc() succeeded without a disk directory")
	}
}
