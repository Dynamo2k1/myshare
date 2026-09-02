// Package cli is MyShare's command-line interface.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dynamo2k1/myshare/internal/api"
)

// Execute runs the root command. version is stamped from main via the linker.
func Execute(version string) {
	api.Version = version
	root := newRootCmd(version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "myshare",
		Short: "Self-hosted file, clipboard and screenshot transfer between your devices",
		Long: `MyShare is a single-binary, self-hosted web app for moving text, screenshots
and large files between your own devices over localhost or your LAN.

Run "myshare" with no arguments to start the server on 127.0.0.1:8787.
Pass a directory to serve its real contents in the Files tab:
    myshare ~/Downloads
    myshare . --ephemeral        # serve here; keep no state after exit`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd, args)
		},
	}
	root.SetVersionTemplate("myshare {{.Version}}\n")

	addServeFlags(root)
	root.AddCommand(newServiceCmd())
	root.AddCommand(newSetPasswordCmd())
	root.AddCommand(newUploadCmd())
	root.AddCommand(newClipboardCmd())

	return root
}
