package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ServerConfig describes how to launch a language server.
type ServerConfig struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
	Enabled *bool    `toml:"enabled"` // nil = enabled
}

// fileConfig mirrors ~/.config/codetree/config.toml:
//
//	[lsp.python]
//	command = "basedpyright-langserver"
//	args = ["--stdio"]
type fileConfig struct {
	LSP map[string]ServerConfig `toml:"lsp"`
}

// defaultServers are probed in order when no config entry exists. A command
// containing a path separator is checked as a file path (leading ~/ expands
// to the user home) — this covers servers installed off-PATH like jdtls.
var defaultServers = map[string][]ServerConfig{
	"python": {
		{Command: "basedpyright-langserver", Args: []string{"--stdio"}},
		{Command: "pyright-langserver", Args: []string{"--stdio"}},
	},
	"go":  {{Command: "gopls"}},
	"cpp": {{Command: "clangd"}},
	"java": {
		{Command: "jdtls"},
		{Command: "~/.local/share/jdtls/bin/jdtls"},                     // standalone dist
		{Command: "~/.local/share/nvim/mason/packages/jdtls/bin/jdtls"}, // nvim mason
	},
	"rust": {{Command: "rust-analyzer"}},
	"typescript": {
		{Command: "typescript-language-server", Args: []string{"--stdio"}},
		{Command: "~/.local/share/nvim/mason/packages/typescript-language-server/node_modules/.bin/typescript-language-server", Args: []string{"--stdio"}},
	},
	"javascript": {
		{Command: "typescript-language-server", Args: []string{"--stdio"}},
		{Command: "~/.local/share/nvim/mason/packages/typescript-language-server/node_modules/.bin/typescript-language-server", Args: []string{"--stdio"}},
	},
}

// expandCommand resolves a leading ~/ to the user home; bare names are
// returned unchanged (PATH lookup happens at exec time).
func expandCommand(cmd string) string {
	if strings.HasPrefix(cmd, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, cmd[2:])
		}
	}
	return cmd
}

// installed reports whether cmd is runnable: in PATH for bare names, or an
// executable file for paths containing a separator.
func installed(cmd string) bool {
	if strings.ContainsRune(cmd, '/') {
		st, err := os.Stat(expandCommand(cmd))
		return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
	}
	_, err := exec.LookPath(cmd)
	return err == nil
}

// ConfigPath is ~/.config/codetree/config.toml (honors XDG_CONFIG_HOME).
func ConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "codetree", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "codetree", "config.toml")
}

func loadConfig(path string) fileConfig {
	var cfg fileConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	_ = toml.Unmarshal(data, &cfg) // malformed config → fall back to defaults
	return cfg
}

// ResolveServer picks the server command for lang. Precedence: config entry
// (enabled=false disables the layer entirely) → first default found in PATH.
// found=false means the LSP layer stays silently absent.
func ResolveServer(lang string) (cfg ServerConfig, found bool) {
	return resolveServer(lang, loadConfig(ConfigPath()))
}

func resolveServer(lang string, cfg fileConfig) (ServerConfig, bool) {
	if entry, ok := cfg.LSP[lang]; ok {
		if entry.Enabled != nil && !*entry.Enabled {
			return ServerConfig{}, false
		}
		if entry.Command != "" {
			if installed(entry.Command) {
				entry.Command = expandCommand(entry.Command)
				return entry, true
			}
			return ServerConfig{}, false // configured but not installed
		}
	}
	for _, d := range defaultServers[lang] {
		if installed(d.Command) {
			d.Command = expandCommand(d.Command)
			return d, true
		}
	}
	return ServerConfig{}, false
}
