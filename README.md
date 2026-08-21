# codetree (`ct`)

A code structure map for your terminal. It scans your project and draws how classes relate — inheritance, interface implementation, composition — as a UML class diagram you can walk around with `hjkl`. Know the shape of a hundred classes before you write a line, without opening a heavy IDE.

[中文文档](README.zh-CN.md)

<p align="center"><img src="demo.gif" alt="codetree demo" width="800"/></p>

## Features

- Class diagrams rendered on a character canvas: extends (solid), implements (dashed), composition (◆)
- Scope to one file or a few marked files; `A` for the whole project
- `enter` focuses a class's neighborhood; `+`/`-` depth, `b` siblings
- `m` expands member compartments — typed fields, method signatures
- Live reload on save; `o` opens the class in `$EDITOR` at its line
- Optional LSP layer sharpens the graph asynchronously (disambiguates same-name bases, infers Python field types, finds Go interface implementations) — pure static stays just as fast without it
- Python / Go / Java / C++ / Rust / TypeScript / JavaScript

## Install

```sh
go install github.com/RollingTheRock/CodeTree/cmd/ct@latest
```

Or grab a single binary from [Releases](https://github.com/RollingTheRock/CodeTree/releases) (Linux / macOS).

## Usage

```sh
ct                   # TUI: space to mark files, enter for the diagram
ct -f diagram .      # print the class diagram
ct -f mermaid .      # Mermaid classDiagram
ct -f json .         # structured JSON for scripts
ct -lsp -f diagram . # apply LSP corrections first, diff on stderr
```

Keys: `hjkl` move · `space` mark · `enter` focus · `esc` back · `m` members · `c` composition edges · `/` filter · `?` help · `q` quit

### Focus on one class's neighborhood

<p align="center"><img src="docs/focus.gif" alt="focus mode" width="800"/></p>

### CLI output, and what the LSP layer corrects

<p align="center"><img src="docs/cli.gif" alt="cli and lsp diff" width="800"/></p>

## LSP (optional)

No server is bundled. If one is on your PATH (or in a well-known spot like `~/.local/share/jdtls` or nvim's mason dir), codetree uses it: pyright/basedpyright, gopls, clangd, jdtls, rust-analyzer, typescript-language-server. To switch or disable, edit `~/.config/codetree/config.toml`:

```toml
[lsp.python]
command = "basedpyright-langserver"
args = ["--stdio"]
# enabled = false
```

## Development

```sh
CGO_ENABLED=1 go build ./...   # tree-sitter needs cgo
go test ./...
```

GIFs recorded with [vhs](https://github.com/charmbracelet/vhs) (`vhs demo.tape`).
