package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	dataDir := flag.String("data-dir", "", "empty directory to create the demo database in")
	version := flag.String("version", "", "the version the server will run as (no leading v); the demo accounts start having seen its changelog, so screenshots show no What's new dot")
	flag.Parse()
	if *dataDir == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./docs/screenshots/seed --data-dir DIR [--version X.Y.Z]")
		os.Exit(2)
	}
	if _, err := Seed(context.Background(), *dataDir, time.Now().UTC(), *version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("seeded %s — sign in as demo / %s\n", *dataDir, Password)
}
