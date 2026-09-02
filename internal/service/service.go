// Package service installs MyShare as a per-user background service on Linux
// (systemd user unit), macOS (launchd LaunchAgent) and Windows (Scheduled Task
// at logon). No implementation requires administrator privileges.
package service

import "os"

// Options are the settings baked into the generated service definition.
type Options struct {
	Host       string
	Port       int
	DataDir    string
	ConfigFile string
	Auth       bool
	TLS        bool
}

// Install writes and enables the service definition for the current OS. It
// creates the data directory first so the service manager can set it as the
// working directory without a chdir failure on first start.
func Install(o Options) error {
	if o.DataDir != "" {
		if err := os.MkdirAll(o.DataDir, 0o755); err != nil {
			return err
		}
	}
	return platformInstall(o)
}

// Uninstall stops and removes the service definition.
func Uninstall() error { return platformUninstall() }

// Start starts the installed service.
func Start() error { return platformStart() }

// Stop stops the installed service.
func Stop() error { return platformStop() }

// Restart restarts the installed service.
func Restart() error { return platformRestart() }

// Status returns a human-readable status string.
func Status() (string, error) { return platformStatus() }
