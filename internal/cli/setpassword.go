package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dynamo2k1/myshare/internal/auth"
	"github.com/dynamo2k1/myshare/internal/config"
	"github.com/dynamo2k1/myshare/internal/store"
)

func newSetPasswordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-password",
		Short: "Set or clear the login password (prompts interactively)",
		Long: `Set the password required when the server runs with --auth.

The password is read from the terminal, never from a flag or argument, and is
stored only as an argon2id hash in the database. Run with --clear to remove it.`,
		Args: cobra.NoArgs,
		RunE: runSetPassword,
	}
	cmd.Flags().String("data-dir", "", "data directory (default ~/MyShare)")
	cmd.Flags().String("config", "", "path to a TOML config file")
	cmd.Flags().Bool("clear", false, "remove the configured password")
	return cmd
}

func runSetPassword(cmd *cobra.Command, _ []string) error {
	var ov config.Overrides
	if cmd.Flags().Changed("data-dir") {
		v, _ := cmd.Flags().GetString("data-dir")
		ov.DataDir = &v
	}
	if cmd.Flags().Changed("config") {
		v, _ := cmd.Flags().GetString("config")
		ov.ConfigFile = &v
	}
	cfg, err := config.Load(ov)
	if err != nil {
		return err
	}

	db, err := store.Open(context.Background(), dbPath(cfg.DataDir), true)
	if err != nil {
		return err
	}
	defer db.Close()
	mgr := auth.New(db, true)

	if clear, _ := cmd.Flags().GetBool("clear"); clear {
		if err := mgr.SetPassword(context.Background(), ""); err != nil {
			return err
		}
		fmt.Println("Password cleared.")
		return nil
	}

	pw, err := readSecret("New password: ")
	if err != nil {
		return err
	}
	confirm, err := readSecret("Confirm password: ")
	if err != nil {
		return err
	}
	if pw != confirm {
		return errors.New("passwords do not match")
	}
	if err := mgr.SetPassword(context.Background(), pw); err != nil {
		return err
	}
	fmt.Println("Password set. Start the server with --auth to require it.")
	return nil
}

func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	// Non-interactive: read a line from stdin (e.g. piped in a script).
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
