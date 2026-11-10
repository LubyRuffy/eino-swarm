package backup

import (
	"net/url"
	"os"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"gopkg.in/yaml.v3"
)

func redactSecrets(cfg *config.Config) {
	for i := range cfg.Models.Providers {
		cfg.Models.Providers[i].APIKey = ""
	}
	for i := range cfg.MCP.Servers {
		cfg.MCP.Servers[i].Env = nil
		cfg.MCP.Servers[i].Headers = nil
		cfg.MCP.Servers[i].URL = stripUserinfo(cfg.MCP.Servers[i].URL)
	}
	cfg.Tools.Proxy.HTTP = stripUserinfo(cfg.Tools.Proxy.HTTP)
	cfg.Tools.Proxy.HTTPS = stripUserinfo(cfg.Tools.Proxy.HTTPS)
}

func stripUserinfo(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func marshalConfig(cfg *config.Config, omit bool) ([]byte, error) {
	if err := failNext(); err != nil {
		return nil, err
	}
	next := cfg.Snapshot()
	if omit {
		redactSecrets(next)
	}
	raw, err := yaml.Marshal(next)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// overlayKeptSecrets copies credentials from the snapshot taken before the
// files were replaced. A backup that left secrets out must not blank a key
// the destination already had. Empty snapshot values stay empty so an
// environment seed applied by Load is not wiped and then saved.
func overlayKeptSecrets(dst, src *config.Config) {
	keys := make(map[string]string, len(src.Models.Providers))
	for _, p := range src.Models.Providers {
		keys[p.ID] = p.APIKey
	}
	for i := range dst.Models.Providers {
		if key := strings.TrimSpace(keys[dst.Models.Providers[i].ID]); key != "" {
			dst.Models.Providers[i].APIKey = key
		}
	}
	envs := make(map[string]map[string]string, len(src.MCP.Servers))
	headers := make(map[string]map[string]string, len(src.MCP.Servers))
	for _, server := range src.MCP.Servers {
		envs[server.ID] = cloneMap(server.Env)
		headers[server.ID] = cloneMap(server.Headers)
	}
	for i := range dst.MCP.Servers {
		id := dst.MCP.Servers[i].ID
		if len(envs[id]) > 0 {
			dst.MCP.Servers[i].Env = envs[id]
		}
		if len(headers[id]) > 0 {
			dst.MCP.Servers[i].Headers = headers[id]
		}
	}
	if strings.TrimSpace(src.Tools.Proxy.HTTP) != "" {
		dst.Tools.Proxy.HTTP = src.Tools.Proxy.HTTP
	}
	if strings.TrimSpace(src.Tools.Proxy.HTTPS) != "" {
		dst.Tools.Proxy.HTTPS = src.Tools.Proxy.HTTPS
	}
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func readConfigFile(path string) (config.Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return config.Config{}, err
	}
	var cfg config.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return config.Config{}, invalidf("config snapshot")
	}
	return cfg, nil
}
