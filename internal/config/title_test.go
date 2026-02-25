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

func TestGeneratePRTitle_CustomScope(t *testing.T) {
	cfg := Defaults()
	cfg.BranchPrefix.Scope = "api"

	title := GeneratePRTitle(cfg, "my-stack", "api/add-login", nil)
	if title != "feat(api): add login" {
		t.Errorf("expected 'feat(api): add login', got %q", title)
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

func TestDetectCommitType_Empty(t *testing.T) {
	typ := detectCommitType(nil)
	if typ != "feat" {
		t.Errorf("expected 'feat' default, got %q", typ)
	}
}

func TestDetectCommitType_NoConventional(t *testing.T) {
	subjects := []string{"add feature", "update readme", "fix stuff"}
	typ := detectCommitType(subjects)
	if typ != "feat" {
		t.Errorf("expected 'feat' default, got %q", typ)
	}
}
