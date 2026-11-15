// Package api exposes the manager over HTTP and serves the web UI.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Mason0920/k8s-cronjob-manager/internal/manager"
)

// Service is the subset of *manager.Manager the handlers use. Tests can
// pass the real manager backed by a fake clientset.
type Service interface {
	Namespaces() []string
	ReadOnly() bool
	LabelSelector() string
	ListCronJobs(ctx context.Context, namespace string) ([]manager.CronJobSummary, error)
	GetCronJob(ctx context.Context, namespace, name string) (manager.CronJobDetail, error)
	RunNow(ctx context.Context, namespace, name string) (manager.JobSummary, error)
	SetSuspend(ctx context.Context, namespace, name string, suspend bool) (manager.CronJobDetail, error)
	ListJobs(ctx context.Context, namespace, name string, limit int) ([]manager.JobSummary, error)
	RecentJobs(ctx context.Context, limit int) ([]manager.JobSummary, error)
	JobLogs(ctx context.Context, namespace, cronJob, job string, opts manager.LogOptions) (manager.LogResult, error)
}

// Options configures the HTTP handler.
type Options struct {
	// AuthToken, when set, is required as "Authorization: Bearer <token>"
	// on every /api/ request.
	AuthToken string
	// UI is the built web UI. nil disables UI serving.
	UI fs.FS
	// Logger receives access and audit logs. Defaults to slog.Default().
	Logger *slog.Logger
}

type server struct {
	svc    Service
	token  []byte
	log    *slog.Logger
	static http.Handler
}

// NewHandler returns the full HTTP handler: API routes, /healthz and the UI.
func NewHandler(svc Service, opts Options) http.Handler {
	s := &server{svc: svc, log: opts.Logger}
	if s.log == nil {
		s.log = slog.Default()
	}
	if opts.AuthToken != "" {
		s.token = []byte(opts.AuthToken)
	}
	if opts.UI != nil {
		s.static = spaHandler(opts.UI)
	}

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/config", s.getConfig)
	api.HandleFunc("GET /api/v1/cronjobs", s.listCronJobs)
	api.HandleFunc("GET /api/v1/jobs", s.recentJobs)
	api.HandleFunc("GET /api/v1/namespaces/{namespace}/cronjobs/{name}", s.getCronJob)
	api.HandleFunc("POST /api/v1/namespaces/{namespace}/cronjobs/{name}/run", s.runNow)
	api.HandleFunc("POST /api/v1/namespaces/{namespace}/cronjobs/{name}/suspend", s.suspend(true))
	api.HandleFunc("POST /api/v1/namespaces/{namespace}/cronjobs/{name}/resume", s.suspend(false))
	api.HandleFunc("GET /api/v1/namespaces/{namespace}/cronjobs/{name}/jobs", s.listJobs)
	api.HandleFunc("GET /api/v1/namespaces/{namespace}/cronjobs/{name}/jobs/{job}/logs", s.jobLogs)

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	root.Handle("/api/", s.requireToken(api))
	if s.static != nil {
		root.Handle("/", s.static)
	}

	// Browsers send Sec-Fetch-Site / Origin; cross-site POSTs are rejected
	// so a page on another origin cannot trigger a run with the user's
	// cookies when this sits behind a cookie-based proxy.
	csrf := http.NewCrossOriginProtection()
	return s.recoverer(s.accessLog(csrf.Handler(root)))
}

func (s *server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"namespaces":    s.svc.Namespaces(),
		"readOnly":      s.svc.ReadOnly(),
		"labelSelector": s.svc.LabelSelector(),
	})
}

func (s *server) listCronJobs(w http.ResponseWriter, r *http.Request) {
	items, err := s.svc.ListCronJobs(r.Context(), r.URL.Query().Get("namespace"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) recentJobs(w http.ResponseWriter, r *http.Request) {
	limit, err := intParam(r, "limit", 20)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	items, err := s.svc.RecentJobs(r.Context(), limit)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) getCronJob(w http.ResponseWriter, r *http.Request) {
	d, err := s.svc.GetCronJob(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) runNow(w http.ResponseWriter, r *http.Request) {
	ns, name := r.PathValue("namespace"), r.PathValue("name")
	job, err := s.svc.RunNow(r.Context(), ns, name)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.log.Info("audit: cronjob run now", "namespace", ns, "cronjob", name, "job", job.Name, "remote", r.RemoteAddr)
	writeJSON(w, http.StatusCreated, job)
}

func (s *server) suspend(value bool) http.HandlerFunc {
	action := "resume"
	if value {
		action = "suspend"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ns, name := r.PathValue("namespace"), r.PathValue("name")
		d, err := s.svc.SetSuspend(r.Context(), ns, name, value)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		s.log.Info("audit: cronjob "+action, "namespace", ns, "cronjob", name, "remote", r.RemoteAddr)
		writeJSON(w, http.StatusOK, d)
	}
}

func (s *server) listJobs(w http.ResponseWriter, r *http.Request) {
	limit, err := intParam(r, "limit", 20)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	items, err := s.svc.ListJobs(r.Context(), r.PathValue("namespace"), r.PathValue("name"), limit)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) jobLogs(w http.ResponseWriter, r *http.Request) {
	tail, err := intParam(r, "tailLines", 0)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	q := r.URL.Query()
	res, err := s.svc.JobLogs(r.Context(), r.PathValue("namespace"), r.PathValue("name"), r.PathValue("job"), manager.LogOptions{
		Pod:       q.Get("pod"),
		Container: q.Get("container"),
		TailLines: int64(tail),
	})
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// requireToken checks the bearer token when one is configured.
func (s *server) requireToken(next http.Handler) http.Handler {
	if s.token == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), s.token) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="k8s-cronjob-manager"`)
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeErr maps manager errors to HTTP status codes. Unknown errors are
// logged in full but returned to the client as a generic message.
func (s *server) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, manager.ErrNamespaceNotAllowed), errors.Is(err, manager.ErrReadOnly):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, manager.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, manager.ErrConcurrencyForbidden), errors.Is(err, manager.ErrAlreadyExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, manager.ErrInvalidArgument):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, context.Canceled):
		// client went away; nothing useful to send
	default:
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error, see server log")
	}
}

func intParam(r *http.Request, key string, def int) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, errors.Join(manager.ErrInvalidArgument, errors.New(key+" must be a non-negative integer"))
	}
	return n, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" {
			return
		}
		s.log.Debug("http request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration", time.Since(start).String(), "remote", r.RemoteAddr)
	})
}

func (s *server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic in handler", "path", r.URL.Path, "panic", v)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
