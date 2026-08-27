// Command release builds auditable, deterministic PowerContext release bundles.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fatal(errors.New("usage: release <asset|package|metadata|checksum|verify> [flags]"))
	}
	var err error
	switch os.Args[1] {
	case "asset":
		err = runAsset(os.Args[2:], os.Stdout)
	case "package":
		err = runPackage(os.Args[2:], os.Stdout)
	case "metadata":
		err = runMetadata(os.Args[2:])
	case "checksum":
		err = runChecksum(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	default:
		err = fmt.Errorf("unknown release command %q", os.Args[1])
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "release:", err)
	os.Exit(1)
}
