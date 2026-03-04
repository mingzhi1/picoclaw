package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellInstance_BuiltinRestricted_BlocksOutsidePath(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	// Create a secret file outside the workspace
	secretFile := filepath.Join(root, "secret.txt")
	os.WriteFile(secretFile, []byte("top secret"), 0o644)

	si := NewShellInstance(nil, workspace, true)

	tests := []struct {
		name string
		args []string
	}{
		{"cat absolute path outside", []string{"cat", secretFile}},
		{"cat relative escape", []string{"cat", "../secret.txt"}},
		{"cp outside source", []string{"cp", secretFile, "local.txt"}},
		{"cp outside dest", []string{"cp", "local.txt", secretFile}},
		{"grep outside", []string{"grep", "secret", secretFile}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := si.Execute(tt.args)
			if !strings.Contains(result, "blocked") {
				t.Errorf("expected builtin to be blocked, got: %s", result)
			}
		})
	}
}

func TestShellInstance_BuiltinRestricted_AllowsInsidePath(t *testing.T) {
	workspace := t.TempDir()
	testFile := filepath.Join(workspace, "hello.txt")
	os.WriteFile(testFile, []byte("hello world"), 0o644)

	si := NewShellInstance(nil, workspace, true)

	// cat a file inside the workspace (by basename, resolved relative to cwd=workspace)
	result := si.Execute([]string{"cat", "hello.txt"})
	if strings.Contains(result, "blocked") {
		t.Errorf("expected in-workspace file to be allowed, got: %s", result)
	}
	if !strings.Contains(result, "hello world") {
		t.Errorf("expected file content in output, got: %s", result)
	}
}

func TestShellInstance_BuiltinUnrestricted_AllowsAnywhere(t *testing.T) {
	workspace := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "data.txt")
	os.WriteFile(outsideFile, []byte("external data"), 0o644)

	si := NewShellInstance(nil, workspace, false)

	result := si.Execute([]string{"cat", outsideFile})
	if strings.Contains(result, "blocked") {
		t.Errorf("unrestricted mode should allow outside paths, got: %s", result)
	}
	if !strings.Contains(result, "external data") {
		t.Errorf("expected file content, got: %s", result)
	}
}

func TestShellInstance_BuiltinRestricted_FlagsNotBlocked(t *testing.T) {
	workspace := t.TempDir()
	os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("x"), 0o644)

	si := NewShellInstance(nil, workspace, true)

	// ls with flags should work
	result := si.Execute([]string{"ls", "-la"})
	if strings.Contains(result, "blocked") {
		t.Errorf("flags should not be treated as paths, got: %s", result)
	}
}
