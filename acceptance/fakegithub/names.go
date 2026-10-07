package fakegithub

// The names of the fields and types that several of GitHub's replies
// share.
const (
	keyBody      = "body"
	keyCreatedAt = "createdAt"
	keyFirst     = "first"
	keyHead      = "headRefName"
	keyHTMLURL   = "html_url"
	keyLabels    = "labels"
	keyLogin     = "login"
	keyName      = "name"
	keyNumber    = "number"
	keyState     = "state"
	keyTitle     = "title"
	keyType      = "type"
	keyURL       = "url"
	typeUser     = "User"
)

// closedState is a closed issue's or pull request's state as GitHub's REST
// API and gh pr list's --state spell it.
const closedState = "closed"
