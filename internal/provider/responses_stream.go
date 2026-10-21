package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

type responsesEvent struct {
	Type        string            `json:"type"`
	Delta       string            `json:"delta"`
	Arguments   string            `json:"arguments"`
	OutputIndex int               `json:"output_index"`
	Item        responsesItem     `json:"item"`
	Response    responsesResponse `json:"response"`
	Error       *openai.APIError  `json:"error"`
	Message     string            `json:"message"`
	Code        string            `json:"code"`
}

type responsesStreamState struct {
	text      string
	reasoning string
	calls     map[int]schema.ToolCall
	observer  *responsesObserver
	send      func(*schema.Message) bool
}

func (m *responsesModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	resp, err := m.request(ctx, in, true, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.Message](streamRelayBuffer)
	go func() {
		defer resp.Body.Close()
		defer sw.Close()
		state := &responsesStreamState{calls: map[int]schema.ToolCall{}, observer: newResponsesObserver(ctx), send: func(msg *schema.Message) bool { return sw.Send(msg, nil) }}
		err := readResponsesStream(resp, state)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil && !errors.Is(err, errResponsesReaderClosed) {
			sw.Send(nil, err)
		}
	}()
	return sr, nil
}

var errResponsesReaderClosed = errors.New("responses reader closed")

func readResponsesStream(resp *http.Response, state *responsesStreamState) error {
	reader := bufio.NewReader(resp.Body)
	var data []string
	eventType := ""
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if (line == "" || readErr != nil) && len(data) > 0 {
			raw := strings.Join(data, "\n")
			data = nil
			if raw == "[DONE]" {
				return io.ErrUnexpectedEOF
			}
			var event responsesEvent
			if err := json.Unmarshal([]byte(raw), &event); err != nil {
				return &openai.RequestError{HTTPStatusCode: resp.StatusCode, HTTPStatus: resp.Status, Err: err}
			}
			if event.Type == "" {
				event.Type = eventType
			}
			eventType = ""
			done, err := state.event(event)
			if err != nil || done {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return io.ErrUnexpectedEOF
			}
			return readErr
		}
	}
}

func (s *responsesStreamState) emit(msg *schema.Message) error {
	msg.Role = schema.Assistant
	if s.send(msg) {
		return errResponsesReaderClosed
	}
	return nil
}

func (s *responsesStreamState) event(e responsesEvent) (bool, error) {
	switch e.Type {
	case "response.output_text.delta", "response.refusal.delta":
		s.text += e.Delta
		return false, s.emit(&schema.Message{Content: e.Delta})
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		s.reasoning += e.Delta
		return false, s.emit(&schema.Message{ReasoningContent: e.Delta})
	case "response.output_item.added", "response.output_item.done":
		s.observer.item(e.Item)
		if e.Item.Type == "function_call" {
			return false, s.function(e.OutputIndex, e.Item)
		}
	case "response.function_call_arguments.delta":
		call, ok := s.calls[e.OutputIndex]
		if !ok {
			return false, fmt.Errorf("provider: responses function arguments arrived before its call")
		}
		call.Function.Arguments += e.Delta
		s.calls[e.OutputIndex] = call
		index := e.OutputIndex
		return false, s.emit(&schema.Message{ToolCalls: []schema.ToolCall{{Index: &index, Function: schema.FunctionCall{Arguments: e.Delta}}}})
	case "response.function_call_arguments.done":
		call, ok := s.calls[e.OutputIndex]
		if !ok {
			return false, fmt.Errorf("provider: responses function arguments arrived before its call")
		}
		return false, s.function(e.OutputIndex, responsesItem{Type: "function_call", CallID: call.ID, Name: call.Function.Name, Arguments: e.Arguments})
	case "response.completed":
		for i, item := range e.Response.Output {
			s.observer.item(item)
			if item.Type == "function_call" {
				if err := s.function(i, item); err != nil {
					return true, err
				}
			}
		}
		out, err := e.Response.message()
		if err != nil {
			return true, err
		}
		out.ToolCalls = nil
		out.Content, err = responsesRemaining(s.text, out.Content)
		if err != nil {
			return true, err
		}
		out.ReasoningContent, err = responsesRemaining(s.reasoning, out.ReasoningContent)
		if err != nil {
			return true, err
		}
		return true, s.emit(out)
	case "response.failed", "response.incomplete", "response.cancelled":
		for _, item := range e.Response.Output {
			s.observer.item(item)
		}
		e.Response.Status = strings.TrimPrefix(e.Type, "response.")
		return true, e.Response.failure()
	case "error":
		if e.Error != nil {
			return true, e.Error
		}
		return true, &openai.APIError{Code: e.Code, Message: e.Message, Type: "response_error"}
	}
	return false, nil
}

// A done item contains the full arguments, while delta events contain suffixes.
// Emit only what has not already crossed the local tool boundary.
func (s *responsesStreamState) function(index int, item responsesItem) error {
	old, exists := s.calls[index]
	if exists {
		if old.ID != item.CallID || old.Function.Name != item.Name {
			return fmt.Errorf("provider: responses function identity changed")
		}
		rest, err := responsesRemaining(old.Function.Arguments, item.Arguments)
		if err != nil {
			return err
		}
		if rest == "" {
			return nil
		}
		old.Function.Arguments = item.Arguments
		s.calls[index] = old
		return s.emit(&schema.Message{ToolCalls: []schema.ToolCall{{Index: &index, Function: schema.FunctionCall{Arguments: rest}}}})
	}
	call := schema.ToolCall{Index: &index, ID: item.CallID, Type: "function", Function: schema.FunctionCall{Name: item.Name, Arguments: item.Arguments}}
	s.calls[index] = call
	return s.emit(&schema.Message{ToolCalls: []schema.ToolCall{call}})
}

func responsesRemaining(sent, full string) (string, error) {
	if !strings.HasPrefix(full, sent) {
		return "", fmt.Errorf("provider: responses completed output disagrees with streamed deltas")
	}
	return strings.TrimPrefix(full, sent), nil
}
