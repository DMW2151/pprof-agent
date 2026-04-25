package config

import "time"

const (
	DefaultAddr     = ":8082"
	DefaultLogPath  = "/tmp/pprof-mcp.log"
	DefaultTTL      = 168 * time.Hour
	DefaultCapacity = 1024
)
