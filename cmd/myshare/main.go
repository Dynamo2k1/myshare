// Command myshare is the MyShare server and CLI: a single self-hosted binary
// for transferring text, screenshots and large files between your own devices.
package main

import "github.com/ranauzair/myshare/internal/cli"

// version is overridden at build time with:
//
//	go build -ldflags "-X main.version=v1.2.3"
var version = "dev"

func main() {
	cli.Execute(version)
}
