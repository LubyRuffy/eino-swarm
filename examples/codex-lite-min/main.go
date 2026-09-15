// Command codex-lite-min is the smallest complete Codex-CLI-style runner on
// github.com/LubyRuffy/eino-swarm. The whole product surface: a Registry, one
// Run call, one callback. Ctrl+C cancels the entire swarm.
//
//	OPENAI_BASE_URL=http://your-endpoint/v1 OPENAI_MODEL=your-model \
//	OPENAI_API_KEY=*** go run ./examples/codex-lite-min -task "..."
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/components/model"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
)

func main() {
	task := flag.String("task", "Summarize README.md and list open questions.", "goal")
	flag.Parse()
	ctx := context.Background()

	shared, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		BaseURL: envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
		Model:   envOr("OPENAI_MODEL", "gpt-4o-mini"),
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		panic(err)
	}

	reg := swarm.NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel { return shared }

	final, err := reg.Run(ctx, *task, func(n swarm.Notification) {
		switch n.Kind {
		case swarm.NotifyAgentMessage:
			fmt.Printf("[%s] %s\n", n.AgentID, oneLine(n.Text))
		case swarm.NotifyError:
			fmt.Fprintf(os.Stderr, "error: %v\n", n.Err)
		}
	})
	if err != nil {
		os.Exit(1)
	}
	fmt.Println("\nFINAL:", oneLine(final))
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func oneLine(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
