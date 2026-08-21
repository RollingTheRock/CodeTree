package lsp

import (
	"os"
	"os/exec"
	"path/filepath"

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

// defaultServers are probed in order when no config entry exists.
var defaultServers = map[string][]ServerConfig{
	"python": {
		{Command: "basedpyright-langserver", Args: []string{"--stdio"}},
		{Command: "pyright-langserver", Args: []string{"--stdio"}},
	},
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
			if _, err := exec.LookPath(entry.Command); err == nil {
				return entry, true
			}
			return ServerConfig{}, false // configured but not installed
		}
	}
	for _, d := range defaultServers[lang] {
		if _, err := exec.LookPath(d.Command); err == nil {
			return d, true
		}
	}
	return ServerConfig{}, false
}
