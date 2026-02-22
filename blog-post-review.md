# Blog Post Review: "Testing CLI Tools That Shell Out"

**Post:** Testing CLI Tools That Shell Out: From 'Works on My Machine' to Provable Correctness
**Author:** Pavel Pascari
**Reviewed:** 2026-02-22

---

## Overall Summary

The post presents a five-layer testing strategy for CLI tools that delegate work to external binaries (`git`, `gh`). It progresses from simple fake binaries through binary injection, spy recording, cross-validation of fakes against reality, and build-tag isolation. The writing is technically precise, well-structured, and grounded in real code from the SDF project.

---

## Evaluation by Role

### Role 1: Existing SDF User

| Dimension | Rating | Notes |
|-----------|--------|-------|
| **Usefulness** | 6/10 | Gives insight into how sdf is tested, which builds trust in the tool. However, an existing user cares more about features, roadmap, and edge-case behavior than internal testing strategy. There is no new information about what sdf *does* — only about how it's verified. |
| **Easy to follow** | 8/10 | Familiar with the domain (stacked PRs, git, gh), so the context sections are easy. The Go code is readable. |
| **Time to lose interest** | ~60% through | Interest holds strong through Layers 1-2 (immediately relatable — "this is how my tool works"). Layers 3-5 are more of a tooling-nerd deep dive; an existing user may skim unless they're also a contributor. |
| **Storyline** | 7/10 | Clear progression. But the story is "how we test" — for someone who already uses sdf, the narrative doesn't create tension or revelation. |
| **Goals achieved** | 6/10 | The implicit goal for this audience is trust-building: "the tool I use is well-tested." The post achieves this partially — it shows rigor, but never connects back to what this means for the user's experience (e.g., "this is why sdf never corrupts your stack"). |
| **Reputation building** | 7/10 | Shows engineering maturity. Could be stronger if it explicitly connected testing discipline to user-facing reliability. |

**Key suggestion:** A one-paragraph aside early on — "If you use sdf, here's what this means for you: every sync, every rebase, every PR edit has been tested against recorded real GitHub responses" — would make this audience feel directly addressed.

---

### Role 2: Someone Evaluating Whether to Use SDF

| Dimension | Rating | Notes |
|-----------|--------|-------|
| **Usefulness** | 5/10 | This person wants to know: what does sdf do, does it work for my workflow, how does it compare to alternatives (ghstack, graphite, etc.)? The post answers none of these. It assumes you already know what sdf is and care about its internals. |
| **Easy to follow** | 6/10 | The opening paragraph ("Your CLI tool doesn't do anything on its own") is generic enough to follow. But by paragraph 3 the reader is deep in testing architecture without ever having been sold on the tool itself. |
| **Time to lose interest** | ~20% through | This reader arrived looking for "should I use sdf?" and found "here's how we test sdf." The mismatch causes early dropout. The problem statement (stacked PRs on GitHub) gets one sentence. |
| **Storyline** | 5/10 | The storyline is coherent but answers the wrong question for this audience. There's no on-ramp explaining what sdf does or why it matters. |
| **Goals achieved** | 4/10 | If the goal was to attract new users, this post undersells the product. It showcases engineering depth but doesn't explain the value proposition. |
| **Reputation building** | 6/10 | Signals that the tool is built seriously, which is positive. But reputation for a tool is primarily built by demonstrating that it solves a real problem well — this post skips that part. |

**Key suggestion:** The introduction needs 2-3 sentences that quickly explain what SDF does and why someone would want it, before diving into testing. Something like: "SDF manages stacked pull requests — it lets you break large changes into a chain of dependent PRs and keeps them synchronized as you iterate. Here's how we make sure that orchestration actually works."

---

### Role 3: Fellow Tool Builder With a Similar Problem

| Dimension | Rating | Notes |
|-----------|--------|-------|
| **Usefulness** | 9/10 | This is the post's ideal reader. Every section is directly applicable. The fake binary pattern, binary injection via package-level vars, spy recording with JSONL, structural JSON comparison, and build tags are all transferable patterns. The code snippets are copy-pasteable. |
| **Easy to follow** | 9/10 | Excellent structure. Each layer builds on the previous one, the code examples are real (not toy snippets), and the rationale for each decision is explicit. The "Why shell scripts instead of Go mocks?" question anticipates exactly what this reader is thinking. |
| **Time to lose interest** | Never (full read) | Each layer introduces a new problem that the previous layer didn't solve. The "honest-fakes problem" transition is the strongest hook — it names a pain that every tool builder has felt but few have articulated. |
| **Storyline** | 9/10 | Classic problem-escalation structure: "here's a simple approach, here's its limitation, here's how we addressed it, repeat." This is the canonical way to present layered engineering solutions. The "trust gradient" conclusion ties it together well. |
| **Goals achieved** | 9/10 | The post delivers exactly what the title promises. A tool builder walks away with a concrete, implementable strategy and real code to reference. |
| **Reputation building** | 9/10 | Strongly positions SDF (and its author) as a thoughtful engineering team. This is the kind of post that gets bookmarked and shared in Slack channels. |

