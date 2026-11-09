//go:build unix

package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A real subprocess owns a further child. Both must end, including when MCP
// initialization never responds and cannot perform cooperative cleanup.
func TestMCPCommandFixture(t *testing.T) {
	mode := os.Getenv("ZWAI_MCP_TEST_PROCESS")
	if mode == "" {
		return
	}
	child := exec.Command(os.Args[0], "-test.run=^TestMCPLeafFixture$")
	child.Env = append(os.Environ(), "ZWAI_MCP_TEST_LEAF=1")
	child.Stdout, child.Stderr = io.Discard, io.Discard
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	cwd, _ := os.Getwd()
	raw, _ := json.Marshal(map[string]any{"parent": os.Getpid(), "child": child.Process.Pid, "cwd": cwd, "value": os.Getenv("ZWAI_MCP_TEST_VALUE")})
	if err := os.WriteFile(os.Getenv("ZWAI_MCP_TEST_RECEIPT"), raw, 0o600); err != nil {
		os.Exit(3)
	}
	if mode == "hang" {
		time.Sleep(time.Hour)
		return
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "subprocess", Version: "test"}, nil)
	if mode == "progress" {
		s.AddTool(&mcp.Tool{Name: "work", InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if token := req.Params.GetProgressToken(); token != nil {
				if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token, Progress: 1, Total: 2, Message: "Reading rows"}); err != nil {
					return nil, err
				}
			}
			finish := os.Getenv("ZWAI_MCP_TEST_RECEIPT") + ".finish"
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				if _, err := os.Stat(finish); err == nil {
					return progressFixtureResult(), nil
				} else if !errors.Is(err, os.ErrNotExist) {
					return nil, err
				}
				select {
				case <-tick.C:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		})
	} else {
		s.AddTool(&mcp.Tool{Name: "observe", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil
		})
	}
	_ = s.Run(context.Background(), &mcp.StdioTransport{})
	_ = child.Process.Kill()
	_ = child.Wait()
	os.Exit(0)
}

func TestMCPLeafFixture(t *testing.T) {
	if os.Getenv("ZWAI_MCP_TEST_LEAF") != "1" {
		return
	}
	time.Sleep(time.Hour)
}

type processReceipt struct {
	Parent, Child int
	Cwd, Value    string
}

func readProcessReceipt(t *testing.T, path string) processReceipt {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		var receipt processReceipt
		if err == nil && json.Unmarshal(raw, &receipt) == nil && receipt.Child > 0 {
			return receipt
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("stdio fixture did not publish process identity")
	return processReceipt{}
}

func assertProcessExited(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		// Linux containers may leave an orphaned zombie until their init reaps
		// it. Such a child has exited and cannot hold pipes or perform writes.
		state, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		if strings.Contains(string(state), "Z") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("owned MCP subprocess %d survived cleanup", pid)
}

func stdioConfig(t *testing.T, mode string) (config.MCPServer, string) {
	t.Helper()
	receipt := filepath.Join(t.TempDir(), "process.json")
	return config.MCPServer{ID: "local", Transport: "stdio", Command: os.Args[0], Args: []string{"-test.run=^TestMCPCommandFixture$"}, Env: map[string]string{"ZWAI_MCP_TEST_PROCESS": mode, "ZWAI_MCP_TEST_RECEIPT": receipt, "ZWAI_MCP_TEST_VALUE": "owned-environment"}, Enabled: true, TimeoutSeconds: 2}, receipt
}

func TestStdioToolsUseSelectedWorkspaceAndCloseOwnedDescendants(t *testing.T) {
	s, path := stdioConfig(t, "serve")
	m := NewManager()
	t.Cleanup(m.Close)
	workspace := t.TempDir() + " "
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(workspace) })
	set, err := m.Tools(t.Context(), []config.MCPServer{s}, workspace, 10000)
	if err != nil || len(set) != 2 {
		t.Fatalf("tools %d %v", len(set), err)
	}
	_, _ = set[0].(tool.InvokableTool).InvokableRun(t.Context(), "{}")
	receipt := readProcessReceipt(t, path)
	resolved, _ := filepath.EvalSymlinks(workspace)
	if receipt.Cwd != resolved || receipt.Value != "owned-environment" {
		t.Fatalf("stdio environment: %+v", receipt)
	}
	out, err := set[1].(tool.InvokableTool).InvokableRun(t.Context(), `{"name":"observe","arguments":{}}`)
	if err != nil || !strings.Contains(out, "owned-environment") {
		t.Fatalf("call %s %v", out, err)
	}
	m.Close()
	assertProcessExited(t, receipt.Parent)
	assertProcessExited(t, receipt.Child)
}

