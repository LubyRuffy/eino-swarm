package engine

import (
	"context"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/mcpclient"
)

// InspectMCP tests a server with an ephemeral session anchored at the data
// directory. It neither mounts tools on a running turn nor changes its snapshot.
func (e *Engine) InspectMCP(ctx context.Context, server config.MCPServer) (mcpclient.Catalog, error) {
	catalog, err := e.mcp.Inspect(ctx, server, e.cfg.DataDir())
	if err == nil {
		e.mcp.RememberCatalog(server, catalog)
	}
	return catalog, err
}
