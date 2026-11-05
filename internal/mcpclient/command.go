package mcpclient

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type commandTransport struct{ command *exec.Cmd }

func (t *commandTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stdout, err := t.command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := t.command.StdinPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	stop, err := startOwnedCommand(t.command)
	if err != nil {
		_ = stdout.Close()
		_ = stdin.Close()
		return nil, err
	}
	inner, _ := (&mcp.IOTransport{Reader: stdout, Writer: stdin}).Connect(ctx)
	return &commandConnection{Connection: inner, command: t.command, stop: stop}, nil
}

type commandConnection struct {
	mcp.Connection
	command *exec.Cmd
	stop    func() error
	once    sync.Once
	err     error
}

func (c *commandConnection) Close() error {
	c.once.Do(func() {
		_ = c.Connection.Close()
		wait := make(chan error, 1)
		go func() { wait <- c.command.Wait() }()
		var exit error
		joined := false
		select {
		case exit = <-wait:
			joined = true
		case <-time.After(100 * time.Millisecond):
		}
		// npm/uv may leave descendants after their parent exits. Stop only
		// this launch's process group/job even when the parent already ended.
		c.err = c.stop()
		if !joined {
			exit = <-wait
		}
		var expected *exec.ExitError
		if exit != nil && !errors.As(exit, &expected) {
			c.err = errors.Join(c.err, exit)
		}
	})
	return c.err
}
