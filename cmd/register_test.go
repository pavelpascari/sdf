package cmd

import (
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestInferStackName_CommonPrefix(t *testing.T) {
	chain := []stack.PRRecord{
		{HeadRefName: "users/db-schema"},
		{HeadRefName: "users/repository"},
		{HeadRefName: "users/controller"},
	}
	name := inferStackName(chain)
	if name != "users" {
		t.Errorf("expected 'users', got %q", name)
	}
}

func TestInferStackName_CommonPrefixWithDash(t *testing.T) {
	chain := []stack.PRRecord{
		{HeadRefName: "auth-db"},
		{HeadRefName: "auth-api"},
		{HeadRefName: "auth-ui"},
	}
	name := inferStackName(chain)
	if name != "auth" {
		t.Errorf("expected 'auth', got %q", name)
	}
}

func TestInferStackName_NoCommonPrefix(t *testing.T) {
	chain := []stack.PRRecord{
		{HeadRefName: "schema-changes"},
		{HeadRefName: "api-layer"},
	}
	name := inferStackName(chain)
	// No useful common prefix; should use the first branch name
	if name != "schema-changes" {
		t.Errorf("expected 'schema-changes', got %q", name)
	}
}

func TestInferStackName_SlashReplacedWithDash(t *testing.T) {
	chain := []stack.PRRecord{
		{HeadRefName: "feature/auth/db"},
		{HeadRefName: "feature/auth/api"},
	}
	name := inferStackName(chain)
	if name != "feature-auth" {
		t.Errorf("expected 'feature-auth', got %q", name)
	}
}

func TestInferStackName_Empty(t *testing.T) {
	name := inferStackName(nil)
	if name != "my-stack" {
		t.Errorf("expected 'my-stack', got %q", name)
	}
}

func TestCommonPrefix(t *testing.T) {
	tests := []struct {
		a, b, want string
	}{
		{"users/db", "users/api", "users/"},
		{"abc", "abd", "ab"},
		{"abc", "xyz", ""},
		{"same", "same", "same"},
		{"short", "shorter", "short"},
	}

	for _, tc := range tests {
		got := commonPrefix(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("commonPrefix(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}
