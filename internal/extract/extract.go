package extract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/CarriedWorldUniverse/bridle"
)

const (
	extractToolName     = "emit_extraction"
	extractToolDesc     = "Emit the structured extraction matching the provided JSON Schema. Call this tool exactly once with the extracted fields as arguments."
	extractSystemPrompt = "You are a schema-driven extraction agent. Read the provided page markdown and call the emit_extraction tool exactly once with arguments that conform to its JSON Schema. Do not call any other tool and do not emit free-form text."
)

// ErrNoToolCall is returned when the model finished a turn without emitting
// the extraction tool call.
var ErrNoToolCall = errors.New("extract: model returned no tool call")

// Turner is the subset of *bridle.Harness that Extractor needs. Tests pass a
// fake; production wires *bridle.Harness.
type Turner interface {
	RunTurn(ctx context.Context, req bridle.TurnRequest, runner bridle.ToolRunner, sink bridle.EventSink) (bridle.TurnResult, error)
}

// ExtractRequest is the input to a single extraction turn.
type ExtractRequest struct {
	PageMarkdown string
	Schema       json.RawMessage
}

// Extractor runs schema-driven extractions through a bridle Turner.
type Extractor struct {
	turner Turner
}

// NewExtractor wraps a Turner.
func NewExtractor(t Turner) *Extractor {
	return &Extractor{turner: t}
}

// Extract drives a single bridle turn against the given page + schema and
// returns the validated extraction JSON.
func (e *Extractor) Extract(ctx context.Context, req ExtractRequest) (json.RawMessage, error) {
	turnReq := bridle.TurnRequest{
		AspectID:           "lynxai-extract",
		AppendSystemPrompt: extractSystemPrompt,
		UserMessage:        req.PageMarkdown,
		Tools: []bridle.ToolDef{
			{
				Name:        extractToolName,
				Description: extractToolDesc,
				InputSchema: req.Schema,
			},
		},
		MaxSteps: 1,
	}

	result, err := e.turner.RunTurn(ctx, turnReq, nopRunner{}, nopSink{})
	if err != nil {
		return nil, fmt.Errorf("extract: bridle run turn: %w", err)
	}
	if len(result.ToolCalls) == 0 {
		return nil, ErrNoToolCall
	}

	args := result.ToolCalls[0].Args
	if err := ValidateAgainstSchema(req.Schema, args); err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}
	return args, nil
}

// nopRunner is supplied as the ToolRunner; the extraction tool is meant to be
// emitted by the model, not executed. If bridle ever tries to run it, we
// return an error so the misuse surfaces.
type nopRunner struct{}

func (nopRunner) Run(_ context.Context, call bridle.ToolCall) (json.RawMessage, error) {
	return nil, fmt.Errorf("extract: unexpected tool execution for %q", call.Name)
}

// nopSink discards all events.
type nopSink struct{}

func (nopSink) Emit(bridle.Event) {}
