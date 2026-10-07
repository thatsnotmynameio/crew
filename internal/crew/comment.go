package crew

import "time"

// Comment is one comment on an issue, as the tracker lists it: who wrote
// it, whether that author is an App, such as one of crew's bots, its body as
// written and when it was written.
type Comment struct {
	// Author is the login of the comment's author.
	Author string
	// App reports whether the author is an App rather than a person.
	App bool
	// Body is the comment's text, as its author wrote it.
	Body string
	// Created is when the comment was written, zero when the tracker does
	// not tell.
	Created time.Time
}
