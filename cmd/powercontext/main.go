// Command powercontext is the PowerContext server and command-line client.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/thunguo/powercontext-go/internal/cli"
)

var (
	version = "devel"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	command := cli.New(cli.VersionInfo{Version: version, Commit: commit, Date: date})
	if err := command.ExecuteContext(context.Background()); err != nil {
		if !cli.ErrorAlreadyReported(err) {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(cli.ExitCode(err))
	}
}
