package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/AlexKris/sidervia/internal/buildinfo"
)

type Registry struct {
	ready      atomic.Bool
	active     atomic.Int64
	requests   [6]atomic.Uint64
	durationMS atomic.Uint64
	build      buildinfo.Info
}

func New(build buildinfo.Info) *Registry { return &Registry{build: build} }

func (r *Registry) SetReady(value bool) { r.ready.Store(value) }

func (r *Registry) StartRequest() func(status int, duration time.Duration) {
	r.active.Add(1)
	return func(status int, duration time.Duration) {
		r.active.Add(-1)
		class := status / 100
		if class < 1 || class > 5 {
			class = 0
		}
		r.requests[class].Add(1)
		milliseconds := duration.Milliseconds()
		if milliseconds < 0 {
			milliseconds = 0
		}
		r.durationMS.Add(uint64(milliseconds))
	}
}

func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprintln(w, "# HELP sidervia_build_info Build metadata for this Sidervia process.")
		_, _ = fmt.Fprintln(w, "# TYPE sidervia_build_info gauge")
		_, _ = fmt.Fprintf(w, "sidervia_build_info{version=%s,commit=%s} 1\n", strconv.Quote(r.build.Version), strconv.Quote(r.build.Commit))
		_, _ = fmt.Fprintln(w, "# HELP sidervia_ready Whether the process is ready to serve requests.")
		_, _ = fmt.Fprintln(w, "# TYPE sidervia_ready gauge")
		ready := 0
		if r.ready.Load() {
			ready = 1
		}
		_, _ = fmt.Fprintf(w, "sidervia_ready %d\n", ready)
		_, _ = fmt.Fprintln(w, "# HELP sidervia_http_active_requests Current HTTP requests.")
		_, _ = fmt.Fprintln(w, "# TYPE sidervia_http_active_requests gauge")
		_, _ = fmt.Fprintf(w, "sidervia_http_active_requests %d\n", r.active.Load())
		_, _ = fmt.Fprintln(w, "# HELP sidervia_http_requests_total Completed HTTP requests by status class.")
		_, _ = fmt.Fprintln(w, "# TYPE sidervia_http_requests_total counter")
		for class := 1; class <= 5; class++ {
			_, _ = fmt.Fprintf(w, "sidervia_http_requests_total{status_class=%q} %d\n", fmt.Sprintf("%dxx", class), r.requests[class].Load())
		}
		_, _ = fmt.Fprintln(w, "# HELP sidervia_http_request_duration_milliseconds_total Cumulative request duration.")
		_, _ = fmt.Fprintln(w, "# TYPE sidervia_http_request_duration_milliseconds_total counter")
		_, _ = fmt.Fprintf(w, "sidervia_http_request_duration_milliseconds_total %d\n", r.durationMS.Load())
	})
}
