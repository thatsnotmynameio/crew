---
name: cw-brainstorm
description: Brainstorms one GitHub issue parked in `crew:brainstorm:ready`, in the current session, the way this repository moves an idea through crew's labels. Use when the user runs /cw-brainstorm with an issue, or asks to brainstorm an issue the crew way, such as an idea parked for a later brainstorm.
argument-hint: "<issue: #42, 42 or its URL>"
---

# Brainstorm an issue for crew

This skill is the crew repository's own aid for building crew, not part of crew. Its prompt and labels are this repository's. Another repository copies the skill and edits its prompt to match its own labels and brainstorm.

crew polls the repository's GitHub issues and runs unattended sessions for the issues its rules take. A brainstorm needs the user, so no rule runs it: this skill runs it here, in the user's session. The skill fills the prompt in step 3 with the issue and follows it. It adds no steps of its own: the checks, label changes and skills to run are the prompt's.

Run `gh` with the repository root, from `git rev-parse --show-toplevel`, as the working directory.

## 1. Find the issue

The issue is the skill's argument: `#42`, `42` or the issue's URL. Without one, ask the user which issue; that is the only question the skill asks.

Read it with `gh issue view <number> --json number,title,url`. When `gh` fails, for example because the issue does not exist, report its error text and stop.

## 2. Fill the prompt

The prompt in step 3 is a Go template over the issue, the same as an action's prompt in crew. Replace each field with the issue's value:

| Field | Value |
| --- | --- |
| `{{.Issue.Ref}}` | `#` and the number, such as `#42` |
| `{{.Issue.Key}}` | the number, such as `42` |
| `{{.Issue.Title}}` | the issue's title |
| `{{.Issue.URL}}` | the issue's URL |

## 3. Run it

The prompt:

```text
Read {{.Issue.Ref}} with `gh issue view {{.Issue.Key}} --json state,labels`. When it is closed, or does not carry the label `crew:brainstorm:ready`, say so, name the crew labels it carries, and stop without changing anything.

Otherwise move it to `crew:brainstorm:in progress`: `gh issue edit {{.Issue.Key}} --remove-label "crew:brainstorm:ready" --add-label "crew:brainstorm:in progress"`. When that fails, report the error and stop.

Then run /compound-engineering:ce-brainstorm {{.Issue.Ref}} ({{.Issue.URL}}). The issue's title and body are the idea to brainstorm.
```

Follow the filled prompt as if the user had typed it as their next message in this session. Run every command it gives, invoke every skill or slash command it names with the arguments it gives, and stop where it says to stop.

Do not add checks, label changes or questions the prompt does not ask for. When a skill the prompt names is not installed, say which one and stop.

## 4. After the brainstorm

When the prompt's work ends, say which issue it was for. When the brainstorm wrote its plan file, mention `/cw-update-issue-plan`, which copies that file into the issue and then moves the issue to `crew:brainstorm:done`, or to a label given as its argument.
