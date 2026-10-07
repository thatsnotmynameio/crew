package crew

import (
	"cmp"
	"strconv"
	"uuid"
)

// RuleName names a rule, as the config's rules key it.
type RuleName string

// ActionName names an action within its rule.
type ActionName string

// CheckName names a check, as the config's checks key it.
type CheckName string

// AgentName names an agent, as the config's agents key it.
type AgentName string

// BotName names one of crew's bots, as the config names it.
type BotName string

// HarnessName names a harness adapter, such as "claude".
type HarnessName string

// QueueName names a queue: DefaultQueue or a queue the config declares.
type QueueName string

// WorkspaceName names a workspace. It is unique among the workspaces that
// exist and safe in a file name.
type WorkspaceName string

// RepositoryID identifies a tracker's repository, by an identity that
// survives a rename: a node id on GitHub. It is opaque to the engine.
type RepositoryID string

// Repository is the repository a tracker works on: its identity, and its
// name as humans write it, such as "owner/name" on GitHub.
type Repository struct {
	ID   RepositoryID
	Name string
}

// IssueID identifies an issue everywhere: its repository, and the opaque
// key its tracker knows it by, such as "42" on GitHub. Two repositories'
// issues with the same key are two issues. It is comparable, so it keys
// maps.
type IssueID struct {
	Repository RepositoryID
	Key        string
}

// String returns the key, the issue's display form within its repository,
// so "#%s" of an IssueID reads as "#42".
func (id IssueID) String() string { return id.Key }

// Compare orders issue ids by repository, then by key as text, as
// cmp.Compare orders values: -1, 0 or +1.
func (id IssueID) Compare(o IssueID) int {
	return cmp.Or(cmp.Compare(id.Repository, o.Repository), cmp.Compare(id.Key, o.Key))
}

// RuleRunID identifies one rule run: no other rule run has it, in any
// repository or crew process. It is opaque; compare it only as text.
type RuleRunID string

// NewRuleRunID returns the id of the run that is the nth, counting from 1,
// among the runs one input of the core started, from the fresh seed the
// engine stamped on that input. The same seed and n give the same id.
func NewRuleRunID(seed uuid.UUID, n int) RuleRunID {
	return RuleRunID(seed.String() + "." + strconv.Itoa(n))
}

// PullRequestReportID identifies a pull request report: the run whose move
// it reports, and which move. It stays the same across the report's
// retries and across crew processes.
type PullRequestReportID string

// TakeReport returns the id of the report that follows the run's take.
func (r RuleRunID) TakeReport() PullRequestReportID {
	return PullRequestReportID(string(r) + "/take")
}

// EndingReport returns the id of the report that follows the final move
// of the run's route.
func (r RuleRunID) EndingReport() PullRequestReportID {
	return PullRequestReportID(string(r) + "/ending")
}
