---
title: Bubble Tea's inline renderer left stale rows when the live view got shorter
date: 2026-10-06
category: integration-issues
module: internal/ui/tui, go.mod
problem_type: integration_issue
component: frontend
symptoms:
  - "After a column loses its last card, the screen shows two crew headers and two Bots titles"
  - "Above the new frame sit the top rows of the previous, taller frame: its header, its Bots title and the top edge of a card"
root_cause: upstream_bug
resolution_type: dependency_update
severity: medium
framework_version: "bubbletea v2.0.10, ultraviolet v0.0.0-20260811164956-006e29f97886"
retire_when: "A charm.land/bubbletea/v2 release requires github.com/charmbracelet/ultraviolet v0.0.0-20261001125412-878653296cfd or later; check the go.mod of the latest bubbletea release"
tags: [bubbletea, ultraviolet, inline-renderer, live-view, stale-rows, frame-height, acceptance, tmux]
---

# Bubble Tea's inline renderer left stale rows when the live view got shorter

## Problem

crew's live view runs Bubble Tea in inline mode: `tea.NewView` with no alt screen (`internal/ui/tui/layout.go:54`). With Bubble Tea v2.0.10 and the ultraviolet version it pulled in, a frame shorter than the one before left the old frame's top rows on screen, and the new frame was drawn below them. Removing the board's Handled column (#230) made the frame shrink in ordinary use, whenever a column loses its last card.

## Symptoms

- After a rule failed and its issue's card left the development column, the screen read: the boot lines, then a stray `crew ╱╱ widgets …` header, a blank row, a `Bots ───` title and a lone `╭───╮`, then the real header, Bots, the board and the rest. Two headers, two Bots titles.
- In this session's runs, the rows that stayed behind matched the rows the frame lost: six (one board card) in the reproduction below, four in crew's screen, where other rows also changed.

## What Didn't Work

- **Suspecting the acceptance harness.** The black-box scenario `TestScreenFailedRuleIssue` (`acceptance/scenarios/screen/board_test.go:56`) caught it through `wantOneFrame` (`acceptance/scenarios/screen/helpers_test.go:279`), which requires one header and one Bots section. The harness draws crew's output with the `x/vt` emulator on a 120×50 pseudo-terminal, so the first suspicion was the emulator. 50 rows are more than the boot lines plus the taller frame, so nothing had scrolled.
- **Looking for a newer Bubble Tea.** v2.0.10 is the latest `charm.land/bubbletea/v2` release, so upgrading Bubble Tea itself was not an option.

## Solution

1. Reproduce outside crew. A 40-line Bubble Tea v2.0.10 program prints eight boot lines, renders an inline view with six extra rows, then drops them after 500 ms. Run inside `tmux new-session -x 120 -y 50` and read with `tmux capture-pane -p`, it showed the same doubled header. tmux is a real terminal emulator, so the renderer was at fault and not `x/vt`.
2. Raise the indirect dependency. The same program built against `github.com/charmbracelet/ultraviolet@v0.0.0-20261001125412-878653296cfd` redraws the shorter frame in place, with one header and no stale rows. In crew:

   ```sh
   go get github.com/charmbracelet/ultraviolet@v0.0.0-20261001125412-878653296cfd
   go mod tidy
   ```

   This also raises `github.com/mattn/go-runewidth` to v0.0.30 and `github.com/xo/terminfo` to v1.0.0, which the newer ultraviolet requires. `go.mod` now pins ultraviolet at line 16, still `// indirect`, under Bubble Tea v2.0.10 at line 7.

After the bump, `TestScreenFailedRuleIssue` passes, along with every other screen scenario, three runs out of three.

## Why This Works

Bubble Tea v2 draws through ultraviolet's terminal renderer. Between the two pseudo-versions ultraviolet changed its renderer (`terminal_renderer.go`, `terminal_renderer_hardscroll.go`, `terminal_renderer_hashmap.go` and `terminal_screen.go` all differ). The tmux reproduction shows the newer one moves back to the frame's first row and clears what the shorter frame no longer covers. This session did not pin down the upstream commit; the evidence is the reproduction before and after the bump.

The reproduction shows the bug does not depend on #230. The Handled column hid it in crew: it only gained cards during a run, so the board's tallest column, and with it the frame, never got shorter. Without that column, the board loses height whenever the tallest column loses a card.

## Prevention

- Keep `wantOneFrame` in every screen scenario that snapshots a screen after the view changes. A snapshot alone would have recorded the doubled frame as the expected one if a tester had accepted it.
- When a change can make the live view shorter (fewer cards, a column that stops being drawn, a section that collapses), add a screen scenario that goes through the shrink, not only one that ends taller.
- Do not lower ultraviolet below v0.0.0-20261001125412 while Bubble Tea is v2.0.10. A `go mod tidy` will not drop the pin, since it is a version raise, but a manual edit or a dependency downgrade could. If a Bubble Tea release later requires an older ultraviolet, run the tmux reproduction above before accepting it.
- To tell a renderer bug from an emulator bug, reproduce in tmux with a minimal Bubble Tea program at the same versions; `tmux capture-pane -p` gives a plain-text screen to compare.

## Related Issues

- #230: drop the Handled column from the live view's board, the change that exposed this.
- `docs/solutions/design-patterns/live-view-scroll-sections-take-a-fixed-height.md`: the live view's height budget, another way the frame changes height.
