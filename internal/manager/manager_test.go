package manager

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

// 2026-11-15 00:00 UTC is 09:00 in Asia/Seoul.
var testNow = time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)

func cronJob(ns, name, uid string, lbls map[string]string, policy batchv1.ConcurrencyPolicy) *batchv1.CronJob {
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         ns,
			Name:              name,
			UID:               types.UID(uid),
			Labels:            lbls,
			CreationTimestamp: metav1.NewTime(testNow.Add(-48 * time.Hour)),
		},
		Spec: batchv1.CronJobSpec{
			Schedule:          "0 3 * * *",
			TimeZone:          ptr.To("Asia/Seoul"),
			ConcurrencyPolicy: policy,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": name},
					Annotations: map[string]string{"owner": "batch-team"},
				},
				Spec: batchv1.JobSpec{
					BackoffLimit: ptr.To[int32](2),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers:    []corev1.Container{{Name: "main", Image: "busybox:1.37"}},
						},
					},
				},
			},
		},
	}
}

func ownedJob(cj *batchv1.CronJob, name, uid string, created time.Time, cond batchv1.JobConditionType, active int32) *batchv1.Job {
	j := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         cj.Namespace,
			Name:              name,
			UID:               types.UID(uid),
			CreationTimestamp: metav1.NewTime(created),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "CronJob", Name: cj.Name, UID: cj.UID, Controller: ptr.To(true),
			}},
		},
		Status: batchv1.JobStatus{Active: active},
	}
	if cond != "" {
		j.Status.Conditions = []batchv1.JobCondition{{Type: cond, Status: corev1.ConditionTrue, Message: "msg-" + string(cond)}}
	}
	return j
}

type fixture struct {
	client *fake.Clientset
	mgr    *Manager
	backup *batchv1.CronJob
}

func newFixture(t *testing.T, readOnly bool, extra ...runtime.Object) fixture {
	t.Helper()
	backup := cronJob("batch", "backup", "uid-backup", map[string]string{"team": "data"}, batchv1.ForbidConcurrent)
	report := cronJob("batch", "report", "uid-report", map[string]string{"team": "data"}, batchv1.AllowConcurrent)
	hidden := cronJob("batch", "hidden", "uid-hidden", map[string]string{"team": "web"}, batchv1.AllowConcurrent)
	elsewhere := cronJob("secret", "backup", "uid-secret", map[string]string{"team": "data"}, batchv1.AllowConcurrent)

	objs := []runtime.Object{backup, report, hidden, elsewhere,
		ownedJob(backup, "backup-old", "uid-j1", testNow.Add(-26*time.Hour), batchv1.JobComplete, 0),
		ownedJob(backup, "backup-new", "uid-j2", testNow.Add(-2*time.Hour), batchv1.JobFailed, 0),
		ownedJob(report, "report-1", "uid-j3", testNow.Add(-1*time.Hour), batchv1.JobComplete, 0),
		ownedJob(hidden, "hidden-1", "uid-j4", testNow.Add(-30*time.Minute), "", 1),
		// A Job with the same name pattern but no owner must never show up.
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Namespace: "batch", Name: "backup-orphan", CreationTimestamp: metav1.NewTime(testNow)}},
	}
	objs = append(objs, extra...)
	client := fake.NewClientset(objs...)
	sel, err := labels.Parse("team=data")
	if err != nil {
		t.Fatal(err)
	}
	mgr := New(client, Options{
		Namespaces: []string{"batch"},
		Selector:   sel,
		ReadOnly:   readOnly,
		Now:        func() time.Time { return testNow },
	})
	return fixture{client: client, mgr: mgr, backup: backup}
}

