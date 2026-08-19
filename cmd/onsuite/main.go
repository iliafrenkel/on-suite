// Command onsuite is the single binary serving the whole ON Suite.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// version is overwritten at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "onsuite:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, errOut io.Writer) error {
	if len(args) == 0 {
		usage(errOut)
		return errors.New("no command given")
	}
	switch args[0] {
	case "serve":
		return serve(args[1:], getenv, errOut)
	case "user":
		return userCmd(args[1:], getenv, errOut)
	case "version":
		fmt.Println("onsuite", version)
		return nil
	case "help", "-h", "--help":
		usage(errOut)
		return nil
	default:
		usage(errOut)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `onsuite — the ON Suite server

Usage:
  onsuite serve [flags]     run the server
  onsuite user add <name>   create an account
  onsuite version           print the build version
  onsuite help              show this message

Run "onsuite serve -h" for serve flags.
`)
}

// registeredApps is the definitive list of applications in this binary.
//
// Adding an app is one line here plus its package. Nothing else in the
// platform needs to change, and reading this function tells you exactly what
// this build contains.
func registeredApps() []app.App {
	return []app.App{
		paste.New(),
	}
}
