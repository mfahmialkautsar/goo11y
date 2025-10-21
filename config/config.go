package config

import "io"

type Config struct {
	Logger   Logger
	Tracer   Tracer
	Profiler Profiler
}

type Logger struct {
	Level       string
	Environment string
	LokiURL     string
	LokiUser    string
	LokiPass    string
	Writers     []io.Writer
	ServiceName string
}

type Tracer struct {
	ServiceName string
	Endpoint    string
	Enabled     bool
}

type Profiler struct {
	Enabled   bool
	ServerURL string
}
