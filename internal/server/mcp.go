package server

import (
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/mcpclient"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

var errMCPExists = errors.New("MCP server ID already exists")

type mcpServerView struct {
	config.MCPServer
	EnvKeys    []string `json:"env_keys"`
	HeaderKeys []string `json:"header_keys"`
}

func mcpView(s config.MCPServer) mcpServerView {
	s.Args = slices.Clone(s.Args)
	if s.Args == nil {
		s.Args = []string{}
	}
	view := mcpServerView{MCPServer: s, EnvKeys: slices.Sorted(maps.Keys(s.Env)), HeaderKeys: slices.Sorted(maps.Keys(s.Headers))}
	if view.EnvKeys == nil {
		view.EnvKeys = []string{}
	}
	if view.HeaderKeys == nil {
		view.HeaderKeys = []string{}
	}
	view.Env, view.Headers = nil, nil
	return view
}

func (s *Server) getMCP(c *gin.Context) {
	servers := []mcpServerView{}
	for _, server := range s.engine.Config().Snapshot().MCP.Servers {
		servers = append(servers, mcpView(server))
	}
	c.JSON(http.StatusOK, gin.H{"servers": servers})
}

type putMCPRequest struct {
	config.MCPServer
	Env     *map[string]string `json:"env"`
	Headers *map[string]string `json:"headers"`
}

func (s *Server) putMCP(c *gin.Context) {
	var req putMCPRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid MCP configuration", "code": "invalid_mcp"})
		return
	}
	server := req.MCPServer
	server.ID = c.Param("id")
	if req.Env != nil {
		server.Env = maps.Clone(*req.Env)
	}
	if req.Headers != nil {
		server.Headers = maps.Clone(*req.Headers)
	}
	if err := server.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "invalid_mcp"})
		return
	}
	err := s.engine.UpdateConfig(func(cfg *config.Config) error {
		index := slices.IndexFunc(cfg.MCP.Servers, func(existing config.MCPServer) bool { return existing.ID == server.ID })
		if index >= 0 && c.GetHeader("If-None-Match") == "*" {
			return errMCPExists
		}
		if index >= 0 {
			if req.Env == nil {
				server.Env = cfg.MCP.Servers[index].Env
			}
			if req.Headers == nil {
				server.Headers = cfg.MCP.Servers[index].Headers
			}
		}
		if index < 0 {
			cfg.MCP.Servers = append(cfg.MCP.Servers, server)
		} else {
			cfg.MCP.Servers[index] = server
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errMCPExists) {
			c.JSON(http.StatusPreconditionFailed, gin.H{"error": errMCPExists.Error(), "code": "mcp_exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save MCP configuration", "code": "mcp_save_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"server": mcpView(server)})
}

func (s *Server) importMCP(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid MCP configuration", "code": "invalid_mcp"})
		return
	}
	err = s.engine.UpdateConfig(func(cfg *config.Config) error {
		merged, err := config.ApplyMCPJSON(cfg.MCP.Servers, raw)
		if err != nil {
			return err
		}
		cfg.MCP.Servers = merged
		return nil
	})
	if err != nil {
		var invalid *config.MCPJSONError
		if errors.As(err, &invalid) {
			c.JSON(http.StatusBadRequest, gin.H{"error": invalid.Error(), "code": "invalid_mcp"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save MCP configuration", "code": "mcp_save_failed"})
		return
	}
	servers := []mcpServerView{}
	for _, server := range s.engine.Config().Snapshot().MCP.Servers {
		servers = append(servers, mcpView(server))
	}
	c.JSON(http.StatusOK, gin.H{"servers": servers})
}

func (s *Server) deleteMCP(c *gin.Context) {
	err := s.engine.UpdateConfig(func(cfg *config.Config) error {
		index := slices.IndexFunc(cfg.MCP.Servers, func(server config.MCPServer) bool { return server.ID == c.Param("id") })
		if index < 0 {
			return store.ErrNotFound
		}
		cfg.MCP.Servers = slices.Delete(cfg.MCP.Servers, index, index+1)
		return nil
	})
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func (s *Server) inspectMCP(c *gin.Context) {
	for _, server := range s.engine.Config().Snapshot().MCP.Servers {
		if server.ID != c.Param("id") {
			continue
		}
		catalog, err := s.engine.InspectMCP(c.Request.Context(), server)
		if err != nil {
			if c.Request.Context().Err() != nil {
				return
			}
			reason := "connection_failed"
			var failure *mcpclient.Error
			if errors.As(err, &failure) {
				reason = failure.Code
			}
			c.JSON(http.StatusBadGateway, gin.H{"error": "MCP connection or tool discovery failed; check the server configuration", "code": "mcp_connection_failed", "reason": reason})
			return
		}
		c.JSON(http.StatusOK, catalog)
		return
	}
	s.fail(c, store.ErrNotFound)
}
