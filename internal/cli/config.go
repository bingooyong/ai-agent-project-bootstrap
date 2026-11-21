package cli

import (
	"github.com/spf13/cobra"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/updater"
)

func addConfigFlag(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().String("config", "", "select an existing configuration file relative to the project root")
	return cmd
}

func configSelection(cmd *cobra.Command) (string, error) {
	value, err := cmd.Flags().GetString("config")
	if err != nil {
		return "", err
	}
	if cmd.Flags().Changed("config") && value == "" {
		return "", &commandError{2, "config requires a non-empty relative file path"}
	}
	if err := updater.ValidateConfigPath(value); err != nil {
		return "", &commandError{2, err.Error()}
	}
	return value, nil
}

func projectConfig(root, selection string) (updater.Config, error) {
	r, err := safeio.Open(root)
	if err != nil {
		return updater.Config{}, &commandError{3, "could not open project"}
	}
	defer r.Close()
	c, _, err := updater.LoadConfig(r, selection)
	if err != nil {
		return updater.Config{}, &commandError{2, "project configuration is missing, unsafe or invalid"}
	}
	return c, nil
}