func TestMCPProgressStreamsFromOwnedStdioCommand(t *testing.T) {
	s, receiptPath := stdioConfig(t, "progress")
	m := NewManager()
	t.Cleanup(m.Close)
	set, err := m.Tools(t.Context(), []config.MCPServer{s}, t.TempDir(), 10000)
	if err != nil || len(set) != 2 {
		t.Fatalf("tools %d %v", len(set), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	remote := set[1].(tool.InvokableTool)
	updates, result := runProgressTool(t, ctx, remote, `{}`)
	if got := awaitProgress(t, updates); got != "Reading rows · 1 / 2" {
		t.Fatalf("stdio progress %q", got)
	}
	select {
	case got := <-result:
		t.Fatalf("stdio tool completed before its release: %+v", got)
	default:
	}
	if err := os.WriteFile(receiptPath+".finish", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := awaitProgress(t, result)
	// The negotiated stdio protocol adds its own terminal metadata. Compare
	// with an otherwise identical call that did not request notifications.
	baseline, err := remote.InvokableRun(ctx, `{"name":"work","arguments":{}}`)
	if got.err != nil || err != nil || got.text != baseline || !strings.Contains(got.text, `"text":"completed external work"`) {
		t.Fatalf("progress altered the stdio result: %+v, baseline %s %v", got, baseline, err)
	}
	receipt := readProcessReceipt(t, receiptPath)
	m.Close()
	assertProcessExited(t, receipt.Parent)
	assertProcessExited(t, receipt.Child)
}

func TestUnresponsiveStdioInitializationReapsOwnedProcessTree(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			s, path := stdioConfig(t, "hang")
			s.TimeoutSeconds = 1
			m := NewManager()
			t.Cleanup(m.Close)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := m.Inspect(ctx, s, t.TempDir()); done <- err }()
			receipt := readProcessReceipt(t, path)
			if mode == "cancel" {
				cancel()
			}
			if mode == "shutdown" {
				m.Close()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("accepted an unresponsive server")
				}
			case <-time.After(4 * time.Second):
				t.Fatal("MCP initialization did not stop")
			}
			assertProcessExited(t, receipt.Parent)
			assertProcessExited(t, receipt.Child)
		})
	}
}

func TestAlreadyCancelledMCPInspectionDoesNotLaunchACommand(t *testing.T) {
	s, path := stdioConfig(t, "serve")
	m := NewManager()
	defer m.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.Inspect(ctx, s, t.TempDir()); err != context.Canceled {
		t.Fatalf("inspect %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cancelled inspection started command: %v", err)
	}
}

func TestStdioTransportRefusesInvalidPipesAndRetainsCleanupErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (&commandTransport{command: exec.Command("cat")}).Connect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled transport %v", err)
	}
	for _, field := range []string{"stdout", "stdin"} {
		cmd := exec.Command("cat")
		if field == "stdout" {
			cmd.Stdout = io.Discard
		} else {
			cmd.Stdin = strings.NewReader("")
		}
		if _, err := (&commandTransport{command: cmd}).Connect(t.Context()); err == nil {
			t.Fatalf("already assigned %s pipe accepted", field)
		}
	}
	connection, err := (&commandTransport{command: exec.Command("cat")}).Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("normal stdio EOF failed cleanup: %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("idempotent close %v", err)
	}
	left, right := mcp.NewInMemoryTransports()
	peer, _ := right.Connect(t.Context())
	defer peer.Close()
	inner, _ := left.Connect(t.Context())
	cleanupErr := errors.New("owned command cleanup failed")
	broken := &commandConnection{Connection: inner, command: exec.Command("cat"), stop: func() error { return cleanupErr }}
	if err := broken.Close(); !errors.Is(err, cleanupErr) || !strings.Contains(err.Error(), "not started") {
		t.Fatalf("cleanup error concealed: %v", err)
	}
}
