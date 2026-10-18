package config

import (
	"fmt"
	"time"
)

func parseQuerySessionTTL(raw string) (time.Duration, error) {
	v, e := time.ParseDuration(raw)
	if e != nil || v < time.Minute || v > 24*time.Hour || v%time.Second != 0 {
		return 0, fmt.Errorf("QUERY_SESSION_TTL must be whole seconds between 60s and 24h")
	}
	return v, nil
}