**Key suggestion:** Consider adding a brief "Limitations and alternatives" section. A fellow tool builder would appreciate hearing: when does this approach break down? What about tools with stateful interactions (like an interactive TUI)? What about Windows compatibility of the shell-script fakes? Acknowledging boundaries would further strengthen credibility.

---

### Role 4: Software Engineer Reading for General Interest

| Dimension | Rating | Notes |
|-----------|--------|-------|
| **Usefulness** | 7/10 | Even if not building a CLI tool, the general principles transfer: testing code that depends on external systems, structural comparison over value comparison, keeping test infrastructure out of production. The "trust gradient" framing is a useful mental model for any testing strategy. |
| **Easy to follow** | 7/10 | Accessible if you know Go basics and have used `git` from the command line. The pseudocode in "The problem, concretely" section is well-placed and clarifies the domain quickly. Some sections (build tags, spy recorder internals) may feel dense for a casual reader. |
| **Time to lose interest** | ~70% through | The first three layers hold general interest (fake binaries = interesting pattern, binary injection = clever trick, spy recording = cool idea). Cross-validation and build tags are implementation details that feel specific to this project unless you're actively solving the same problem. |
| **Storyline** | 7/10 | Good narrative arc. The escalation from "simple fakes" to "provably honest fakes" is a satisfying progression. The "What we learned" section provides good takeaways even if you skimmed the middle. |
| **Goals achieved** | 7/10 | A generalist reader comes away with new ideas and a broadened perspective on testing. The post doesn't try to be a tutorial (good), but also doesn't fully generalize its lessons beyond CLI tools (missed opportunity). |
| **Reputation building** | 7/10 | Establishes the author as someone who thinks deeply about testing. SDF itself registers faintly — the reader remembers "that stacked PRs tool with the smart testing" more than specific features. |

**Key suggestion:** The "What we learned" section is the most valuable part for this audience. Consider making the individual lessons slightly more general — e.g., "Structural comparison is the right granularity" could briefly mention that this principle applies to any test that compares API responses, database snapshots, or serialized state, not just CLI output.

---

### Role 5: LinkedIn Reader Aware of Stacked Diffs

| Dimension | Rating | Notes |
|-----------|--------|-------|
| **Usefulness** | 6/10 | This reader knows the stacked-diffs space and is curious about SDF's approach. The post provides no comparison to alternatives (graphite, ghstack, spr, git-branchless) and doesn't explain SDF's design philosophy (delegating to git/gh rather than reimplementing). The testing angle is interesting but tangential to what this reader came for. |
| **Easy to follow** | 7/10 | Domain familiarity helps. The reader understands why stacked PRs need rebase-and-push orchestration, so the "problem, concretely" section resonates. |
| **Time to lose interest** | ~40% through | The opening hooks them ("oh, another stacked-diffs tool, let me see how they do it"). But the post pivots entirely to testing methodology and never returns to the stacked-diffs domain. By Layer 3 this reader is thinking "I came to learn about stacked diffs, not JSONL spy files." |
| **Storyline** | 6/10 | The story is internally consistent but doesn't serve this reader's expectations. They expected a post about stacked diffs that happens to discuss testing; they got a post about testing that happens to use stacked diffs as an example. |
| **Goals achieved** | 5/10 | If the goal was to attract the stacked-diffs community, the post doesn't make a case for SDF's approach vs. alternatives. The "delegates to git and gh" design is mentioned once but never explored — this is actually SDF's most distinctive architectural decision and deserves its own section or its own post. |
| **Reputation building** | 6/10 | Shows competence but doesn't differentiate SDF in the stacked-diffs landscape. A reader from this community leaves thinking "solid engineering, but what makes this tool different?" |

**Key suggestion:** Either (a) add a section early on explaining SDF's "thin orchestration layer" philosophy and why it matters for stacked-diffs workflows, or (b) adjust the LinkedIn positioning to target CLI tool builders rather than stacked-diffs practitioners. The post is excellent for audience (3) but is being marketed to audience (5).

---

## Cross-Role Summary

