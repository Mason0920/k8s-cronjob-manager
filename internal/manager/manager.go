// Package manager holds the Kubernetes side of the tool: listing CronJobs,
// running them by hand, suspending them, and reading job history and logs.
// Every call checks the namespace allowlist and the label selector first.
package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

const (
	// ManualAnnotation is the annotation kubectl puts on a Job created with
	// `kubectl create job --from=cronjob/<name>`.
	ManualAnnotation = "cronjob.kubernetes.io/instantiate"

	// MaxTailLines caps the tailLines query parameter.
	MaxTailLines = 5000
	// MaxHistory caps the number of jobs returned by one history call.
	MaxHistory = 100

	// Job names end up in the pod label batch.kubernetes.io/job-name, and
	// label values are limited to 63 characters.
	maxJobNameLength = 63
	listPageSize     = 500

	jobNameLabel               = "batch.kubernetes.io/job-name"
	legacyJobNameLabel         = "job-name"
	defaultContainerAnnotation = "kubectl.kubernetes.io/default-container"
)

// Options configures a Manager.
type Options struct {
	// Namespaces is the allowlist. Requests for any other namespace fail.
	Namespaces []string
	// Selector limits which CronJobs are visible. nil means all.
	Selector labels.Selector
	// ReadOnly disables run, suspend and resume.
	ReadOnly bool
	// LogTailLines is the default number of log lines.
	LogTailLines int64
	// LogLimitBytes caps the size of one log response.
	LogLimitBytes int64
	// Now is used for job names and next schedule times. Defaults to time.Now.
	Now func() time.Time
}

// Manager talks to the Kubernetes API.
type Manager struct {
	client  kubernetes.Interface
	opts    Options
	allowed map[string]bool
}

// New returns a Manager. The client can be a real or a fake clientset.
func New(client kubernetes.Interface, opts Options) *Manager {
	if opts.Selector == nil {
		opts.Selector = labels.Everything()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.LogTailLines <= 0 {
		opts.LogTailLines = 500
	}
	if opts.LogLimitBytes <= 0 {
		opts.LogLimitBytes = 1 << 20
	}
	allowed := make(map[string]bool, len(opts.Namespaces))
	for _, ns := range opts.Namespaces {
		allowed[ns] = true
	}
	return &Manager{client: client, opts: opts, allowed: allowed}
}

// Namespaces returns a copy of the allowlist.
func (m *Manager) Namespaces() []string { return slices.Clone(m.opts.Namespaces) }

// ReadOnly reports whether write operations are disabled.
func (m *Manager) ReadOnly() bool { return m.opts.ReadOnly }

// LabelSelector returns the selector as a string ("" means all).
func (m *Manager) LabelSelector() string { return m.opts.Selector.String() }

func (m *Manager) checkNamespace(ns string) error {
	if !m.allowed[ns] {
		return fmt.Errorf("%w: %q", ErrNamespaceNotAllowed, ns)
	}
	return nil
}

// ListCronJobs returns the CronJobs in one namespace, or in every allowed
// namespace when namespace is empty.
func (m *Manager) ListCronJobs(ctx context.Context, namespace string) ([]CronJobSummary, error) {
	namespaces := m.opts.Namespaces
	if namespace != "" {
		if err := m.checkNamespace(namespace); err != nil {
			return nil, err
		}
		namespaces = []string{namespace}
	}
	now := m.opts.Now()
	out := []CronJobSummary{}
	for _, ns := range namespaces {
		items, err := m.listCronJobs(ctx, ns)
		if err != nil {
			return nil, err
		}
		for i := range items {
			out = append(out, toSummary(&items[i], now))
		}
	}
	slices.SortFunc(out, func(a, b CronJobSummary) int {
		if c := strings.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

// GetCronJob returns one CronJob with its template details.
func (m *Manager) GetCronJob(ctx context.Context, namespace, name string) (CronJobDetail, error) {
	cj, err := m.getCronJob(ctx, namespace, name)
	if err != nil {
		return CronJobDetail{}, err
	}
	return toDetail(cj, m.opts.Now()), nil
}

// RunNow creates a Job from the CronJob's jobTemplate, like
// `kubectl create job --from=cronjob/<name>`.
func (m *Manager) RunNow(ctx context.Context, namespace, name string) (JobSummary, error) {
	if m.opts.ReadOnly {
		return JobSummary{}, ErrReadOnly
	}
	cj, err := m.getCronJob(ctx, namespace, name)
	if err != nil {
		return JobSummary{}, err
	}
	if cj.Spec.ConcurrencyPolicy == batchv1.ForbidConcurrent {
		jobs, err := m.ownedJobs(ctx, cj)
		if err != nil {
			return JobSummary{}, err
		}
		for i := range jobs {
			if !isFinished(&jobs[i]) {
				return JobSummary{}, fmt.Errorf("%w: %s", ErrConcurrencyForbidden, jobs[i].Name)
			}
		}
	}
	job := JobFromCronJob(cj, m.opts.Now())
	created, err := m.client.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return JobSummary{}, ErrAlreadyExists
		}
		return JobSummary{}, fmt.Errorf("create job: %w", err)
	}
	return toJobSummary(created, cj.Name), nil
}

// SetSuspend sets spec.suspend on the CronJob with a merge patch.
func (m *Manager) SetSuspend(ctx context.Context, namespace, name string, suspend bool) (CronJobDetail, error) {
	if m.opts.ReadOnly {
		return CronJobDetail{}, ErrReadOnly
	}
	// Get first so the selector check applies before any write.
	if _, err := m.getCronJob(ctx, namespace, name); err != nil {
		return CronJobDetail{}, err
	}
	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"suspend": suspend}})
	if err != nil {
		return CronJobDetail{}, err
	}
	cj, err := m.client.BatchV1().CronJobs(namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return CronJobDetail{}, ErrNotFound
		}
		return CronJobDetail{}, fmt.Errorf("patch cronjob: %w", err)
	}
	return toDetail(cj, m.opts.Now()), nil
}

