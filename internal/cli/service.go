package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ranauzair/myshare/internal/config"
	"github.com/ranauzair/myshare/internal/service"
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Install and manage MyShare as a background service (no root required)",
		Long: `Manage a per-user background service so MyShare starts automatically.

  Linux   -> systemd user unit  (~/.config/systemd/user/myshare.service)
  macOS   -> launchd LaunchAgent (~/Library/LaunchAgents/com.myshare.plist)
  Windows -> Scheduled Task      (runs at logon)

None of these require administrator privileges.`,
	}

	var (
		host, dataDir, port, config_ string
	)
	persist := func(c *cobra.Command) {
		c.Flags().StringVar(&host, "host", "", "host to bake into the service")
		c.Flags().StringVar(&port, "port", "", "port to bake into the service")
		c.Flags().StringVar(&dataDir, "data-dir", "", "data directory to bake into the service")
		c.Flags().StringVar(&config_, "config", "", "config file to reference")
	}

	install := &cobra.Command{
		Use:   "install",
		Short: "Install (and enable) the service",
		RunE: func(c *cobra.Command, _ []string) error {
			opts := serviceOptsFromFlags(host, port, dataDir, config_)
			return service.Install(opts)
		},
	}
	persist(install)

	simple := func(use, short string, fn func() error) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, RunE: func(*cobra.Command, []string) error { return fn() }}
	}

	cmd.AddCommand(install)
	cmd.AddCommand(simple("uninstall", "Stop and remove the service", service.Uninstall))
	cmd.AddCommand(simple("start", "Start the service", service.Start))
	cmd.AddCommand(simple("stop", "Stop the service", service.Stop))
	cmd.AddCommand(simple("restart", "Restart the service", service.Restart))
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show service status",
		RunE: func(*cobra.Command, []string) error {
			s, err := service.Status()
			if err != nil {
				return err
			}
			fmt.Println(s)
			return nil
		},
	})
	return cmd
}

func serviceOptsFromFlags(host, port, dataDir, cfgFile string) service.Options {
	// Resolve final values through the normal config precedence so the unit
	// records concrete settings rather than "whatever the env is later".
	var ov config.Overrides
	if host != "" {
		ov.Host = &host
	}
	if dataDir != "" {
		ov.DataDir = &dataDir
	}
	if cfgFile != "" {
		ov.ConfigFile = &cfgFile
	}
	if port != "" {
		ov.Port = new(int)
		fmt.Sscanf(port, "%d", ov.Port)
	}
	cfg, err := config.Load(ov)
	if err != nil {
		// Fall back to defaults; Install will still produce a usable unit.
		cfg = config.Defaults()
	}
	return service.Options{
		Host:       cfg.Host,
		Port:       cfg.Port,
		DataDir:    cfg.DataDir,
		ConfigFile: cfg.ConfigFilePath,
		Auth:       cfg.Auth,
		TLS:        cfg.TLS,
	}
}
