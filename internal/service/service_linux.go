//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const unitName = "myshare.service"

func unitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", unitName), nil
}

func platformInstall(o Options) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)

	args := []string{"--host", o.Host, "--port", strconv.Itoa(o.Port), "--data-dir", o.DataDir}
	if o.ConfigFile != "" {
		args = append(args, "--config", o.ConfigFile)
	}
	if o.Auth {
		args = append(args, "--auth")
	}
	if o.TLS {
		args = append(args, "--tls")
	}

	unit := fmt.Sprintf(`[Unit]
Description=MyShare self-hosted file and clipboard transfer
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s %s
WorkingDirectory=%s
Restart=on-failure
RestartSec=3
# Hardening (user services still benefit).
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=default.target
`, quote(exe), strings.Join(quoteAll(args), " "), quote(o.DataDir))

	p, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(unit), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", p)

	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "--user", "enable", "--now", unitName); err != nil {
		return err
	}
	fmt.Println("service installed and started.")
	fmt.Println()
	fmt.Println("Tip: so it keeps running after you log out, enable lingering once:")
	fmt.Println("    loginctl enable-linger $USER")
	return nil
}

func platformUninstall() error {
	_ = run("systemctl", "--user", "disable", "--now", unitName)
	p, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = run("systemctl", "--user", "daemon-reload")
	fmt.Println("service removed.")
	return nil
}

func platformStart() error   { return run("systemctl", "--user", "start", unitName) }
func platformStop() error    { return run("systemctl", "--user", "stop", unitName) }
func platformRestart() error { return run("systemctl", "--user", "restart", unitName) }

func platformStatus() (string, error) {
	out, err := exec.Command("systemctl", "--user", "--no-pager", "status", unitName).CombinedOutput()
	if len(out) > 0 {
		return string(out), nil
	}
	return "", err
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func quote(s string) string {
	if strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = quote(s)
	}
	return out
}