// ListJobs returns the Jobs owned by the CronJob, newest first.
func (m *Manager) ListJobs(ctx context.Context, namespace, name string, limit int) ([]JobSummary, error) {
	cj, err := m.getCronJob(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	jobs, err := m.ownedJobs(ctx, cj)
	if err != nil {
		return nil, err
	}
	out := make([]JobSummary, 0, len(jobs))
	for i := range jobs {
		out = append(out, toJobSummary(&jobs[i], cj.Name))
	}
	return newestFirst(out, limit), nil
}

// RecentJobs returns the newest Jobs owned by any visible CronJob in any
// allowed namespace.
func (m *Manager) RecentJobs(ctx context.Context, limit int) ([]JobSummary, error) {
	out := []JobSummary{}
	for _, ns := range m.opts.Namespaces {
		cronJobs, err := m.listCronJobs(ctx, ns)
		if err != nil {
			return nil, err
		}
		if len(cronJobs) == 0 {
			continue
		}
		owners := make(map[types.UID]string, len(cronJobs))
		for i := range cronJobs {
			owners[cronJobs[i].UID] = cronJobs[i].Name
		}
		jobs, err := m.listJobs(ctx, ns)
		if err != nil {
			return nil, err
		}
		for i := range jobs {
			ref := metav1.GetControllerOf(&jobs[i])
			if ref == nil || ref.Kind != "CronJob" {
				continue
			}
			if cjName, ok := owners[ref.UID]; ok {
				out = append(out, toJobSummary(&jobs[i], cjName))
			}
		}
	}
	return newestFirst(out, limit), nil
}

// JobFromCronJob builds the Job that `kubectl create job --from=cronjob/x`
// would build: the jobTemplate spec, its labels and annotations, the
// instantiate=manual annotation and a controller ownerReference to the
// CronJob. kubectl lets a template annotation override instantiate=manual;
// here the manual marker always wins so manual runs stay identifiable.
func JobFromCronJob(cj *batchv1.CronJob, now time.Time) *batchv1.Job {
	annotations := make(map[string]string, len(cj.Spec.JobTemplate.Annotations)+1)
	maps.Copy(annotations, cj.Spec.JobTemplate.Annotations)
	annotations[ManualAnnotation] = "manual"

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        ManualJobName(cj.Name, now),
			Namespace:   cj.Namespace,
			Annotations: annotations,
			Labels:      maps.Clone(cj.Spec.JobTemplate.Labels),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: batchv1.SchemeGroupVersion.String(),
				Kind:       "CronJob",
				Name:       cj.Name,
				UID:        cj.UID,
				Controller: ptr.To(true),
			}},
		},
		Spec: *cj.Spec.JobTemplate.Spec.DeepCopy(),
	}
}

