# sdf split — Plan Refinement & Persistence (Iteration 5)

## Summary

Replace the Y/n confirmation prompt with a three-choice menu: Execute, Refine, Abort. "Refine" spawns an interactive Claude session (`claude --resume`) so the user can adjust the plan conversationally. After the session, sdf re-extracts the updated plan and loops back.

Additionally, persist the plan to disk as YAML during the analysis-to-execution window. Delete it after successful execution.

---

## Three-Choice Prompt

After displaying the plan, show:

```
? What would you like to do?
  > Execute this split
    Refine plan (opens Claude session)
    Abort
```

Uses existing `ui.Select`. The `-y` flag skips this and goes straight to execute.

---

## Refine Flow

When "Refine" is chosen:

1. Spawn `claude --resume <session_id> <initial_prompt>` as an interactive terminal process (not `-p` mode)
2. The initial prompt:
   - Summarizes the current plan (layers, files, descriptions)
   - Reminds Claude of the YAML output format
   - Asks the user what they'd like to change
3. User interacts with Claude directly in their terminal
4. When the interactive session exits, sdf re-extracts the plan:
   - Resume the same session with `-p`: `claude --resume <session_id> -p "Return the current split plan as YAML"`
   - Parse and validate the response
5. Display the updated plan, loop back to the 3-choice menu

### Initial prompt for interactive session

```
The user wants to refine the split plan. Here's the current plan:

<plan summary: layers with names, descriptions, files, partial files>

Ask the user what they'd like to change. When they're satisfied, they can
exit this session (Ctrl+C or /exit) and sdf will re-read the updated plan.

Remember: every changed file must appear in at least one layer, use kebab-case
layer names, and return YAML in the same format when asked.
```

### Re-extraction prompt (after interactive session)

```
Return the current split plan as YAML (wrapped in ```yaml fences).
Use the exact same format:

```yaml
layers:
  - name: <kebab-case-name>
    description: "<one-line summary>"
    files:
      - <path/to/file>
```

Include any changes discussed in the refinement session.
```

---

## Plan Persistence

### Save location

`.sdf/split-plans/<stack-name>.yaml`

### Lifecycle

1. **Save**: immediately after analysis completes (before the 3-choice prompt)
2. **Update**: after each refinement loop (overwrite with updated plan)
3. **Delete**: after successful split execution (tree identity verified)
4. **Survive abort**: if user aborts, plan stays on disk for reference

### Format

Standard YAML serialization of the `Plan` struct — same format Claude produces. Includes `partial_files` when present.

### Directory

`.sdf/split-plans/` created alongside `.sdf/stacks/` in the `.sdf/` tree (gitignored).

---

## Refine Loop

```
Analyze → Save plan → Display → Choose
  ├─ Execute → run → delete plan on success
  ├─ Refine → interactive claude → re-extract → re-validate → Save → Display → Choose (loop)
  └─ Abort → plan stays on disk
```

---

## Command Interface Changes

No new flags. Existing flags unchanged:

```
sdf split --from <branch> --stack <name>              # 3-choice prompt
sdf split --from <branch> --stack <name> -y           # skip prompt, execute
sdf split --from <branch> --stack <name> --dry-run    # show plan only, no prompt
```

---

## Code Changes

### cmd/split.go

- Replace `ui.Confirm("Execute this split?")` with `ui.Select` offering three choices
- Add refinement loop: call `refine()` → re-extract → re-validate → redisplay → loop
- Save plan to disk after analysis and after each refinement
- Delete plan file after successful execution

### internal/split/plan.go

- Add `SavePlan(path string, plan *Plan) error` — marshal to YAML, write to file
- Add `PlanPath(root, stackName string) string` — returns `.sdf/split-plans/<name>.yaml`
- Add `DeletePlan(path string) error` — remove plan file (ignore not-exist)

### internal/claude/claude.go

- Add `RunInteractiveResume(sessionID, prompt string) error` — spawns `claude --resume <id> <prompt>` with stdin/stdout/stderr connected to the terminal

### internal/split/ai.go

- Add `BuildRefinePrompt(plan *Plan) string` — builds the initial prompt for the interactive session
- Add `BuildReExtractPrompt() string` — builds the prompt to re-extract the plan after refinement
- Add `ReExtractPlan(sessionID, name string, display io.Writer) (*Plan, string, error)` — resumes session in `-p` mode, parses plan

---

## Error Handling

| Condition | Behavior |
|-----------|----------|
| Interactive `claude --resume` exits non-zero | Warning, loop back with original plan |
| Re-extraction fails to parse | Retry once, then fall back to original plan with warning |
| Re-extracted plan fails validation | Show errors, loop back with original plan |
| Plan file write failure | Non-fatal warning, execution proceeds |
| Plan file delete failure | Non-fatal warning |

---

## User-Facing Output

### Three-choice prompt

```
? What would you like to do?
  > Execute this split
    Refine plan (opens Claude session)
    Abort
```

### Refinement

```
Opening Claude session for plan refinement...
(Exit with Ctrl+C or /exit when done)

[interactive Claude session runs here]

Re-reading plan from Claude session...
```

Then redisplays the updated plan and shows the 3-choice prompt again.

### Plan persistence

```
  Plan saved to .sdf/split-plans/my-feature.yaml
```

(Shown once after initial analysis, and again after each refinement.)

---

## Testing Strategy

### cmd/split.go — Integration tests

- Plan file created after analysis
- Plan file deleted after successful execution
- Plan file survives abort

### internal/split/plan_test.go — Unit tests

- SavePlan writes valid YAML
- SavePlan round-trips through ParsePlan
- DeletePlan removes file
- DeletePlan on non-existent file is not an error

### internal/split/ai_test.go — Unit tests

- BuildRefinePrompt includes layer names and files
- BuildReExtractPrompt returns expected format

### internal/claude/claude.go — Manual testing only

- RunInteractiveResume spawns a terminal process (cannot be unit tested)
