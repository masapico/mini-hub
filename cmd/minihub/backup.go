package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/masapico/minihub/internal/config"
	"github.com/masapico/minihub/internal/storage/backend"
)

func backup(args []string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	configPath := flags.String("config", config.DefaultFilename, "configuration file")
	data := flags.String("data", "", "persistent data directory")
	out := flags.String("out", "", "destination backup database file (required)")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *out == "" {
		return errors.New("usage: minihub backup [-config file] [-data directory] -out backup.db")
	}

	explicit := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	dataDir := *data
	if !explicit["data"] {
		cfg, loaded, err := config.Load(*configPath, explicit["config"])
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}
		if loaded && cfg.Server.DataDir != "" {
			dataDir = cfg.Server.DataDir
		} else {
			dataDir = "data"
		}
	}

	absOut, err := filepath.Abs(*out)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}

	start := time.Now()
	if err := backend.Backup(dataDir, absOut); err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	duration := time.Since(start)

	info, err := os.Stat(absOut)
	if err != nil {
		return fmt.Errorf("inspect backup output: %w", err)
	}

	fmt.Printf("Backup created: %s (size: %d bytes, elapsed: %v)\n", absOut, info.Size(), duration.Round(time.Millisecond))
	return nil
}

