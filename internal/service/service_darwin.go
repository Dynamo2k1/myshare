//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const label = "com.myshare.agent"

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

func platformInstall(o Options) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)

	args := []string{exe, "--host", o.Host, "--port", strconv.Itoa(o.Port), "--data-dir", o.DataDir}
	if o.ConfigFile != "" {
		args = append(args, "--config", o.ConfigFile)
	}
	if o.Auth {
		args = append(args, "--auth")
	}
	if o.TLS {
		args = append(args, "--tls")
	}

	var argXML string
	for _, a := range args {
		argXML += "\n    <string>" + xmlEscape(a) + "</string>"
	}
	logDir := filepath.Join(o.DataDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array>%s
  </array>
  <key>WorkingDirectory</key><string>%s</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s/myshare.out.log</string>
  <key>StandardErrorPath</key><string>%s/myshare.err.log</string>
</dict>
</plist>
`, label, argXML, xmlEscape(o.DataDir), logDir, logDir)

	p, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(plist), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", p)
	_ = exec.Command("launchctl", "unload", p).Run()
	if err := exec.Command("launchctl", "load", p).Run(); err != nil {
		return err
	}
	fmt.Println("service installed and started.")
	return nil
}

func platformUninstall() error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", p).Run()
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("service removed.")
	return nil
}

func platformStart() error {
	p, _ := plistPath()
	return exec.Command("launchctl", "load", p).Run()
}

func platformStop() error {
	p, _ := plistPath()
	return exec.Command("launchctl", "unload", p).Run()
}

func platformRestart() error {
	_ = platformStop()
	return platformStart()
}

func platformStatus() (string, error) {
	out, _ := exec.Command("launchctl", "list").CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, label) {
			return "loaded: " + strings.TrimSpace(line), nil
		}
	}
	return "not loaded", nil
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
