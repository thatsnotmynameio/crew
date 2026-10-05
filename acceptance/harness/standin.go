package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

const (
	// standInName is the name the test binary plays the stand-in for crew
	// under, in the harness's own tests.
	standInName = "acceptance-standin"
	// standInLifetime bounds how long the stand-in runs when nothing stops
	// it.
	standInLifetime = 10 * time.Minute
)

// standIn stands in for crew in the harness's own tests, which run without
// a crew binary. Its arguments say what it does:
//
//	env                  print its environment, one variable per line, and exit 0
//	ignore-sigint        ignore SIGINT, print "ignoring SIGINT" and run until killed
//	run <program> <args> start program from PATH in a process group of its
//	                     own, as crew starts gh and claude, print "started
//	                     <program>", then run until SIGINT and exit 0
func standIn() int {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "%s: no command\n", standInName)
		return exitDoubleFailed
	}
	switch {
	case args[0] == "env":
		for _, kv := range os.Environ() {
			fmt.Fprintln(os.Stdout, kv)
		}
		return 0
	case args[0] == "ignore-sigint":
		signal.Ignore(syscall.SIGINT)
		fmt.Fprintln(os.Stdout, "ignoring SIGINT")
		time.Sleep(standInLifetime)
		return 0
	case args[0] == "run" && len(args) > 1:
		return standInRun(args[1], args[2:])
	}
	fmt.Fprintf(os.Stderr, "%s: unknown command %q\n", standInName, args)
	return exitDoubleFailed
}

// standInRun starts program with args, then waits for SIGINT and returns 0.
func standInRun(program string, args []string) int {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	//nolint:gosec // G204: the program its own test names, gh or claude, found on the scenario's PATH
	cmd := exec.CommandContext(context.Background(), program, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: start %s: %v\n", standInName, program, err)
		return exitDoubleFailed
	}
	go func() { _ = cmd.Wait() }()
	fmt.Fprintln(os.Stdout, "started", program)
	select {
	case <-interrupts:
	case <-time.After(standInLifetime):
	}
	return 0
}
