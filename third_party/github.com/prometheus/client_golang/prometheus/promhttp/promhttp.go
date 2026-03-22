package promhttp

import (
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

type HandlerOpts struct{}

func HandlerFor(reg *prometheus.Registry, _ HandlerOpts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(strings.Join(reg.Gather(), "\n") + "\n"))
	})
}
