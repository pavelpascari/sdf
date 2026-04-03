package config

import "testing"

func TestGeneratePRTitle_SimpleHumanize(t *testing.T) {
	cfg := Defaults()
	cfg.PRTitle.ConventionalCommits = boolPtr(false)

	title := GeneratePRTitle(cfg, "my-stack", "my-stack/add-user-auth", nil)
	if title != "add user auth" {
		t.Errorf("expected 'add user auth', got %q", title)
	}
}

func TestGeneratePRTitle_HumanizeUnderscores(t *testing.T) {
	cfg := Defaults()
	cfg.PRTitle.ConventionalCommits = boolPtr(false)

	title := GeneratePRTitle(cfg, "feat", "feat/db_schema_update", nil)
	if title != "db schema update" {
		t.Errorf("expected 'db schema update', got %q", title)
	}
}

func TestGeneratePRTitle_NoPrefixMatch(t *testing.T) {
	cfg := Defaults()
	cfg.PRTitle.ConventionalCommits = boolPtr(false)

	title := GeneratePRTitle(cfg, "my-stack", "some-other-branch", nil)
	if title != "some other branch" {
		t.Errorf("expected 'some other branch', got %q", title)
	}
}

func TestGeneratePRTitle_DefaultUsesConventionalWithScope(t *testing.T) {
	cfg := Defaults()

	title := GeneratePRTitle(cfg, "my-stack", "my-stack/add-auth", nil)
	if title != "feat(my-stack): add auth" {
		t.Errorf("expected 'feat(my-stack): add auth', got %q", title)
	}
}

func TestGeneratePRTitle_ConventionalCommits_DetectFix(t *testing.T) {
	cfg := Defaults()

	subjects := []string{
		"fix: correct null pointer in handler",
		"fix: handle edge case for empty input",
		"update tests",
	}

	title := GeneratePRTitle(cfg, "my-stack", "my-stack/null-fix", subjects)
	if title != "fix(my-stack): null fix" {
		t.Errorf("expected 'fix(my-stack): null fix', got %q", title)
	}
}

func TestGeneratePRTitle_ConventionalCommits_DetectFromScoped(t *testing.T) {
	cfg := Defaults()

	subjects := []string{
		"refactor(auth): simplify token validation",
		"refactor(auth): extract middleware",
	}

	title := GeneratePRTitle(cfg, "stack", "stack/auth-cleanup", subjects)
	if title != "refactor(stack): auth cleanup" {
		t.Errorf("expected 'refactor(stack): auth cleanup', got %q", title)
	}
}

func TestGeneratePRTitle_ConventionalCommits_MostCommonWins(t *testing.T) {
	cfg := Defaults()

	subjects := []string{
		"feat: add endpoint",
		"fix: typo",
		"feat: add validation",
		"feat: add tests",
	}

	title := GeneratePRTitle(cfg, "s", "s/api-layer", subjects)
	if title != "feat(s): api layer" {
		t.Errorf("expected 'feat(s): api layer', got %q", title)
	}
}

func TestGeneratePRTitle_CustomScope_UsesStackID(t *testing.T) {
	cfg := Defaults()
	cfg.BranchPrefix.Scope = "api"

	// branch_prefix.scope should only affect branch naming, not PR title scope.
	// PR title scope should always use the stack name.
	title := GeneratePRTitle(cfg, "my-stack", "api/add-login", nil)
	if title != "feat(my-stack): add login" {
		t.Errorf("expected 'feat(my-stack): add login', got %q", title)
	}
}

func TestGeneratePRTitle_WithTicket(t *testing.T) {
	cfg := Defaults()
	cfg.PRTitle.TicketPattern = `([A-Z]+-\d+)`

	title := GeneratePRTitle(cfg, "proj", "proj/PROJ-123-add-auth", nil)
	if title != "feat(PROJ-123): add auth" {
		t.Errorf("expected 'feat(PROJ-123): add auth', got %q", title)
	}
}

func TestGeneratePRTitle_WithTicketAndType(t *testing.T) {
	cfg := Defaults()
	cfg.PRTitle.TicketPattern = `([A-Z]+-\d+)`

	subjects := []string{"fix: resolve deadlock in worker"}

	title := GeneratePRTitle(cfg, "proj", "proj/JIRA-42-deadlock-fix", subjects)
	if title != "fix(JIRA-42): deadlock fix" {
		t.Errorf("expected 'fix(JIRA-42): deadlock fix', got %q", title)
	}
}