func TestListCronJobsAppliesAllowlistAndSelector(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()

	got, err := f.mgr.ListCronJobs(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range got {
		names = append(names, c.Namespace+"/"+c.Name)
	}
	if strings.Join(names, ",") != "batch/backup,batch/report" {
		t.Fatalf("names = %v, want batch/backup,batch/report", names)
	}

	// 03:00 Asia/Seoul on 2026-11-16 is 18:00 UTC on 2026-11-15.
	want := time.Date(2026, 11, 15, 18, 0, 0, 0, time.UTC)
	if got[0].NextScheduleTime == nil || !got[0].NextScheduleTime.Equal(want) {
		t.Fatalf("next schedule = %v, want %v", got[0].NextScheduleTime, want)
	}

	if _, err := f.mgr.ListCronJobs(ctx, "secret"); !errors.Is(err, ErrNamespaceNotAllowed) {
		t.Fatalf("err = %v, want ErrNamespaceNotAllowed", err)
	}
}

func TestGetCronJobOutsideSelectorIsNotFound(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()

	if _, err := f.mgr.GetCronJob(ctx, "batch", "hidden"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden: err = %v, want ErrNotFound", err)
	}
	if _, err := f.mgr.GetCronJob(ctx, "batch", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: err = %v, want ErrNotFound", err)
	}
	d, err := f.mgr.GetCronJob(ctx, "batch", "backup")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Containers) != 1 || d.Containers[0].Image != "busybox:1.37" {
		t.Fatalf("containers = %+v", d.Containers)
	}
	if d.ConcurrencyPolicy != "Forbid" || ptr.Deref(d.BackoffLimit, 0) != 2 {
		t.Fatalf("detail = %+v", d)
	}
}

func TestRunNowCreatesJobLikeKubectl(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()

	got, err := f.mgr.RunNow(ctx, "batch", "report")
	if err != nil {
		t.Fatal(err)
	}
	wantName := "report-manual-" + "1794700800"
	if got.Name != wantName || !got.Manual || got.CronJob != "report" {
		t.Fatalf("summary = %+v, want name %s and manual", got, wantName)
	}

	job, err := f.client.BatchV1().Jobs("batch").Get(ctx, wantName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if job.Annotations[ManualAnnotation] != "manual" || job.Annotations["owner"] != "batch-team" {
		t.Fatalf("annotations = %v", job.Annotations)
	}
	if job.Labels["app"] != "report" {
		t.Fatalf("labels = %v", job.Labels)
	}
	ref := metav1.GetControllerOf(job)
	if ref == nil || ref.Kind != "CronJob" || ref.Name != "report" || ref.UID != "uid-report" {
		t.Fatalf("controller ref = %+v", ref)
	}
	if ptr.Deref(job.Spec.BackoffLimit, 0) != 2 || job.Spec.Template.Spec.Containers[0].Image != "busybox:1.37" {
		t.Fatalf("spec not copied from jobTemplate: %+v", job.Spec)
	}

	// The new Job shows up in the CronJob history.
	hist, err := f.mgr.ListJobs(ctx, "batch", "report", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("history len = %d, want 2", len(hist))
	}
}

func TestRunNowRespectsForbidConcurrency(t *testing.T) {
	ctx := context.Background()
	backup := cronJob("batch", "backup", "uid-backup", map[string]string{"team": "data"}, batchv1.ForbidConcurrent)
	running := ownedJob(backup, "backup-running", "uid-run", testNow.Add(-time.Minute), "", 1)
	f := newFixture(t, false, running)

	if _, err := f.mgr.RunNow(ctx, "batch", "backup"); !errors.Is(err, ErrConcurrencyForbidden) {
		t.Fatalf("err = %v, want ErrConcurrencyForbidden", err)
	}

	// Without the running Job the same call succeeds.
	if err := f.client.BatchV1().Jobs("batch").Delete(ctx, "backup-running", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mgr.RunNow(ctx, "batch", "backup"); err != nil {
		t.Fatalf("after delete: %v", err)
	}
}

func TestWritesBlockedInReadOnlyMode(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	f.client.ClearActions()

	if _, err := f.mgr.RunNow(ctx, "batch", "report"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("run: err = %v, want ErrReadOnly", err)
	}
	if _, err := f.mgr.SetSuspend(ctx, "batch", "report", true); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("suspend: err = %v, want ErrReadOnly", err)
	}
	for _, a := range f.client.Actions() {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			t.Fatalf("unexpected write action in read-only mode: %s %s", a.GetVerb(), a.GetResource().Resource)
		}
	}
}

