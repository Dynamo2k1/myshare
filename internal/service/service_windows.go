//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const taskName = "MyShare"

func platformInstall(o Options) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

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
	// schtasks wants the whole command as one /TR string.
	tr := fmt.Sprintf(`"%s" %s`, exe, strings.Join(args, " "))

	create := exec.Command("schtasks", "/Create", "/F",
		"/SC", "ONLOGON",
		"/TN", taskName,
		"/TR", tr,
		"/RL", "LIMITED")
	create.Stdout, create.Stderr = os.Stdout, os.Stderr
	if err := create.Run(); err != nil {
		return fmt.Errorf("schtasks create: %w", err)
	}
	fmt.Printf("scheduled task %q created (runs at logon).\n", taskName)
	return platformStart()
}

func platformUninstall() error {
	_ = platformStop()
	c := exec.Command("schtasks", "/Delete", "/F", "/TN", taskName)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func platformStart() error {
	c := exec.Command("schtasks", "/Run", "/TN", taskName)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func platformStop() error {
	// End any running instance; ignore "not running".
	_ = exec.Command("schtasks", "/End", "/TN", taskName).Run()
	return nil
}

func platformRestart() error {
	_ = platformStop()
	return platformStart()
}

func platformStatus() (string, error) {
	out, err := exec.Command("schtasks", "/Query", "/TN", taskName, "/FO", "LIST").CombinedOutput()
	if err != nil {
		return "not installed", nil
	}
	return string(out), nil
}

var _ = filepath.Separator
