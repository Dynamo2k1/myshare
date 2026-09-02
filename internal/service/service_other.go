//go:build !linux && !darwin && !windows

package service

import "errors"

var errUnsupported = errors.New("service management is not supported on this platform")

func platformInstall(Options) error   { return errUnsupported }
func platformUninstall() error        { return errUnsupported }
func platformStart() error            { return errUnsupported }
func platformStop() error             { return errUnsupported }
func platformRestart() error          { return errUnsupported }
func platformStatus() (string, error) { return "", errUnsupported }