func TestRunNowOutsideAllowlistOrSelector(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()
	if _, err := f.mgr.RunNow(ctx, "secret", "backup"); !errors.Is(err, ErrNamespaceNotAllowed) {
		t.Fatalf("err = %v, want ErrNamespaceNotAllowed", err)
	}
	if _, err := f.mgr.RunNow(ctx, "batch", "hidden"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestManualJobNameFitsInLabelValue(t *testing.T) {
	// CronJob names can be up to 52 characters.
	long := strings.Repeat("a", 44) + "-bcdefgh"
	name := ManualJobName(long, testNow)
	if len(name) > 63 {
		t.Fatalf("len(%q) = %d, want <= 63", name, len(name))
	}
	if !strings.HasSuffix(name, "-manual-1794700800") {
		t.Fatalf("suffix lost: %q", name)
	}
	if strings.Contains(name, "--") {
		t.Fatalf("double dash after truncation: %q", name)
	}
	if got := ManualJobName("short", testNow); got != "short-manual-1794700800" {
		t.Fatalf("short name = %q", got)
	}
}

func TestSetSuspendAndResume(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()

	d, err := f.mgr.SetSuspend(ctx, "batch", "backup", true)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Suspend || d.NextScheduleTime != nil {
		t.Fatalf("after suspend: suspend=%v next=%v", d.Suspend, d.NextScheduleTime)
	}
	cj, err := f.client.BatchV1().CronJobs("batch").Get(ctx, "backup", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !ptr.Deref(cj.Spec.Suspend, false) || cj.Spec.Schedule != "0 3 * * *" {
		t.Fatalf("patch changed more than suspend or did not apply: %+v", cj.Spec)
	}

	d, err = f.mgr.SetSuspend(ctx, "batch", "backup", false)
	if err != nil {
		t.Fatal(err)
	}
	if d.Suspend || d.NextScheduleTime == nil {
		t.Fatalf("after resume: suspend=%v next=%v", d.Suspend, d.NextScheduleTime)
	}

	if _, err := f.mgr.SetSuspend(ctx, "batch", "hidden", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden: err = %v, want ErrNotFound", err)
	}
}

func TestListJobsOnlyOwnedNewestFirst(t *testing.T) {
	f := newFixture(t, false)
	got, err := f.mgr.ListJobs(context.Background(), "batch", "backup", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "backup-new" || got[1].Name != "backup-old" {
		t.Fatalf("history = %+v", got)
	}
	if got[0].Status != JobFailed || got[0].Message != "msg-Failed" || got[1].Status != JobSucceeded {
		t.Fatalf("statuses = %s/%s", got[0].Status, got[1].Status)
	}

	one, err := f.mgr.ListJobs(context.Background(), "batch", "backup", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 {
		t.Fatalf("limit 1 returned %d", len(one))
	}
}

func TestRecentJobsSkipsHiddenAndOrphans(t *testing.T) {
	f := newFixture(t, false)
	got, err := f.mgr.RecentJobs(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, j := range got {
		names = append(names, j.Name)
	}
	if strings.Join(names, ",") != "report-1,backup-new,backup-old" {
		t.Fatalf("recent = %v", names)
	}
}

func TestJobStatus(t *testing.T) {
	cases := []struct {
		name string
		job  batchv1.Job
		want string
	}{
		{"pending", batchv1.Job{}, JobPending},
		{"running", batchv1.Job{Status: batchv1.JobStatus{Active: 1}}, JobRunning},
		{"complete", batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}}, JobSucceeded},
		{"success criteria met", batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue}}}}, JobSucceeded},
		{"failure target", batchv1.Job{Status: batchv1.JobStatus{Active: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue}}}}, JobFailed},
		{"suspended", batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobSuspended, Status: corev1.ConditionTrue}}}}, JobSuspended},
		{"false condition ignored", batchv1.Job{Status: batchv1.JobStatus{Active: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionFalse}}}}, JobRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := jobStatus(&tc.job); got != tc.want {
				t.Fatalf("status = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestNextScheduleHandlesBadInput(t *testing.T) {
	if NextSchedule("not a cron", "", testNow) != nil {
		t.Fatal("want nil for invalid schedule")
	}
	if NextSchedule("0 3 * * *", "Mars/Olympus", testNow) != nil {
		t.Fatal("want nil for unknown time zone")
	}
	got := NextSchedule("*/15 * * * *", "", testNow.Add(time.Minute))
	if got == nil || !got.Equal(testNow.Add(15*time.Minute)) {
		t.Fatalf("next = %v", got)
	}
}

// logPod returns a pod created by job, with two containers and an init
// container, and the default-container annotation pointing at "main".
func logPod(job *batchv1.Job, name string, created time.Time) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         job.Namespace,
			Name:              name,
			CreationTimestamp: metav1.NewTime(created),
			Labels:            map[string]string{"batch.kubernetes.io/job-name": job.Name},
			Annotations:       map[string]string{"kubectl.kubernetes.io/default-container": "main"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: ptr.To(true),
			}},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers:     []corev1.Container{{Name: "sidecar"}, {Name: "main"}},
		},
	}
}

