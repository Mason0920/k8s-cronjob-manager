package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/Mason0920/k8s-cronjob-manager/internal/manager"
)

const testToken = "test-token-0123456789"

var testNow = time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)

func newServer(t *testing.T, readOnly bool, token string) *httptest.Server {
	t.Helper()
	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Namespace: "batch", Name: "backup", UID: "uid-backup"},
		Spec: batchv1.CronJobSpec{
			Schedule: "*/5 * * * *",
			JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main", Image: "busybox:1.37"}}},
			}}},
		},
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Namespace: "batch", Name: "backup-1", UID: "uid-job",
		CreationTimestamp: metav1.NewTime(testNow.Add(-time.Hour)),
		OwnerReferences:   []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: "backup", UID: "uid-backup", Controller: ptr.To(true)}},
	}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "batch", Name: "backup-1-abcde",
			Labels:          map[string]string{"batch.kubernetes.io/job-name": "backup-1"},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "backup-1", UID: "uid-job", Controller: ptr.To(true)}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	client := fake.NewClientset(cj, job, pod)
	client.PrependReactor("get", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "log" {
			return false, nil, nil
		}
		return true, &runtime.Unknown{Raw: []byte("hello from job\n")}, nil
	})
	mgr := manager.New(client, manager.Options{
		Namespaces: []string{"batch"},
		ReadOnly:   readOnly,
		Now:        func() time.Time { return testNow },
	})
	ui := fstest.MapFS{
		"index.html":     {Data: []byte("<!doctype html><title>ui</title>")},
		"assets/app.js":  {Data: []byte("console.log(1)")},
		"assets/app.css": {Data: []byte("body{}")},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(NewHandler(mgr, Options{AuthToken: token, UI: ui, Logger: logger}))
	t.Cleanup(srv.Close)
	return srv
}

type reqOpt func(*http.Request)

func withToken(tok string) reqOpt {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

func do(t *testing.T, srv *httptest.Server, method, path string, opts ...reqOpt) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range opts {
		o(req)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, body
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return v
}

func TestHealthzNeedsNoToken(t *testing.T) {
	srv := newServer(t, false, testToken)
	code, _, body := do(t, srv, "GET", "/healthz")
	if code != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		t.Fatalf("healthz = %d %q", code, body)
	}
}

func TestBearerToken(t *testing.T) {
	srv := newServer(t, false, testToken)
	if code, h, _ := do(t, srv, "GET", "/api/v1/cronjobs"); code != http.StatusUnauthorized || h.Get("WWW-Authenticate") == "" {
		t.Fatalf("no token: %d", code)
	}
	if code, _, _ := do(t, srv, "GET", "/api/v1/cronjobs", withToken("wrong-token-0123456789")); code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", code)
	}
	code, _, body := do(t, srv, "GET", "/api/v1/cronjobs", withToken(testToken))
	if code != http.StatusOK {
		t.Fatalf("good token: %d %s", code, body)
	}
	items := decode[struct{ Items []manager.CronJobSummary }](t, body).Items
	if len(items) != 1 || items[0].Name != "backup" || items[0].NextScheduleTime == nil {
		t.Fatalf("items = %+v", items)
	}
}

func TestConfigEndpoint(t *testing.T) {
	srv := newServer(t, true, "")
	code, _, body := do(t, srv, "GET", "/api/v1/config")
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	cfg := decode[struct {
		Namespaces []string
		ReadOnly   bool
	}](t, body)
	if !cfg.ReadOnly || len(cfg.Namespaces) != 1 || cfg.Namespaces[0] != "batch" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestRunNowAndStatusCodes(t *testing.T) {
	srv := newServer(t, false, "")

	code, _, body := do(t, srv, "POST", "/api/v1/namespaces/batch/cronjobs/backup/run")
	if code != http.StatusCreated {
		t.Fatalf("run: %d %s", code, body)
	}
	job := decode[manager.JobSummary](t, body)
	if job.Name != "backup-manual-1794700800" || !job.Manual {
		t.Fatalf("job = %+v", job)
	}

	// Same second, same name: conflict instead of a silent duplicate.
	if code, _, _ := do(t, srv, "POST", "/api/v1/namespaces/batch/cronjobs/backup/run"); code != http.StatusConflict {
		t.Fatalf("second run: %d, want 409", code)
	}
	if code, _, _ := do(t, srv, "POST", "/api/v1/namespaces/kube-system/cronjobs/backup/run"); code != http.StatusForbidden {
		t.Fatalf("other namespace: %d, want 403", code)
	}
	if code, _, _ := do(t, srv, "POST", "/api/v1/namespaces/batch/cronjobs/missing/run"); code != http.StatusNotFound {
		t.Fatalf("missing: %d, want 404", code)
	}
	if code, _, _ := do(t, srv, "GET", "/api/v1/namespaces/batch/cronjobs/backup/run"); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET on run: %d, want 405", code)
	}
}

