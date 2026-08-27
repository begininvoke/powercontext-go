package cli

import (
	"errors"
	"os"

	"github.com/ob-labs/powercontext-go/server"
	"github.com/spf13/cobra"
)

const (
	defaultMarketplaceSource = "ob-labs/powercontext-go"
	defaultMarketplaceRef    = "main"
	powerContextPlugin       = "powercontext"
	dshPluginName            = "powercontext-dsh"
)

func newSetupCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "setup", Short: "Install and configure PowerContext integrations."}
	command.AddCommand(
		newSetupCodexCommand(state),
		newSetupClaudeCodeCommand(state),
		newSetupDSHCommand(state),
		newSetupPiCommand(state),
		newSetupOpenCodeCommand(state),
		newSetupHermesCommand(state),
		newSetupOpenClawCommand(state),
	)
	return command
}
func prepareDataDirectory() (string, error) {
	directory, err := server.PowerContextDataDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", errors.New("cannot create PowerContext data directory")
	}
	return directory, nil
}
