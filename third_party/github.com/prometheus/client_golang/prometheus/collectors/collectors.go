package collectors

import (
	"fmt"
	"os"
	"runtime"
)

type noOpCollector struct {
	name  string
	value func() float64
}

func (c noOpCollector) Collect() []string {
	return []string{fmt.Sprintf("# HELP %s generated collector", c.name), fmt.Sprintf("# TYPE %s gauge", c.name), fmt.Sprintf("%s %g", c.name, c.value())}
}
func NewGoCollector() interface{ Collect() []string } {
	return noOpCollector{name: "go_goroutines", value: func() float64 { return float64(runtime.NumGoroutine()) }}
}

type ProcessCollectorOpts struct{}

func NewProcessCollector(ProcessCollectorOpts) interface{ Collect() []string } {
	return noOpCollector{name: "process_pid", value: func() float64 { return float64(os.Getpid()) }}
}