func TestGeneratePRTitle_TicketNoMatch_FallsBackToScope(t *testing.T) {
	cfg := Defaults()
	cfg.PRTitle.TicketPattern = `([A-Z]+-\d+)`

	title := GeneratePRTitle(cfg, "feat", "feat/add-login", nil)
	if title != "feat(feat): add login" {
		t.Errorf("expected 'feat(feat): add login', got %q", title)
	}
}

func TestGeneratePRTitle_InvalidTicketRegex_FallsBackToScope(t *testing.T) {
	cfg := Defaults()
	cfg.PRTitle.TicketPattern = `([invalid` // bad regex

	title := GeneratePRTitle(cfg, "s", "s/feature", nil)
	if title != "feat(s): feature" {
		t.Errorf("expected 'feat(s): feature', got %q", title)
	}
}

func TestGeneratePRTitle_EmptyStackID_NoScope(t *testing.T) {
	cfg := Defaults()

	title := GeneratePRTitle(cfg, "", "some-branch", nil)
	if title != "feat: some branch" {
		t.Errorf("expected 'feat: some branch', got %q", title)
	}
}

func TestExtractTicket_CaptureGroup(t *testing.T) {
	cfg := Defaults()
	cfg.PRTitle.TicketPattern = `([A-Z]+-\d+)`

	ticket := extractTicket(cfg, "proj/PROJ-123-feature")
	if ticket != "PROJ-123" {
		t.Errorf("expected 'PROJ-123', got %q", ticket)
	}
}

func TestExtractTicket_NoGroup(t *testing.T) {
	cfg := Defaults()
	cfg.PRTitle.TicketPattern = `[A-Z]+-\d+`

	ticket := extractTicket(cfg, "proj/PROJ-456-feature")
	if ticket != "PROJ-456" {
		t.Errorf("expected 'PROJ-456', got %q", ticket)
	}
}

func TestExtractTicket_EmptyPattern(t *testing.T) {
	cfg := Defaults()
	ticket := extractTicket(cfg, "any-branch")
	if ticket != "" {
		t.Errorf("expected empty, got %q", ticket)
	}
}

func TestDetectCommitType(t *testing.T) {
	tests := []struct {
		name     string
		subjects []string
		want     string
	}{
		{"nil input defaults to feat", nil, "feat"},
		{"empty slice defaults to feat", []string{}, "feat"},
		{"no conventional prefixes defaults to feat", []string{"add feature", "update readme", "fix stuff"}, "feat"},
		{"single fix commit", []string{"fix: null pointer in handler"}, "fix"},
		{"single feat commit", []string{"feat: add user login"}, "feat"},
		{"single docs commit", []string{"docs: update README"}, "docs"},
		{"single refactor commit", []string{"refactor: simplify auth flow"}, "refactor"},
		{"single perf commit", []string{"perf: optimize query"}, "perf"},
		{"single test commit", []string{"test: add unit tests for parser"}, "test"},
		{"single build commit", []string{"build: update go version"}, "build"},
		{"single ci commit", []string{"ci: add lint step"}, "ci"},
		{"single chore commit", []string{"chore: bump dependencies"}, "chore"},
		{"single revert commit", []string{"revert: undo auth change"}, "revert"},
		{"single style commit", []string{"style: format imports"}, "style"},
		{"scoped commit detected", []string{"fix(auth): handle expired tokens"}, "fix"},
		{"most common wins feat over fix", []string{
			"feat: add endpoint",
			"fix: typo",
			"feat: add validation",
			"feat: add tests",
		}, "feat"},
		{"most common wins fix over feat", []string{
			"fix: null pointer",
			"feat: add logging",
			"fix: race condition",
			"fix: off-by-one",
		}, "fix"},
		{"mixed with non-conventional ignored", []string{
			"fix: handle timeout",
			"update readme",
			"fix: retry logic",
			"WIP save",
		}, "fix"},
		{"scoped commits counted correctly", []string{
			"refactor(auth): simplify token validation",
			"refactor(auth): extract middleware",
			"feat: add logging",
		}, "refactor"},
		{"tie broken by iteration order but both valid", []string{
			"feat: add endpoint",
			"fix: null pointer",
		}, "feat"}, // map iteration, but both are valid conventional types
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectCommitType(tt.subjects)
			if got != tt.want {
				t.Errorf("detectCommitType() = %q, want %q", got, tt.want)
			}
		})
	}
}
