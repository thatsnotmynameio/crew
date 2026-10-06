// Package acceptance holds crew's black-box acceptance suite: doubles for gh
// and claude that crew finds on PATH, a harness that runs the crew binary
// GoReleaser builds against them, and the scenarios. It is its own module and
// never imports crew's packages: the suite reaches crew only through the
// binary. See README.md.
package acceptance
