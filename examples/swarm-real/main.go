// Command swarm-real runs a REAL LLM multi-agent task on
// github.com/LubyRuffy/eino-swarm: a manager (Kimi K2.6) decomposes a research
// goal into four independent questions, spawns four sub-agents in ONE turn
// (parallel), each worker answers with real model calls, and the manager
// synthesizes the results.
//
// Required env: FOFA_AI_KEY (aigateway at https://ai.fofa.info:2440)
// Run:
//
//	FOFA_AI_KEY=... go run ./examples/swarm-real
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

const managerPrompt = `You coordinate a worker swarm AND answer directly when
possible — exactly like Codex CLI.

Decision rule, applied BEFORE anything else:
- Greetings ("hi"), small talk, simple factual questions, single-step lookups,
  or any request you can answer well in one direct reply: JUST ANSWER.
  Do NOT spawn anything. Spawning for trivial input is a bug.
- Only decompose when the task genuinely needs multiple independent work
  streams (parallel research angles, separate subtopics, heavy multi-step
  investigation). Then split into 2-4 non-overlapping questions and call
  spawn_agent for each IN ONE assistant message (they run in parallel).
- After spawning: wait_agents on all ids, then synthesize a compact answer
  (max 200 words) from the workers' results. Do not answer the parts yourself.
- If a worker fails, resume_agent that id with a tighter task. Do not spawn a
  second worker with the same role.
- If more work depends on a finished worker, resume_agent that id instead of
  spawning a blank one. fork_context copies this conversation, not a worker's.

When you decide no worker is needed, just reply directly — that is the
correct behavior, not laziness.`

const workerPrompt = `You are a focused research agent. Answer your assigned
question in at most 3 sentences of concrete technical facts. No preamble.`

func main() {
	start := time.Now()
	ctx := context.Background()

	if os.Getenv("FOFA_AI_KEY") == "" {
		fmt.Fprintln(os.Stderr, "set FOFA_AI_KEY")
		os.Exit(2)
	}
	// one shared client for the whole swarm
	shared, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		BaseURL: "https://ai.fofa.info:2440/v1",
		APIKey:  os.Getenv("FOFA_AI_KEY"),
		Model:   envOr("OPENAI_MODEL", "deepseek-v4-flash-0731"),
		Timeout: 3 * time.Minute,
	})
	if err != nil {
		panic(err)
	}

	var mu sync.Mutex
	spawned := map[string]time.Time{} // agentID -> spawn wall time
	finished := map[string]time.Time{}
	msgCount := 0

	reg := swarm.NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel { return shared }

	// task decides the path: short/simple => direct answer, 0 spawns;
	// complex research => fan-out. Try both:
	//   go run ./examples/swarm-real -task "hi"
	//   go run ./examples/swarm-real (default research task)
	taskFlag := flag.String("task", "What are the key Go concurrency patterns a backend team should master, and why?", "goal for the swarm")
	flag.Parse()

	final, err := reg.Run(ctx, *taskFlag, func(n swarm.Notification) {
		mu.Lock()
		defer mu.Unlock()
		ts := time.Since(start).Seconds()
		switch n.Kind {
		case swarm.NotifySpawned:
			spawned[n.AgentID] = time.Now()
			fmt.Printf("\033[2m[%6.2fs]\033[0m \033[32mSPAWNED\033[0m  \033[36m%s\033[0m\n", ts, n.AgentID)
		case swarm.NotifyFinished:
			finished[n.AgentID] = time.Now()
			status := "ok"
			if n.Err != nil {
				status = "err: " + n.Err.Error()
			}
			elapsed := 0.0
			if s, ok := spawned[n.AgentID]; ok {
				elapsed = finished[n.AgentID].Sub(s).Seconds()
			}
			if n.Err != nil {
				fmt.Printf("\033[2m[%6.2fs]\033[0m \033[31mFINISHED\033[0m  %-24s ran=%.2fs \033[31m%s\033[0m\n", ts, n.AgentID, elapsed, oneLine(status, 60))
			} else {
				fmt.Printf("\033[2m[%6.2fs]\033[0m \033[32mFINISHED\033[0m  %-14s ran=%.2fs\n", ts, n.AgentID, elapsed)
			}
		case swarm.NotifyAgentMessage:
			if n.AgentID == swarm.DefaultManagerID {
				msgCount++
				fmt.Printf("[%6.2fs] MANAGER   %s\n", ts, oneLine(n.Text, 100))
			} else {
				fmt.Printf("[%6.2fs] worker %-9s %s\n", ts, n.AgentID, oneLine(n.Text, 80))
			}
		case swarm.NotifyTurn:
			fmt.Printf("[%6.2fs]   ── %-14s %s\n", ts, n.AgentID, n.Text)
		case swarm.NotifyReasoningDelta:
			// live thinking: single refreshed line per agent (dim/cyan),
			// tail of the accumulated reasoning — collapses the token spam.
			fmt.Printf("\033[2m[%6.2fs] 💭 %-16s %s\033[K\033[0m\r", ts, n.AgentID, lastRunes(n.Text, 52))
		case swarm.NotifyDelta:
			// live answer: refreshed tail, cyan agent tag
			fmt.Printf("\033[36m[%6.2fs] ⋯ %-13s\033[0m %s\033[K\r", ts, n.AgentID, lastLine(n.Text))
		case swarm.NotifyToolCall:
			fmt.Printf("[%6.2fs]   → %-14s CALL %s\n", ts, n.AgentID, oneLine(n.Text, 90))
		case swarm.NotifyToolResult:
			fmt.Printf("[%6.2fs]   ← %-14s %s\n", ts, n.AgentID, oneLine(n.Text, 90))
		case swarm.NotifyDone:
			fmt.Printf("[%6.2fs] DONE\n", ts)
		case swarm.NotifyError:
			fmt.Printf("[%6.2fs] ERROR %v\n", ts, n.Err)
		}
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "run failed:", err)
		os.Exit(1)
	}

	// concurrency verdict from spawn/finish wall times
	mu.Lock()
	defer mu.Unlock()
	var maxConc int
	for id, s := range spawned {
		f, ok := finished[id]
		if !ok {
			continue
		}
		c := 0
		for id2, s2 := range spawned {
			f2, ok2 := finished[id2]
			if !ok2 {
				continue
			}
			if !s2.After(f) || !f.Before(s2) || id == id2 {
				if s.Before(f2.Add(time.Millisecond)) && f2.After(s) {
					c++
				}
			}
		}
		if c > maxConc {
			maxConc = c
		}
	}
	fmt.Printf("\n=== %d workers spawned, max observed concurrency=%d, wall=%.1fs ===\n",
		len(spawned), maxConc, time.Since(start).Seconds())
	fmt.Println("\nFINAL ANSWER:\n", final)
}

func lastLine(s string) string {
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// lastRunes returns at most n trailing runes of s.
func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

func oneLine(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", "")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
