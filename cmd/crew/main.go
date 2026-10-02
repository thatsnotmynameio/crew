// Command crew polls the issue tracker and runs each workflow stage's actions in
// coding-agent sessions, as configured in the repository's .crew/config.yaml.
package main

import "fmt"

// version is set at build time with -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	fmt.Println("crew", version)
}
