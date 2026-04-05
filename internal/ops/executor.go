package ops

import (
	"fmt"
	"strings"
)

// HandlerFunc executes a step and returns its outputs.
type HandlerFunc func(stepID, kind string, inputs map[string]string) (map[string]string, error)

// Option configures an Executor.
type Option func(*Executor)

// WithHandler sets a custom handler (used for testing).
func WithHandler(h HandlerFunc) Option {
	return func(e *Executor) { e.handler = h }
}

// WithPersistence enables saving progress to disk after each step.
func WithPersistence(root string) Option {
	return func(e *Executor) { e.root = root; e.persist = true }
}

// Executor runs an Operation's steps in order, resolving refs and tracking status.
type Executor struct {
	op      *Operation
	handler HandlerFunc
	root    string
	persist bool
}

// NewExecutor creates an Executor for the given operation.
func NewExecutor(op *Operation, opts ...Option) *Executor {
	e := &Executor{op: op}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Run validates the step graph, then executes steps in order.
func (e *Executor) Run() error {
	if err := Validate(e.op); err != nil {
		return fmt.Errorf("invalid operation plan: %w", err)
	}
	for _, step := range e.op.Steps {
		if step.Status == StatusDone || step.Status == StatusSkipped {
			continue
		}
		inputs, err := e.resolveInputs(step)
		if err != nil {
			return err
		}
		step.Status = StatusInProgress
		e.save()
		outputs, err := e.handler(step.ID, step.Kind, inputs)
		if err != nil {
			step.Status = StatusFailed
			step.Error = err.Error()
			e.save()
			return fmt.Errorf("step %s (%s) failed: %w", step.ID, step.Kind, err)
		}
		step.Outputs = outputs
		step.Status = StatusDone
		step.Error = ""
		e.save()
	}
	return nil
}

func (e *Executor) resolveInputs(step *Step) (map[string]string, error) {
	resolved := make(map[string]string, len(step.Inputs))
	for name, val := range step.Inputs {
		if val.Literal != "" {
			resolved[name] = val.Literal
			continue
		}
		if val.Ref != "" {
			parts := strings.SplitN(val.Ref, ".", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("step %s: malformed ref %q", step.ID, val.Ref)
			}
			upstream := e.op.FindStep(parts[0])
			if upstream == nil {
				return nil, fmt.Errorf("step %s: references unknown step %q", step.ID, parts[0])
			}
			if upstream.Status != StatusDone {
				return nil, fmt.Errorf("step %s: depends on %q which has status %q", step.ID, parts[0], upstream.Status)
			}
			output, ok := upstream.Outputs[parts[1]]
			if !ok {
				return nil, fmt.Errorf("step %s: references output %s.%s which was not produced", step.ID, parts[0], parts[1])
			}
			resolved[name] = output
			continue
		}
	}
	return resolved, nil
}

func (e *Executor) save() {
	if e.persist && e.root != "" {
		_ = Save(e.root, e.op)
	}
}
