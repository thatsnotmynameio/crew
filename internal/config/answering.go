package config

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	// appSuffix ends every App's login on GitHub: <slug>[bot].
	appSuffix = "[bot]"
	// githubActions is the App every workflow comments as, which can post
	// anyone's text, so it never answers (R39).
	githubActions = "github-actions[bot]"
	// githubActionsRefusal says why githubActions is refused wherever a
	// login that answers is written.
	githubActionsRefusal = githubActions + " never answers: any workflow can post anyone's text as it"
)

// answeringApps reads the optional answering_apps n into cfg: a list of App
// logins, each ending with [bot] and none github-actions[bot] in any case
// (KTD-W4). A list written, [] included, is the answering list; left out,
// cfg keeps none and AnsweringAppsWritten false, so crew's bots answer.
func answeringApps(n *yaml.Node, cfg *Config) error {
	const path = "answering_apps"
	switch n.Kind {
	case 0:
		return nil
	case yaml.SequenceNode:
	default:
		return keyError(path, n.Line, "must be a list of App logins, such as claude[bot]")
	}
	var errs []error
	apps := make([]string, 0, len(n.Content))
	for _, e := range sequenceItems(entry{value: n, path: path}) {
		login, err := appLogin(e)
		errs = append(errs, err)
		apps = append(apps, login)
	}
	cfg.AnsweringApps, cfg.AnsweringAppsWritten = apps, true
	return errors.Join(errs...)
}

// appLogin returns the App login the item e of answering_apps holds, and
// what is wrong with it.
func appLogin(e entry) (string, error) {
	v := resolve(e.value)
	if v.Kind != yaml.ScalarNode {
		return "", keyError(e.path, v.Line, "must be an App's login, such as claude[bot]")
	}
	login := v.Value
	lower := strings.ToLower(login)
	switch {
	case lower == githubActions:
		return "", keyError(e.path, v.Line, githubActionsRefusal)
	case !strings.HasSuffix(lower, appSuffix) || len(login) == len(appSuffix):
		return "", keyError(e.path, v.Line, fmt.Sprintf("%q is not an App's login: it must be <slug>[bot]", login))
	}
	return login, nil
}
