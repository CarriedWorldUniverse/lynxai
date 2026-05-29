package extract

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/CarriedWorldUniverse/bridle"
)

// fakeTurner records what was passed to RunTurn and returns a canned result.
type fakeTurner struct {
	gotReq bridle.TurnRequest
	result bridle.TurnResult
	err    error
	calls  int
}

func (f *fakeTurner) RunTurn(_ context.Context, req bridle.TurnRequest, _ bridle.ToolRunner, _ bridle.EventSink) (bridle.TurnResult, error) {
	f.calls++
	f.gotReq = req
	return f.result, f.err
}

func TestExtract_HappyPath(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`)
	emitted := json.RawMessage(`{"n":42}`)

	ft := &fakeTurner{
		result: bridle.TurnResult{
			ToolCalls: []bridle.ToolInvocation{
				{Name: extractToolName, Args: emitted},
			},
		},
	}

	ex := NewExtractor(ft, "test-model")
	got, err := ex.Extract(context.Background(), ExtractRequest{
		PageMarkdown: "# title\n\nN is 42.",
		Schema:       schema,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if string(got) != string(emitted) {
		t.Fatalf("got %s, want %s", got, emitted)
	}

	if ft.calls != 1 {
		t.Fatalf("RunTurn called %d times, want 1", ft.calls)
	}
	if len(ft.gotReq.Tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(ft.gotReq.Tools))
	}
	tool := ft.gotReq.Tools[0]
	if tool.Name != extractToolName {
		t.Errorf("tool name = %q, want %q", tool.Name, extractToolName)
	}
	if string(tool.InputSchema) != string(schema) {
		t.Errorf("tool.InputSchema = %s, want %s", tool.InputSchema, schema)
	}
	if ft.gotReq.MaxSteps != 1 {
		t.Errorf("MaxSteps = %d, want 1", ft.gotReq.MaxSteps)
	}
	if ft.gotReq.Model != "test-model" {
		t.Errorf("Model = %q, want %q (bridle requires TurnRequest.Model)", ft.gotReq.Model, "test-model")
	}
}

func TestExtract_NoToolCallIsError(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	ft := &fakeTurner{
		result: bridle.TurnResult{ToolCalls: nil},
	}
	ex := NewExtractor(ft, "test-model")
	_, err := ex.Extract(context.Background(), ExtractRequest{
		PageMarkdown: "stuff",
		Schema:       schema,
	})
	if !errors.Is(err, ErrNoToolCall) {
		t.Fatalf("err = %v, want ErrNoToolCall", err)
	}
}

func TestExtract_ValidationFailureBubblesUp(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`)
	ft := &fakeTurner{
		result: bridle.TurnResult{
			ToolCalls: []bridle.ToolInvocation{
				{Name: extractToolName, Args: json.RawMessage(`{}`)},
			},
		},
	}
	ex := NewExtractor(ft, "test-model")
	_, err := ex.Extract(context.Background(), ExtractRequest{
		PageMarkdown: "stuff",
		Schema:       schema,
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "validate") && !strings.Contains(err.Error(), "required") && !strings.Contains(err.Error(), "n") {
		t.Fatalf("expected validation error mentioning the missing field, got %v", err)
	}
}
