// Command runwasm executes a WebAssembly module the way the application does,
// so scripts/check-lessons.sh verifies lesson output against the same runtime
// that will grade a learner's submission rather than an approximation of it.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/FACorreiaa/seshat/internal/executor"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: runwasm <module.wasm>")
		os.Exit(2)
	}

	module, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	result, err := executor.RunModule(context.Background(), module)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Print(result.Stdout)
	fmt.Fprint(os.Stderr, result.Stderr)
	if result.TimedOut {
		fmt.Fprintln(os.Stderr, "timed out")
		os.Exit(1)
	}
	os.Exit(int(result.ExitCode))
}