func TestJobLogs(t *testing.T) {
	backup := cronJob("batch", "backup", "uid-backup", map[string]string{"team": "data"}, batchv1.ForbidConcurrent)
	job := ownedJob(backup, "backup-new", "uid-j2", testNow.Add(-2*time.Hour), batchv1.JobFailed, 0)
	older := logPod(job, "backup-new-aaaaa", testNow.Add(-2*time.Hour))
	newer := logPod(job, "backup-new-bbbbb", testNow.Add(-90*time.Minute))
	// A pod with the right label but another owner must be ignored.
	stray := logPod(job, "backup-new-stray", testNow)
	stray.OwnerReferences[0].UID = "someone-else"

	f := newFixture(t, false, older, newer, stray)
	var seen *corev1.PodLogOptions
	f.client.PrependReactor("get", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "log" {
			return false, nil, nil
		}
		ga := a.(k8stesting.GenericAction)
		seen = ga.GetValue().(*corev1.PodLogOptions)
		return true, &runtime.Unknown{Raw: []byte("line 1\nline 2\n")}, nil
	})
	ctx := context.Background()

	res, err := f.mgr.JobLogs(ctx, "batch", "backup", "backup-new", LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pod != "backup-new-bbbbb" || res.Container != "main" {
		t.Fatalf("picked %s/%s, want newest pod and default container", res.Pod, res.Container)
	}
	if strings.Join(res.Pods, ",") != "backup-new-bbbbb,backup-new-aaaaa" {
		t.Fatalf("pods = %v", res.Pods)
	}
	if strings.Join(res.Containers, ",") != "sidecar,main,init" {
		t.Fatalf("containers = %v", res.Containers)
	}
	if res.Log != "line 1\nline 2\n" || res.Truncated {
		t.Fatalf("log = %q truncated=%v", res.Log, res.Truncated)
	}
	if seen == nil || seen.Container != "main" || ptr.Deref(seen.TailLines, 0) != 500 || ptr.Deref(seen.LimitBytes, 0) != 1<<20 {
		t.Fatalf("log options = %+v", seen)
	}

	res, err = f.mgr.JobLogs(ctx, "batch", "backup", "backup-new", LogOptions{Pod: "backup-new-aaaaa", Container: "init", TailLines: 999999})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pod != "backup-new-aaaaa" || seen.Container != "init" || ptr.Deref(seen.TailLines, 0) != MaxTailLines {
		t.Fatalf("explicit selection: pod=%s opts=%+v", res.Pod, seen)
	}

	if _, err := f.mgr.JobLogs(ctx, "batch", "backup", "backup-new", LogOptions{Pod: "backup-new-stray"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("stray pod: err = %v, want ErrInvalidArgument", err)
	}
	if _, err := f.mgr.JobLogs(ctx, "batch", "backup", "backup-new", LogOptions{Container: "nope"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("bad container: err = %v, want ErrInvalidArgument", err)
	}
	// report-1 exists but belongs to another CronJob.
	if _, err := f.mgr.JobLogs(ctx, "batch", "backup", "report-1", LogOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign job: err = %v, want ErrNotFound", err)
	}
	if _, err := f.mgr.JobLogs(ctx, "batch", "backup", "backup-new", LogOptions{TailLines: -1}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("negative tail: err = %v, want ErrInvalidArgument", err)
	}

	// A job without pods returns an empty result with a message.
	res, err = f.mgr.JobLogs(ctx, "batch", "backup", "backup-old", LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pods) != 0 || res.Message == "" {
		t.Fatalf("no pods: %+v", res)
	}
}

func TestJobLogsTruncatesAtLimit(t *testing.T) {
	backup := cronJob("batch", "backup", "uid-backup", map[string]string{"team": "data"}, batchv1.ForbidConcurrent)
	job := ownedJob(backup, "backup-new", "uid-j2", testNow.Add(-2*time.Hour), batchv1.JobFailed, 0)
	client := fake.NewClientset(backup, job, logPod(job, "backup-new-aaaaa", testNow))
	client.PrependReactor("get", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "log" {
			return false, nil, nil
		}
		return true, &runtime.Unknown{Raw: []byte(strings.Repeat("x", 100))}, nil
	})
	mgr := New(client, Options{Namespaces: []string{"batch"}, LogLimitBytes: 10, Now: func() time.Time { return testNow }})

	res, err := mgr.JobLogs(context.Background(), "batch", "backup", "backup-new", LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Log) != 10 || !res.Truncated {
		t.Fatalf("len=%d truncated=%v, want 10 and true", len(res.Log), res.Truncated)
	}
}
