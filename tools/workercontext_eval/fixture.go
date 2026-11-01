package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type workerFixture struct {
	AgentID    string  `json:"agent_id"`
	Role       string  `json:"role"`
	Generation int     `json:"generation"`
	Status     string  `json:"status"`
	Result     *string `json:"result,omitempty"`
	ResultFile *string `json:"result_file,omitempty"`
	Error      string  `json:"error"`
}

type fixture struct {
	SchemaVersion int    `json:"schema_version"`
	Instruction   string `json:"instruction"`
	Messages      []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Rounds []struct {
		User string `json:"user"`
	} `json:"rounds"`
	Workers []workerFixture `json:"workers"`
}

func loadFixture(path string) (*fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f fixture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&f); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("fixture contains trailing JSON")
	}
	if f.SchemaVersion != 1 || f.Instruction == "" || len(f.Workers) == 0 || (len(f.Messages) == 0 && len(f.Rounds) == 0) {
		return nil, errors.New("fixture needs schema_version=1, instruction, workers and messages/rounds")
	}
	seen := map[string]bool{}
	for index := range f.Workers {
		w := &f.Workers[index]
		if w.AgentID == "" || w.Role == "" || w.Generation <= 0 || seen[w.AgentID] ||
			(w.Status != "done" && w.Status != "failed") || (w.Status == "failed" && w.Error == "") ||
			(w.Status == "done" && w.Error != "") || (w.Result == nil) == (w.ResultFile == nil) {
			return nil, fmt.Errorf("invalid or duplicated worker identity/result: %q", w.AgentID)
		}
		seen[w.AgentID] = true
		if w.ResultFile != nil {
			file := *w.ResultFile
			if !filepath.IsAbs(file) {
				file = filepath.Join(filepath.Dir(path), file)
			}
			body, err := os.ReadFile(file)
			if err != nil {
				return nil, err
			}
			text := string(body)
			w.Result = &text
		}
	}
	for _, message := range f.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			return nil, errors.New("initial messages accept only user/assistant roles")
		}
	}
	for _, round := range f.Rounds {
		if round.User == "" {
			return nil, errors.New("empty round input")
		}
	}
	return &f, nil
}

func plantWorkers(reg *swarm.Registry, workers []workerFixture) error {
	for _, w := range workers {
		var failure error
		if w.Status == "failed" {
			failure = errors.New(w.Error)
		}
		if err := reg.PlantFinished(swarm.FinishedWorker{ID: w.AgentID, Role: w.Role, Generation: w.Generation, Result: *w.Result, Err: failure}); err != nil {
			return err
		}
	}
	return nil
}

func initialMessages(f *fixture) []adk.Message {
	messages := make([]adk.Message, 0, len(f.Messages))
	for _, message := range f.Messages {
		messages = append(messages, &schema.Message{Role: schema.RoleType(message.Role), Content: message.Content})
	}
	return messages
}