| Dimension | Role 1 (User) | Role 2 (Evaluator) | Role 3 (Tool Builder) | Role 4 (Generalist) | Role 5 (LinkedIn/SD) |
|-----------|:-:|:-:|:-:|:-:|:-:|
| **Usefulness** | 6 | 5 | 9 | 7 | 6 |
| **Easy to follow** | 8 | 6 | 9 | 7 | 7 |
| **Interest retention** | 6 | 4 | 10 | 7 | 5 |
| **Storyline** | 7 | 5 | 9 | 7 | 6 |
| **Goals achieved** | 6 | 4 | 9 | 7 | 5 |
| **Reputation building** | 7 | 6 | 9 | 7 | 6 |
| **Average** | **6.7** | **5.0** | **9.2** | **7.0** | **5.8** |

---

## Structural Observations

### Strengths

1. **Layered progression is masterful.** Each section introduces a problem that the previous layer couldn't solve. This "escalating stakes" structure keeps the ideal reader hooked and gives the post a satisfying arc.

2. **Code snippets are real and well-chosen.** They're not toy examples — they come from actual production code. The level of detail (showing `t.Cleanup`, showing the shell script case-matching) is exactly right: enough to implement, not so much that it's a code dump.

3. **The "honest fakes" framing is memorable.** Naming the core problem ("fakes rot") gives readers a concept they can reference later. This is the kind of phrase that gets quoted in team discussions.

4. **The summary table is effective.** The "How the layers compose" table is a quick-reference that makes the post skimmable for return visits.

5. **The writing is confident and direct.** No hedging, no excessive qualifiers. Sentences like "Shell scripts are the simplest thing that satisfies this constraint" are clear and assertive.

### Weaknesses

1. **The audience is narrow but the distribution is broad.** The post is written for Role 3 (fellow tool builders) but will be shared on LinkedIn where Roles 2 and 5 are more common. This mismatch will cause high bounce rates from people who expected a stacked-diffs post.

2. **SDF is underexplained.** A single sentence describes what SDF does. For a blog post on the SDF website, this is surprisingly modest. Readers unfamiliar with the tool have no on-ramp.

3. **No visual aids.** The layered architecture practically begs for a diagram — even a simple ASCII one showing "Unit Tests <-> Fakes <-> Cross-Validator <-> Recordings <-> Real Tools." The five-layer mental model is strong but would land faster with a visual.

4. **The "What we learned" section restates rather than elevates.** Each bullet summarizes a section rather than offering a new insight that emerges from having all five layers together. The "trust gradient" at the end does elevate — consider moving this framing earlier and using "What we learned" for genuinely new observations.

5. **No mention of trade-offs or failures.** The post reads as "here's what we built and it works." What went wrong along the way? What did you try that didn't work? What's still unsolved? A "War stories" subsection would make the narrative more human and more credible.

6. **Missing call to action for each audience.** The post ends with "feel free to steal the pattern" — which works for Role 3 but leaves other roles with no next step. Consider adding: "If you're interested in stacked PRs, check out [getting started guide]. If you want to contribute, here's how the test suite works."

---

## Recommendations (Prioritized)

### High Impact

1. **Add a 3-sentence SDF explainer in the introduction.** What it does, who it's for, and its core design philosophy (thin orchestration over reimplementation). This costs 30 seconds of reading time and unlocks Roles 2 and 5.

2. **Add a diagram of the five-layer testing architecture.** Even a simple box-and-arrow diagram. This makes the post skimmable and shareable (people screenshot diagrams for Slack/Twitter).

3. **Match distribution strategy to audience.** If sharing on LinkedIn to the stacked-diffs community, consider writing a separate short post that positions SDF in that landscape and links to this deep-dive for the technically curious.

### Medium Impact

4. **Add a "Limitations" paragraph.** When does this approach break down? (Interactive tools, Windows, tools with non-deterministic output.) This strengthens credibility with skeptical technical readers.

5. **Add differentiated calls to action at the end.** One for users ("try sdf"), one for tool builders ("steal this pattern"), one for contributors ("here's how to run the test suite").

6. **Strengthen "What we learned" with non-obvious insights.** E.g., "We expected cross-validation to catch bugs in our fakes. Instead, the first thing it caught was a bug in the real GitHub CLI — a field name that changed between versions." (If true, or a similar anecdote.)

### Low Impact (Nice to Have)

7. **Add timestamps or version context.** "As of gh v2.x" — helps readers calibrate whether the examples are current.

8. **Consider a "Before and after" opening.** Start with a concrete bug that would have been caught by this system, then build toward the solution. Narrative tension from minute one.

9. **Link to specific files in the repo.** Instead of "lives in `internal/spy/`", link to the actual directory on GitHub. Reduces friction for the reader who wants to go deeper.
