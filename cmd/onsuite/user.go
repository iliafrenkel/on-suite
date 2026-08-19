package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

func userCmd(args []string, getenv func(string) string, errOut io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(errOut, "usage: onsuite user add <username> [--admin] [--data-dir DIR]\n")
		return errors.New("user: no subcommand given")
	}
	switch args[0] {
	case "add":
		return userAdd(args[1:], getenv, os.Stdin, os.Stdout, errOut)
	default:
		return fmt.Errorf("user: unknown subcommand %q", args[0])
	}
}

func userAdd(args []string, getenv func(string) string, in *os.File, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("user add", flag.ContinueOnError)
	fs.SetOutput(errOut)
	admin := fs.Bool("admin", false, "grant administrator rights")
	dataDir := fs.String("data-dir", envOrDefault(getenv, "ONSUITE_DATA_DIR", "./data"),
		"directory holding the database")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("user add: exactly one username is required")
	}
	username := positional[0]
	if err := auth.ValidateUsername(username); err != nil {
		return err
	}

	password, err := readPassword(in, out)
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	cfg := config.Config{DataDir: *dataDir}
	ctx := context.Background()
	handle, _, _, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()

	user, err := auth.NewStore(handle).CreateUser(ctx, username, hash, *admin)
	if err != nil {
		return err
	}

	role := "user"
	if user.IsAdmin {
		role = "administrator"
	}
	fmt.Fprintf(out, "Created %s %q (id %d) in %s\n", role, user.Username, user.ID, cfg.DBPath())
	return nil
}

// readPassword takes the password from in, never from a flag: a flag value is
// visible in ps output and in shell history.
//
// On a terminal it prompts twice with echo disabled. Otherwise it reads a
// single line, so "onsuite user add ilia < secret.txt" works unattended.
func readPassword(in *os.File, out io.Writer) (string, error) {
	if !term.IsTerminal(int(in.Fd())) {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		password := strings.TrimRight(line, "\r\n")
		if password == "" {
			return "", errors.New("no password supplied on stdin")
		}
		return password, nil
	}

	fmt.Fprintf(out, "Password (at least %d characters): ", auth.MinPasswordLength)
	first, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	fmt.Fprint(out, "Repeat password: ")
	second, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("read password confirmation: %w", err)
	}

	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return string(first), nil
}

// envOrDefault mirrors config's precedence for the commands that take only a
// data directory and do not need the full Config.
func envOrDefault(getenv func(string) string, key, def string) string {
	if getenv == nil {
		return def
	}
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

// parseInterspersed parses flags that may appear before or after positional
// arguments, and returns the positional ones.
//
// Go's flag package stops parsing at the first non-flag argument. A plain
// fs.Parse would therefore treat "--admin" as a positional argument in
// "onsuite user add ilia --admin" — which is the form the spec itself
// documents — and fail with a confusing "exactly one username is required".
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
}
