// Command zwai is the single entry point for the swarm UIs:
//
//	zwai tui     terminal UI (bubbletea, two panes)
//	zwai web     browser UI (SSE live events) at http://localhost:8787
//	zwai desktop native window wrapping the webui
//
// All frontends consume the same swarm.Notification stream; the core never
// changes per UI.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/tui"
	"github.com/LubyRuffy/eino-swarm/internal/webui"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

func newRegistry(ctx context.Context) *swarm.Registry {
	shared, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		BaseURL: envOr("OPENAI_BASE_URL", "https://ai.fofa.info:2440/v1"),
		APIKey:  firstEnv("FOFA_AI_KEY", "OPENAI_API_KEY"),
		Model:   envOr("OPENAI_MODEL", "deepseek-v4-flash-0731"),
		Timeout: 5 * time.Minute,
	})
	if err != nil {
		panic(err)
	}
	reg := swarm.NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel { return shared }
	return reg
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "tui":
		fs := flag.NewFlagSet("tui", flag.ExitOnError)
		task := fs.String("task", "研究一下奥巴马出生那一年发生了什么大事", "goal")
		_ = fs.Parse(os.Args[2:])
		tui.Run(ctx, newRegistry(ctx), *task)
	case "web":
		fs := flag.NewFlagSet("web", flag.ExitOnError)
		addr := fs.String("addr", envOr("ZWAI_ADDR", ":8787"), "listen address")
		task := fs.String("task", "", "task to auto-start (empty = start idle)")
		open := fs.Bool("open", true, "open the browser automatically")
		_ = fs.Parse(os.Args[2:])
		webui.Serve(ctx, newRegistry(ctx), *addr, *task, *open)
	case "desktop":
		fs := flag.NewFlagSet("desktop", flag.ExitOnError)
		task := fs.String("task", "", "task to auto-start")
		_ = fs.Parse(os.Args[2:])
		webui.Desktop(ctx, newRegistry(ctx), *task)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(`
zwai — Codex-style agent swarm UIs

usage:
  zwai tui     [--task "...]              terminal UI (bubbletea)
  zwai web     [--addr :8787] [--task ..] browser UI (SSE live events)
  zwai desktop [--task ..]               native window wrapping webui
`))
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
