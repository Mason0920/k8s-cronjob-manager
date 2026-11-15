package manager

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// JobLogs returns the log of one container in one pod of a Job. The Job must
// be owned by the named CronJob, so the endpoint cannot be used to read logs
// of arbitrary pods in an allowed namespace.
func (m *Manager) JobLogs(ctx context.Context, namespace, cronJob, jobName string, opts LogOptions) (LogResult, error) {
	tail := opts.TailLines
	switch {
	case tail < 0:
		return LogResult{}, fmt.Errorf("%w: tailLines must be positive", ErrInvalidArgument)
	case tail == 0:
		tail = m.opts.LogTailLines
	case tail > MaxTailLines:
		tail = MaxTailLines
	}
	limit := m.opts.LogLimitBytes

	cj, err := m.getCronJob(ctx, namespace, cronJob)
	if err != nil {
		return LogResult{}, err
	}
	job, err := m.client.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return LogResult{}, ErrNotFound
		}
		return LogResult{}, fmt.Errorf("get job: %w", err)
	}
	if !ownedBy(job, cj) {
		return LogResult{}, ErrNotFound
	}

	res := LogResult{Job: job.Name, TailLines: tail, LimitBytes: limit, Pods: []string{}, Containers: []string{}}

	pods, err := m.jobPods(ctx, job)
	if err != nil {
		return LogResult{}, err
	}
	if len(pods) == 0 {
		res.Message = "no pods found for this job (not started yet, or already removed by ttlSecondsAfterFinished)"
		return res, nil
	}
	for i := range pods {
		res.Pods = append(res.Pods, pods[i].Name)
	}

	pod := &pods[0] // newest
	if opts.Pod != "" {
		idx := slices.IndexFunc(pods, func(p corev1.Pod) bool { return p.Name == opts.Pod })
		if idx < 0 {
			return LogResult{}, fmt.Errorf("%w: pod %q does not belong to job %q", ErrInvalidArgument, opts.Pod, job.Name)
		}
		pod = &pods[idx]
	}
	res.Pod = pod.Name
	res.Containers = containerNames(pod)

	container := opts.Container
	if container != "" {
		if !slices.Contains(res.Containers, container) {
			return LogResult{}, fmt.Errorf("%w: container %q not in pod %q", ErrInvalidArgument, container, pod.Name)
		}
	} else {
		container = defaultContainer(pod)
	}
	res.Container = container

	req := m.client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container:  container,
		TailLines:  &tail,
		LimitBytes: &limit,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		// A pending pod or a container that has not started answers 400.
		// That is a normal state for a fresh Job, so report it instead of failing.
		if apierrors.IsBadRequest(err) {
			res.Message = err.Error()
			return res, nil
		}
		return LogResult{}, fmt.Errorf("stream logs: %w", err)
	}
	defer stream.Close()

	// LimitBytes is enforced by the API server; read one byte more as a
	// second guard and to detect truncation.
	data, err := io.ReadAll(io.LimitReader(stream, limit+1))
	if err != nil {
		return LogResult{}, fmt.Errorf("read logs: %w", err)
	}
	if int64(len(data)) >= limit {
		res.Truncated = true
		data = data[:min(int64(len(data)), limit)]
	}
	res.Log = string(data)
	return res, nil
}

// jobPods lists the pods created by the Job, newest first.
func (m *Manager) jobPods(ctx context.Context, job *batchv1.Job) ([]corev1.Pod, error) {
	var pods []corev1.Pod
	for _, key := range []string{jobNameLabel, legacyJobNameLabel} {
		list, err := m.client.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: key + "=" + job.Name,
		})
		if err != nil {
			return nil, fmt.Errorf("list pods: %w", err)
		}
		if len(list.Items) > 0 {
			pods = list.Items
			break
		}
	}
	out := pods[:0]
	for _, p := range pods {
		ref := metav1.GetControllerOf(&p)
		if ref != nil && ref.Kind == "Job" && ref.UID == job.UID {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b corev1.Pod) int {
		if c := b.CreationTimestamp.Compare(a.CreationTimestamp.Time); c != 0 {
			return c
		}
		return strings.Compare(b.Name, a.Name)
	})
	return out, nil
}

// containerNames lists regular containers first, then init containers.
func containerNames(pod *corev1.Pod) []string {
	names := make([]string, 0, len(pod.Spec.Containers)+len(pod.Spec.InitContainers))
	for _, c := range pod.Spec.Containers {
		names = append(names, c.Name)
	}
	for _, c := range pod.Spec.InitContainers {
		names = append(names, c.Name)
	}
	return names
}

func defaultContainer(pod *corev1.Pod) string {
	if name := pod.Annotations[defaultContainerAnnotation]; name != "" {
		for _, c := range pod.Spec.Containers {
			if c.Name == name {
				return name
			}
		}
	}
	if len(pod.Spec.Containers) > 0 {
		return pod.Spec.Containers[0].Name
	}
	return ""
}