// ManualJobName returns <cronjob>-manual-<unix seconds>, cutting the CronJob
// part so the whole name fits in 63 characters.
func ManualJobName(cronJobName string, now time.Time) string {
	suffix := "-manual-" + strconv.FormatInt(now.Unix(), 10)
	prefix := cronJobName
	if limit := maxJobNameLength - len(suffix); len(prefix) > limit {
		prefix = strings.TrimRight(prefix[:limit], "-.")
	}
	return prefix + suffix
}

func (m *Manager) getCronJob(ctx context.Context, namespace, name string) (*batchv1.CronJob, error) {
	if err := m.checkNamespace(namespace); err != nil {
		return nil, err
	}
	cj, err := m.client.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get cronjob: %w", err)
	}
	// A CronJob outside the selector is reported as not found so the API
	// does not reveal that it exists.
	if !m.opts.Selector.Matches(labels.Set(cj.Labels)) {
		return nil, ErrNotFound
	}
	return cj, nil
}

func (m *Manager) listCronJobs(ctx context.Context, namespace string) ([]batchv1.CronJob, error) {
	var out []batchv1.CronJob
	opts := metav1.ListOptions{LabelSelector: m.opts.Selector.String(), Limit: listPageSize}
	for {
		list, err := m.client.BatchV1().CronJobs(namespace).List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("list cronjobs in %s: %w", namespace, err)
		}
		out = append(out, list.Items...)
		if list.Continue == "" {
			return out, nil
		}
		opts.Continue = list.Continue
	}
}

func (m *Manager) listJobs(ctx context.Context, namespace string) ([]batchv1.Job, error) {
	var out []batchv1.Job
	opts := metav1.ListOptions{Limit: listPageSize}
	for {
		list, err := m.client.BatchV1().Jobs(namespace).List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("list jobs in %s: %w", namespace, err)
		}
		out = append(out, list.Items...)
		if list.Continue == "" {
			return out, nil
		}
		opts.Continue = list.Continue
	}
}

// ownedJobs returns the Jobs whose controller is the given CronJob.
func (m *Manager) ownedJobs(ctx context.Context, cj *batchv1.CronJob) ([]batchv1.Job, error) {
	jobs, err := m.listJobs(ctx, cj.Namespace)
	if err != nil {
		return nil, err
	}
	out := jobs[:0]
	for i := range jobs {
		if ownedBy(&jobs[i], cj) {
			out = append(out, jobs[i])
		}
	}
	return out, nil
}

func ownedBy(job *batchv1.Job, cj *batchv1.CronJob) bool {
	ref := metav1.GetControllerOf(job)
	return ref != nil && ref.Kind == "CronJob" && ref.UID == cj.UID && ref.Name == cj.Name
}

