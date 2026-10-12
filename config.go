package main

// config.go - optional JSON configuration file. Every flag can be
// pre-set there (key = flag name without dashes); explicitly passed
// command line flags always win. Values may be strings, numbers or
// booleans.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

// applyConfig loads the config file and applies all values that were
// NOT given on the command line. A missing default file is silently
// ignored; a missing explicitly-passed file, or any parse error, is
// reported.
func applyConfig(path string, explicit bool) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !explicit && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	// Which flags were set on the command line?
	cliSet := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { cliSet[f.Name] = true })

	for k, v := range cfg {
		if cliSet[k] {
			continue // command line wins
		}
		if flag.Lookup(k) == nil {
			return fmt.Errorf("unknown option %q in %s", k, path)
		}
		var s string
		switch t := v.(type) {
		case string:
			s = t
		case bool:
			s = fmt.Sprintf("%v", t)
		case float64:
			s = fmt.Sprintf("%v", t)
		default:
			return fmt.Errorf("invalid value for %q in %s", k, path)
		}
		if err := flag.Set(k, s); err != nil {
			return fmt.Errorf("option %q in %s: %w", k, path, err)
		}
	}
	return nil
}
