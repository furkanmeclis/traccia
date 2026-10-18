package config

import (
	"testing"
	"time"
)

func TestQuerySessionTTLConfiguration(t *testing.T) {
	for _, raw := range []string{"60s", "30m", "24h"} {
		t.Run(raw, func(t *testing.T) {
			v, e := parseQuerySessionTTL(raw)
			if e != nil || v < time.Minute || v > 24*time.Hour {
				t.Fatal(v, e)
			}
		})
	}
	for _, raw := range []string{"invalid", "0s", "59s", "24h1s", "60.5s"} {
		t.Run(raw, func(t *testing.T) {
			if _, e := parseQuerySessionTTL(raw); e == nil {
				t.Fatal("invalid TTL accepted")
			}
		})
	}
}