func toSummary(cj *batchv1.CronJob, now time.Time) CronJobSummary {
	s := CronJobSummary{
		Namespace:          cj.Namespace,
		Name:               cj.Name,
		Schedule:           cj.Spec.Schedule,
		TimeZone:           ptr.Deref(cj.Spec.TimeZone, ""),
		Suspend:            ptr.Deref(cj.Spec.Suspend, false),
		ConcurrencyPolicy:  string(cj.Spec.ConcurrencyPolicy),
		Active:             len(cj.Status.Active),
		LastScheduleTime:   timePtr(cj.Status.LastScheduleTime),
		LastSuccessfulTime: timePtr(cj.Status.LastSuccessfulTime),
		CreatedAt:          cj.CreationTimestamp.UTC(),
	}
	if s.ConcurrencyPolicy == "" {
		s.ConcurrencyPolicy = string(batchv1.AllowConcurrent)
	}
	if !s.Suspend {
		s.NextScheduleTime = NextSchedule(cj.Spec.Schedule, s.TimeZone, now)
	}
	return s
}

func toDetail(cj *batchv1.CronJob, now time.Time) CronJobDetail {
	tmpl := cj.Spec.JobTemplate.Spec
	d := CronJobDetail{
		CronJobSummary:             toSummary(cj, now),
		Labels:                     cj.Labels,
		StartingDeadlineSeconds:    cj.Spec.StartingDeadlineSeconds,
		SuccessfulJobsHistoryLimit: cj.Spec.SuccessfulJobsHistoryLimit,
		FailedJobsHistoryLimit:     cj.Spec.FailedJobsHistoryLimit,
		BackoffLimit:               tmpl.BackoffLimit,
		ActiveDeadlineSeconds:      tmpl.ActiveDeadlineSeconds,
		Containers:                 []Container{},
	}
	for _, c := range tmpl.Template.Spec.Containers {
		d.Containers = append(d.Containers, Container{Name: c.Name, Image: c.Image})
	}
	return d
}

// NextSchedule returns the next run time in UTC, or nil when the schedule
// cannot be parsed. Without a timeZone the CronJob controller uses the time
// zone of kube-controller-manager, which is UTC on most clusters; this
// function assumes UTC in that case.
func NextSchedule(schedule, timeZone string, now time.Time) *time.Time {
	loc := time.UTC
	if timeZone != "" {
		l, err := time.LoadLocation(timeZone)
		if err != nil {
			return nil
		}
		loc = l
	}
	sched, err := cron.ParseStandard(schedule)
	if err != nil {
		return nil
	}
	next := sched.Next(now.In(loc))
	if next.IsZero() {
		return nil
	}
	next = next.UTC()
	return &next
}

func toJobSummary(job *batchv1.Job, cronJob string) JobSummary {
	status, msg := jobStatus(job)
	return JobSummary{
		Namespace:      job.Namespace,
		Name:           job.Name,
		CronJob:        cronJob,
		Status:         status,
		Manual:         job.Annotations[ManualAnnotation] == "manual",
		CreatedAt:      job.CreationTimestamp.UTC(),
		StartTime:      timePtr(job.Status.StartTime),
		CompletionTime: timePtr(job.Status.CompletionTime),
		Active:         job.Status.Active,
		Succeeded:      job.Status.Succeeded,
		Failed:         job.Status.Failed,
		Message:        msg,
	}
}

func jobStatus(job *batchv1.Job) (string, string) {
	suspended := false
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete, batchv1.JobSuccessCriteriaMet:
			return JobSucceeded, ""
		case batchv1.JobFailed, batchv1.JobFailureTarget:
			msg := c.Message
			if msg == "" {
				msg = c.Reason
			}
			return JobFailed, msg
		case batchv1.JobSuspended:
			suspended = true
		}
	}
	switch {
	case suspended:
		return JobSuspended, ""
	case job.Status.Active > 0:
		return JobRunning, ""
	default:
		return JobPending, ""
	}
}

func isFinished(job *batchv1.Job) bool {
	s, _ := jobStatus(job)
	return s == JobSucceeded || s == JobFailed
}

func newestFirst(jobs []JobSummary, limit int) []JobSummary {
	slices.SortFunc(jobs, func(a, b JobSummary) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.Name, a.Name)
	})
	if limit <= 0 || limit > MaxHistory {
		limit = MaxHistory
	}
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs
}

func timePtr(t *metav1.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}
