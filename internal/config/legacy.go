package config

import (
	"errors"
	"fmt"

	"go.yaml.in/yaml/v3"
)

// oldTopKeys are the old top-level keys reported as a whole. Each old key
// maps to what replaced it: "now" and the key that took its place, or
// "gone" and why.
func oldTopKeys() map[string]string {
	return map[string]string{
		"harness":      "now agents.<name>.harness, beside the harness's name",
		"workflow":     "now rules, which maps each rule's name to the rule",
		"extra_labels": "gone; crew acts only on its rules' labels and leaves every other label alone",
		"prompts":      "gone; crew never ran them, so keep them in the skills that do",
	}
}

// oldSettings are the keys of the old config: section.
func oldSettings() map[string]string {
	return map[string]string{
		"poll_interval_seconds":  "now poll_interval_seconds",
		"max_parallel_issues":    "now max_parallel_issues",
		"run_time_limit_seconds": "now run_time_limit_seconds",
		"usage_in_status":        "now usage_in_status",
		"queues":                 "now queues",
		"clerk_slots": "gone; crew has no clerk queue of its own: " +
			"declare one under queues and name it in its rules' queue",
		"harness": "now agents.<name>.harness.name",
		"model":   "now agents.<name>.harness.model",
		"mate":    "now tracker.bot",
	}
}

// oldStageKeys are the keys of an old workflow stage that a rule does not
// have, whether written in a stage or in a rule.
func oldStageKeys() map[string]string {
	return map[string]string{
		"name":           "gone; a rule's name is its key under rules",
		"description":    "gone; crew does not read it",
		"issue_template": "gone; crew does not read it",
		"label":          "now labels.ready of the rule",
		"moves_to":       "now labels.running of the rule",
		"on_success":     "now labels.success of the rule",
		"on_failure":     "now labels.failure of the rule",
		"on_board":       "gone; board lists the board's columns, and notify says whether the rule sends notifications",
	}
}

// oldActionKeys are the keys of an old stage's action that an action does
// not have, whether written in a stage or in a rule.
func oldActionKeys() map[string]string {
	return map[string]string{
		"name": "gone; an action's name is its key under its rule's actions",
		"mate": "now agents.<name>.bot of the action's agent",
	}
}

// oldSettingsGone is said of a config: section that holds none of the old
// settings, so it is still named.
const oldSettingsGone = "gone; its keys are now at the top level"

// oldKeys reports every old key of the file's YAML document root, in file
// order, each with its line and replacement: the old top-level keys, the
// old stages and actions under workflow, and old stage or action keys
// written in a rule or an action under rules. It runs before the strict
// decode, which would stop at the first, so a config in the old keys is
// refused with all of them at once.
func oldKeys(root *yaml.Node) error {
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	var errs []error
	for _, e := range entries(root.Content[0], "") {
		switch e.key.Value {
		case "config":
			found := oldKeysOf(e, oldSettings())
			if len(found) == 0 {
				found = []error{keyError(e.path, e.key.Line, oldSettingsGone)}
			}
			errs = append(errs, found...)
		case "workflow":
			errs = append(errs, keyError(e.path, e.key.Line, oldTopKeys()[e.key.Value]))
			errs = append(errs, oldRuleKeys(sequenceItems(e), sequenceItems)...)
		case "rules":
			errs = append(errs, oldRuleKeys(mappingKeys(e), mappingKeys)...)
		default:
			if text, ok := oldTopKeys()[e.key.Value]; ok {
				errs = append(errs, keyError(e.path, e.key.Line, text))
			}
		}
	}
	return errors.Join(errs...)
}

// oldRuleKeys reports the old stage keys of each rule, and the old action
// keys of each of their actions, which items lists.
func oldRuleKeys(rules []entry, items func(entry) []entry) []error {
	var errs []error
	for _, rule := range rules {
		errs = append(errs, oldKeysOf(rule, oldStageKeys())...)
		for _, e := range mappingKeys(rule) {
			if e.key.Value == "actions" {
				for _, action := range items(e) {
					errs = append(errs, oldKeysOf(action, oldActionKeys())...)
				}
			}
		}
	}
	return errs
}

// oldKeysOf reports each key of e's mapping that table holds, with its
// replacement.
func oldKeysOf(e entry, table map[string]string) []error {
	var errs []error
	for _, k := range mappingKeys(e) {
		if text, ok := table[k.key.Value]; ok {
			errs = append(errs, keyError(k.path, k.key.Line, text))
		}
	}
	return errs
}

// mappingKeys lists the keys of e's value when it is a mapping, and nothing
// otherwise.
func mappingKeys(e entry) []entry {
	if e.value.Kind != yaml.MappingNode {
		return nil
	}
	return entries(e.value, e.path)
}

// sequenceItems lists the items of e's value when it is a sequence, each
// without a key and with its index in its path, and nothing otherwise.
func sequenceItems(e entry) []entry {
	if e.value.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]entry, len(e.value.Content))
	for i, item := range e.value.Content {
		out[i] = entry{value: item, path: fmt.Sprintf("%s[%d]", e.path, i)}
	}
	return out
}
