package crew

// Answerers are who may answer a question a session asks on its issue: the
// code owners, and the Apps on the answering list. Collaborators do not
// answer, and neither does any other App (R37, R38).
type Answerers struct {
	// CodeOwners are the code owners' logins, those sessions get as
	// CREW_CODE_OWNERS.
	CodeOwners []string
	// Apps are the logins of the Apps that answer, each <slug>[bot]: the
	// config's answering_apps, or crew's own bots without it. It never
	// holds github-actions[bot], which can post anyone's text (R39).
	Apps []string
}
