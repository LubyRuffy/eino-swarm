package mcpclient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func catalogPath(dir, fingerprint string) string {
	return filepath.Join(dir, "mcp-catalogs", fingerprint+".json")
}

// SaveCatalog writes the tool catalog for this server's connection fingerprint.
// The file holds names, descriptions and schemas only. Environment variables
// and headers stay in the fingerprint and never in the body.
func SaveCatalog(dir string, server config.MCPServer, catalog Catalog) error {
	if dir == "" {
		return nil
	}
	body, err := json.Marshal(Catalog{ServerID: catalog.ServerID, Tools: catalog.Tools, Skipped: catalog.Skipped})
	if err != nil {
		return err
	}
	path := catalogPath(dir, fingerprint(server))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// LoadCatalog reads a catalog saved for the server's current fingerprint.
// A missing or unreadable file is a cache miss, not a successful empty catalog.
func LoadCatalog(dir string, server config.MCPServer) (Catalog, bool) {
	if dir == "" {
		return Catalog{}, false
	}
	body, err := os.ReadFile(catalogPath(dir, fingerprint(server)))
	if err != nil {
		return Catalog{}, false
	}
	var catalog Catalog
	if json.Unmarshal(body, &catalog) != nil || catalog.ServerID != server.ID {
		return Catalog{}, false
	}
	if catalog.Tools == nil {
		catalog.Tools = []ToolDescriptor{}
	}
	return catalog, true
}

func (m *Manager) SetCatalogDir(dir string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.catalogDir = dir
	m.mu.Unlock()
}

func (m *Manager) catalogDirectory() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.catalogDir
}

func (m *Manager) RememberCatalog(server config.MCPServer, catalog Catalog) {
	if m == nil {
		return
	}
	_ = SaveCatalog(m.catalogDirectory(), server, catalog)
}

func filterToolQuery(tools []ToolDescriptor, query string) []ToolDescriptor {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return tools
	}
	matched := make([]ToolDescriptor, 0, len(tools))
	for _, tool := range tools {
		if strings.Contains(strings.ToLower(tool.Name), query) ||
			strings.Contains(strings.ToLower(tool.OriginalName), query) ||
			strings.Contains(strings.ToLower(tool.Description), query) {
			matched = append(matched, tool)
		}
	}
	return matched
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	const maxRunes = 80
	runes := []rune(text)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return text
}

// ToolIndex is the name and one-line description list baked into a progressive
// proxy. A long catalog is truncated and tells the model to query further.
func ToolIndex(tools []ToolDescriptor) string {
	const limit = 1500
	if len(tools) == 0 {
		return ""
	}
	var b strings.Builder
	for i, tool := range tools {
		name := tool.OriginalName
		if name == "" {
			name = tool.Name
		}
		piece := name
		if line := firstLine(tool.Description); line != "" {
			piece += " — " + line
		}
		next := piece
		if b.Len() > 0 {
			next = "; " + piece
		}
		if b.Len()+len(next) > limit {
			fmt.Fprintf(&b, "; %d more, query list_tools to continue", len(tools)-i)
			break
		}
		b.WriteString(next)
	}
	return b.String()
}
