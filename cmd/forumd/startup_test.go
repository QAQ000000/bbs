package main

import (
	"testing"
	"time"
)

func TestMaintenanceFlagsCannotStartSeedOrOtherActions(t *testing.T) {
	for flags := 0; flags < 32; flags++ {
		count := 0
		for bit := 0; bit < 5; bit++ {
			if flags&(1<<bit) != 0 {
				count++
			}
		}
		err := validateStartupFlags(flags&1 != 0, flags&2 != 0, flags&4 != 0, flags&8 != 0, flags&16 != 0, time.Minute)
		if (err != nil) != (count > 1) {
			t.Fatalf("flags=%d err=%v", flags, err)
		}
	}
	if err := validateStartupFlags(false, false, true, false, false, 0); err == nil {
		t.Fatal("unbounded migration timeout accepted")
	}
}
