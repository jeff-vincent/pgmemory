package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/.pgmemory/models/foo.gguf", filepath.Join(home, ".pgmemory/models/foo.gguf")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDefault_HasSaneValues(t *testing.T) {
	if Default.Port != 7432 {
		t.Errorf("default port = %d, want 7432", Default.Port)
	}
	if Default.EmbeddingDim != 1024 {
		t.Errorf("default embedding dim = %d, want 1024", Default.EmbeddingDim)
	}
	if Default.RetrievalTopK != 5 {
		t.Errorf("default retrieval top-k = %d, want 5", Default.RetrievalTopK)
	}
	if Default.UpstreamAnthropicURL != "https://api.anthropic.com" {
		t.Errorf("default upstream URL = %q", Default.UpstreamAnthropicURL)
	}
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Port != Default.Port {
		t.Errorf("port = %d, want default %d", cfg.Port, Default.Port)
	}
}

func TestLoad_CustomConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, ".pgmemory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	content := `port: 9999
postgres_url: "postgres://test:pass@localhost:5432/testdb"
embedding_dim: 256
retrieval_top_k: 10
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Port != 9999 {
		t.Errorf("port = %d, want 9999", cfg.Port)
	}
	if cfg.PostgresURL != "postgres://test:pass@localhost:5432/testdb" {
		t.Errorf("postgres_url = %q", cfg.PostgresURL)
	}
	if cfg.EmbeddingDim != 256 {
		t.Errorf("embedding_dim = %d, want 256", cfg.EmbeddingDim)
	}
	if cfg.RetrievalTopK != 10 {
		t.Errorf("retrieval_top_k = %d, want 10", cfg.RetrievalTopK)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, ".pgmemory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("{{invalid yaml"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestWriteDefault_CreatesFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := WriteDefault(); err != nil {
		t.Fatalf("WriteDefault() error: %v", err)
	}

	path := filepath.Join(tmp, ".pgmemory", "config.yaml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("config file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestWriteDefault_NoOverwrite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, ".pgmemory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	existing := "port: 1234\n"
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	if err := WriteDefault(); err != nil {
		t.Fatalf("WriteDefault() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Error("WriteDefault overwrote existing config file")
	}
}

func TestEnsureDir_CreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}

	dir := filepath.Join(tmp, ".pgmemory")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected a directory")
	}
}

func TestLoad_ExpandsHomePath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, ".pgmemory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	content := `model_path: "~/.pgmemory/models/test.gguf"
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	expected := filepath.Join(tmp, ".pgmemory/models/test.gguf")
	if cfg.ModelPath != expected {
		t.Errorf("model_path = %q, want %q", cfg.ModelPath, expected)
	}
}

// --- Mode tests ---

func TestDefaultModeIsProxy(t *testing.T) {
	if Default.Mode != ModeProxy {
		t.Errorf("default mode = %q, want %q", Default.Mode, ModeProxy)
	}
}

func TestValidMode(t *testing.T) {
	valid := []string{ModeProxy, ModeMCP, ModeMCPReadOnly}
	for _, m := range valid {
		if !ValidMode(m) {
			t.Errorf("ValidMode(%q) = false, want true", m)
		}
	}

	invalid := []string{"", "invalid", "read-only", "PROXY", "MCP"}
	for _, m := range invalid {
		if ValidMode(m) {
			t.Errorf("ValidMode(%q) = true, want false", m)
		}
	}
}

func TestProxyWriteEnabled(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"", true}, // empty defaults to proxy-like behaviour
		{ModeProxy, true},
		{ModeMCP, false},
		{ModeMCPReadOnly, false},
	}
	for _, tt := range tests {
		cfg := &Config{Mode: tt.mode}
		if got := cfg.ProxyWriteEnabled(); got != tt.want {
			t.Errorf("ProxyWriteEnabled() with mode %q = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestMCPReadOnly(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"", false},
		{ModeProxy, false},
		{ModeMCP, false},
		{ModeMCPReadOnly, true},
	}
	for _, tt := range tests {
		cfg := &Config{Mode: tt.mode}
		if got := cfg.MCPReadOnly(); got != tt.want {
			t.Errorf("MCPReadOnly() with mode %q = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestSetMode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// SetMode should work even without an existing config file.
	if err := SetMode(ModeMCP); err != nil {
		t.Fatalf("SetMode(%q) error: %v", ModeMCP, err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Mode != ModeMCP {
		t.Errorf("mode = %q, want %q", cfg.Mode, ModeMCP)
	}

	// Switch to read-only, preserving other fields.
	if err := SetMode(ModeMCPReadOnly); err != nil {
		t.Fatalf("SetMode(%q) error: %v", ModeMCPReadOnly, err)
	}
	cfg, _ = Load()
	if cfg.Mode != ModeMCPReadOnly {
		t.Errorf("mode = %q, want %q", cfg.Mode, ModeMCPReadOnly)
	}
}

func TestSetMode_InvalidMode(t *testing.T) {
	if err := SetMode("bogus"); err == nil {
		t.Error("expected error for invalid mode, got nil")
	}
}

func TestSetMode_PreservesOtherFields(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, ".pgmemory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	content := `port: 8888
postgres_url: "postgres://custom:5432/pgmemory"
mode: proxy
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if err := SetMode(ModeMCPReadOnly); err != nil {
		t.Fatalf("SetMode error: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Port != 8888 {
		t.Errorf("port = %d, want 8888 (SetMode should preserve other fields)", cfg.Port)
	}
	if cfg.PostgresURL != "postgres://custom:5432/pgmemory" {
		t.Errorf("postgres_url = %q (SetMode should preserve other fields)", cfg.PostgresURL)
	}
	if cfg.Mode != ModeMCPReadOnly {
		t.Errorf("mode = %q, want %q", cfg.Mode, ModeMCPReadOnly)
	}
}

func TestLoad_ModeField(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, ".pgmemory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	content := `mode: mcp-readonly
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Mode != ModeMCPReadOnly {
		t.Errorf("mode = %q, want %q", cfg.Mode, ModeMCPReadOnly)
	}
}
