// Package upgrade holds what crew upgrade decides before it changes
// anything: the release it is asked for, and whether it may replace the
// running crew at all.
//
// ParseArgs reads crew upgrade's one optional argument, a release version
// written vX.Y.Z or X.Y.Z, and returns it as vX.Y.Z, or an EnvError, the
// error on which crew exits 2. Classify tells a release build, the only
// kind crew upgrade replaces, from a go install build and a local one.
package upgrade
