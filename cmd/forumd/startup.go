package main

import (
	"fmt"
	"time"
)

func validateStartupFlags(seed, check, migrate, repair, importSmileys bool, timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("migration-timeout must be positive")
	}
	n := 0
	for _, enabled := range []bool{seed, check, migrate, repair, importSmileys} {
		if enabled {
			n++
		}
	}
	if n > 1 {
		return fmt.Errorf("seed, check-backup, migrate, enqueue-derived-repair and import-smileys are mutually exclusive")
	}
	return nil
}
