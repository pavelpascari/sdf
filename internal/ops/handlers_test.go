package ops

import "testing"

func TestDefaultHandler_UnknownKind(t *testing.T) {
	outputs, err := DefaultHandler("step-1", "unknown-kind", map[string]string{"foo": "bar"})
	if err != nil {
		t.Errorf("expected no error for unknown kind, got: %v", err)
	}
	if outputs != nil {
		t.Errorf("expected nil outputs for unknown kind, got: %v", outputs)
	}
}

func TestDefaultHandler_NoOpKinds(t *testing.T) {
	kinds := []string{"render-status", "reorder-nodes", "reconcile-prs"}
	for _, kind := range kinds {
		outputs, err := DefaultHandler("step-1", kind, nil)
		if err != nil {
			t.Errorf("kind %s: expected no error, got: %v", kind, err)
		}
		if outputs != nil {
			t.Errorf("kind %s: expected nil outputs, got: %v", kind, outputs)
		}
	}
}
