// queryprobe exercises the normally built binary's SQLite callback ABI. Race
// instrumentation can hide the pinned driver's zero-length BLOB pointer fault.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/history"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "expected database path and JSON query")
		os.Exit(2)
	}
	var input struct {
		SQL    string `json:"sql"`
		Params []any  `json:"params"`
	}
	if err := json.Unmarshal([]byte(os.Args[2]), &input); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := history.New(os.Args[1]).Query(ctx, input.SQL, input.Params, 10000)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
