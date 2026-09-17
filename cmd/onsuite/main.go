// Command onsuite is the single binary serving the whole ON Suite.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/apps/reader"
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
	case "export":
		return exportCmd(args[1:], getenv, os.Stdout, errOut)
	case "backup":
		return backupCmd(args[1:], getenv, os.Stdout, errOut)
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
  onsuite export <name>     write a user's data as JSON
  onsuite backup            write a database snapshot
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
		flash.New(),
		notes.New(),
		paste.New(),
		reader.New(),
	}
}

// filterApps returns the apps that should be part of this run's registry:
// every registered app, minus the ones named in disabled. It is how
// -disable-apps/ONSUITE_DISABLE_APPS takes effect — see openDatabase, the
// only caller.
//
// An unknown ID is almost certainly a typo, so it is a startup error rather
// than a silent no-op. A duplicate ID is harmless (disabled is treated as a
// set) and not an error. Ending up with zero apps is also an error: a binary
// with nothing registered is never an intentional configuration.
func filterApps(all []app.App, disabled []string) ([]app.App, error) {
	if len(disabled) == 0 {
		return all, nil
	}

	off := make(map[string]bool, len(disabled))
	known := make([]string, len(all))
	for i, a := range all {
		known[i] = a.Meta().ID
	}
	for _, id := range disabled {
		found := false
		for _, k := range known {
			if k == id {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("disable-apps: unknown app %q (known apps: %s)", id, strings.Join(known, ", "))
		}
		off[id] = true
	}

	kept := make([]app.App, 0, len(all))
	for _, a := range all {
		if !off[a.Meta().ID] {
			kept = append(kept, a)
		}
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("disable-apps: disables every registered app (%s); refusing to run with none", strings.Join(known, ", "))
	}
	return kept, nil
}
