package lsp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"codetree/core"
)

// Client is a minimal LSP client over stdio. One client per language pass.
type Client struct {
	conn    jsonrpc2.Conn
	srv     protocol.Server
	cmd     *exec.Cmd // nil for injected transports (tests)
	root    string    // absolute project root
	langKey string    // "python"/"go"/... — fallback languageID
	opened  map[string]bool
}

// langIDByExt maps file extensions to LSP language identifiers.
var langIDByExt = map[string]string{
	".py": "python", ".pyi": "python",
	".go": "go",
	".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp",
	".h": "cpp", ".hpp": "cpp", ".hxx": "cpp",
	".java": "java",
	".rs":   "rust",
	".ts":   "typescript", ".tsx": "typescriptreact",
	".js": "javascript", ".jsx": "javascriptreact",
}

func (c *Client) langID(rel string) string {
	if id, ok := langIDByExt[strings.ToLower(filepath.Ext(rel))]; ok {
		return id
	}
	return c.langKey
}

// stdioRWC joins the server's stdout (read) and stdin (write).
type stdioRWC struct {
	io.Reader
	io.Writer
	closer func() error
}

func (s *stdioRWC) Close() error { return s.closer() }

// Start launches the server process and performs the LSP handshake.
func Start(ctx context.Context, cfg ServerConfig, root, langKey string) (*Client, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil // server logs discarded
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	rwc := &stdioRWC{Reader: stdout, Writer: stdin, closer: func() error {
		stdin.Close()
		return cmd.Process.Kill()
	}}
	c, err := newClient(ctx, jsonrpc2.NewStream(rwc), root)
	if err != nil {
		rwc.Close()
		return nil, err
	}
	c.cmd = cmd
	c.langKey = langKey
	return c, nil
}

// newClient wraps a message stream with the handshake. Test seam: fake
// servers inject an in-memory channel stream.
func newClient(ctx context.Context, stream jsonrpc2.Stream, root string) (*Client, error) {
	// NewClient installs the union-aware codec and a null client handler.
	_, conn, srv := protocol.NewClient(ctx, protocol.UnimplementedClient{}, stream)
	c := &Client{conn: conn, srv: srv, root: root, opened: map[string]bool{}}

	rootURI := uri.File(root)
	pid := int32(os.Getpid())
	if _, err := c.srv.Initialize(ctx, &protocol.InitializeParams{
		ProcessID:    &pid,
		RootURI:      &rootURI,
		Capabilities: protocol.ClientCapabilities{},
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
			WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{{URI: rootURI, Name: filepath.Base(root)}}),
		},
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if err := c.srv.Initialized(ctx, &protocol.InitializedParams{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("initialized: %w", err)
	}
	return c, nil
}

// Shutdown terminates the session and kills the server process.
func (c *Client) Shutdown(ctx context.Context) {
	_ = c.srv.Shutdown(ctx)
	_ = c.srv.Exit(ctx)
	_ = c.conn.Close()
	if c.cmd != nil {
		_ = c.cmd.Wait() // process killed via transport close; reap it
	}
}

// openDoc sends didOpen with the on-disk content (once per file).
func (c *Client) openDoc(ctx context.Context, rel string) error {
	if c.opened[rel] {
		return nil
	}
	src, err := os.ReadFile(filepath.Join(c.root, rel))
	if err != nil {
		return err
	}
	c.opened[rel] = true
	return c.srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        uri.File(filepath.Join(c.root, rel)),
			LanguageID: protocol.LanguageKind(c.langID(rel)),
			Version:    1,
			Text:       string(src),
		},
	})
}

func (c *Client) docPos(rel string, pos core.Pos) protocol.TextDocumentPositionParams {
	return protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.File(filepath.Join(c.root, rel))},
		Position:     protocol.Position{Line: uint32(pos.Line - 1), Character: uint32(pos.Col)},
	}
}

// Definition resolves the token at rel:pos to its definition locations.
func (c *Client) Definition(ctx context.Context, rel string, pos core.Pos) ([]protocol.Location, error) {
	if err := c.openDoc(ctx, rel); err != nil {
		return nil, err
	}
	res, err := c.srv.Definition(ctx, &protocol.DefinitionParams{
		TextDocumentPositionParams: c.docPos(rel, pos),
	})
	if err != nil {
		return nil, err
	}
	return flattenDefinitionResult(res), nil
}

// Implementation resolves an interface token to its implementing types (Go).
func (c *Client) Implementation(ctx context.Context, rel string, pos core.Pos) ([]protocol.Location, error) {
	if err := c.openDoc(ctx, rel); err != nil {
		return nil, err
	}
	res, err := c.srv.Implementation(ctx, &protocol.ImplementationParams{
		TextDocumentPositionParams: c.docPos(rel, pos),
	})
	if err != nil {
		return nil, err
	}
	return flattenDefinitionResult(res), nil
}

func flattenDefinitionResult(res protocol.DefinitionResult) []protocol.Location {
	switch r := res.(type) {
	case protocol.LocationSlice:
		return []protocol.Location(r)
	case *protocol.Location:
		if r != nil {
			return []protocol.Location{*r}
		}
	case protocol.DefinitionLinkSlice:
		var out []protocol.Location
		for _, l := range r {
			out = append(out, protocol.Location{URI: l.TargetURI, Range: l.TargetRange})
		}
		return out
	}
	return nil
}

// hoverText returns the raw hover text at rel:pos.
func (c *Client) hoverText(ctx context.Context, rel string, pos core.Pos) (string, error) {
	if err := c.openDoc(ctx, rel); err != nil {
		return "", err
	}
	h, err := c.srv.Hover(ctx, &protocol.HoverParams{
		TextDocumentPositionParams: c.docPos(rel, pos),
	})
	if err != nil || h == nil {
		return "", err
	}
	switch ct := h.Contents.(type) {
	case *protocol.MarkupContent:
		return ct.Value, nil
	case protocol.String:
		return string(ct), nil
	case *protocol.MarkedStringWithLanguage:
		return ct.Value, nil
	}
	return "", nil
}

var hoverTypeRe = regexp.MustCompile(`:\s*([^ \n]+)`) // "(variable) x: Dog" → "Dog"

// HoverType resolves the inferred type name of the token at rel:pos.
func (c *Client) HoverType(ctx context.Context, rel string, pos core.Pos) (string, error) {
	text, err := c.hoverText(ctx, rel, pos)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.Trim(line, " `")
		if m := hoverTypeRe.FindStringSubmatch(line); m != nil {
			return strings.Trim(m[1], " `"), nil
		}
	}
	return "", nil
}

// DocumentSymbols returns hierarchical symbols of one file.
func (c *Client) DocumentSymbols(ctx context.Context, rel string) (protocol.DocumentSymbolSlice, error) {
	if err := c.openDoc(ctx, rel); err != nil {
		return nil, err
	}
	res, err := c.srv.DocumentSymbol(ctx, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.File(filepath.Join(c.root, rel))},
	})
	if err != nil {
		return nil, err
	}
	switch r := res.(type) {
	case protocol.DocumentSymbolSlice:
		return protocol.DocumentSymbolSlice(r), nil
	}
	return nil, nil
}

// URIToRel converts a file URI back to a project-relative path.
func URIToRel(root string, u uri.URI) string {
	s := strings.TrimPrefix(string(u), "file://")
	if rel, err := filepath.Rel(root, s); err == nil {
		return filepath.ToSlash(rel)
	}
	return s
}
