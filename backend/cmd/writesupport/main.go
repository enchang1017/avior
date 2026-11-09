// Command writesupport writes deploy/governed/write-support.sql from the list
// the provisioning slice runs. FEAT-076.
//
// Run by `make gen`, and checked by the drift gate like every other generated
// file: a psql script and the statements Avior sends are one definition, and
// the only way to keep a copy honest is to make it from the original.
package main

import (
	"fmt"
	"os"

	"github.com/enchang1017/avior/backend/internal/feature/provision"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <output.sql>\n", os.Args[0])
		os.Exit(64)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "writesupport: %v\n", err)
		os.Exit(1)
	}
}

func run(path string) error {
	script, err := provision.WriteSupportScript()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