func TestReadOnlyRejectsWrites(t *testing.T) {
	srv := newServer(t, true, "")
	for _, p := range []string{"run", "suspend", "resume"} {
		code, _, body := do(t, srv, "POST", "/api/v1/namespaces/batch/cronjobs/backup/"+p)
		if code != http.StatusForbidden || !strings.Contains(string(body), "read-only") {
			t.Fatalf("%s: %d %s, want 403 read-only", p, code, body)
		}
	}
	if code, _, _ := do(t, srv, "GET", "/api/v1/namespaces/batch/cronjobs/backup"); code != http.StatusOK {
		t.Fatalf("read in read-only mode: %d", code)
	}
}

func TestSuspendResume(t *testing.T) {
	srv := newServer(t, false, "")
	code, _, body := do(t, srv, "POST", "/api/v1/namespaces/batch/cronjobs/backup/suspend")
	if code != http.StatusOK || !decode[manager.CronJobDetail](t, body).Suspend {
		t.Fatalf("suspend: %d %s", code, body)
	}
	code, _, body = do(t, srv, "POST", "/api/v1/namespaces/batch/cronjobs/backup/resume")
	if code != http.StatusOK || decode[manager.CronJobDetail](t, body).Suspend {
		t.Fatalf("resume: %d %s", code, body)
	}
}

func TestCrossOriginPostIsRejected(t *testing.T) {
	srv := newServer(t, false, "")
	code, _, _ := do(t, srv, "POST", "/api/v1/namespaces/batch/cronjobs/backup/run", func(r *http.Request) {
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Origin", "https://evil.example")
	})
	if code != http.StatusForbidden {
		t.Fatalf("cross-site POST: %d, want 403", code)
	}
	code, _, _ = do(t, srv, "POST", "/api/v1/namespaces/batch/cronjobs/backup/run", func(r *http.Request) {
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	})
	if code != http.StatusCreated {
		t.Fatalf("same-origin POST: %d, want 201", code)
	}
}

func TestHistoryAndLogs(t *testing.T) {
	srv := newServer(t, false, "")
	code, _, body := do(t, srv, "GET", "/api/v1/namespaces/batch/cronjobs/backup/jobs?limit=5")
	if code != http.StatusOK {
		t.Fatalf("history: %d %s", code, body)
	}
	items := decode[struct{ Items []manager.JobSummary }](t, body).Items
	if len(items) != 1 || items[0].Name != "backup-1" {
		t.Fatalf("history = %+v", items)
	}

	code, _, body = do(t, srv, "GET", "/api/v1/namespaces/batch/cronjobs/backup/jobs/backup-1/logs?tailLines=50")
	if code != http.StatusOK {
		t.Fatalf("logs: %d %s", code, body)
	}
	res := decode[manager.LogResult](t, body)
	if res.Log != "hello from job\n" || res.Pod != "backup-1-abcde" || res.TailLines != 50 {
		t.Fatalf("logs = %+v", res)
	}

	if code, _, _ := do(t, srv, "GET", "/api/v1/namespaces/batch/cronjobs/backup/jobs?limit=abc"); code != http.StatusBadRequest {
		t.Fatalf("bad limit: %d, want 400", code)
	}
	if code, _, _ := do(t, srv, "GET", "/api/v1/namespaces/batch/cronjobs/backup/jobs/backup-1/logs?container=nope"); code != http.StatusBadRequest {
		t.Fatalf("bad container: %d, want 400", code)
	}

	code, _, body = do(t, srv, "GET", "/api/v1/jobs")
	if code != http.StatusOK || len(decode[struct{ Items []manager.JobSummary }](t, body).Items) != 1 {
		t.Fatalf("recent: %d %s", code, body)
	}
}

func TestUnknownAPIPathIsNotTheUI(t *testing.T) {
	srv := newServer(t, false, "")
	// Unknown API paths must 404 instead of falling through to index.html.
	code, _, body := do(t, srv, "GET", "/api/v1/nope")
	if code != http.StatusNotFound || strings.Contains(string(body), "<title>ui</title>") {
		t.Fatalf("unknown api: %d %s", code, body)
	}
}

func TestUIServingWithSPAFallback(t *testing.T) {
	srv := newServer(t, false, testToken)

	code, _, body := do(t, srv, "GET", "/")
	if code != http.StatusOK || !strings.Contains(string(body), "<title>ui</title>") {
		t.Fatalf("root: %d %s", code, body)
	}
	// Client-side routes get index.html; the UI itself is not behind the token.
	code, h, body := do(t, srv, "GET", "/cronjobs/batch/backup")
	if code != http.StatusOK || !strings.Contains(string(body), "<title>ui</title>") || h.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("spa route: %d %s", code, body)
	}
	code, h, _ = do(t, srv, "GET", "/assets/app.js")
	if code != http.StatusOK || !strings.Contains(h.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d cache=%q", code, h.Get("Cache-Control"))
	}
	if code, _, _ := do(t, srv, "GET", "/assets/missing.js"); code != http.StatusNotFound {
		t.Fatalf("missing asset: %d, want 404", code)
	}
}
