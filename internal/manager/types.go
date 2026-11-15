package manager

import (
	"errors"
	"time"
)

// Errors returned by Manager. The HTTP layer maps them to status codes.
var (
	// ErrNamespaceNotAllowed means the namespace is not in the allowlist.
	ErrNamespaceNotAllowed = errors.New("namespace is not in the allowlist")
	// ErrNotFound means the object does not exist or is outside the label selector.
	ErrNotFound = errors.New("not found")
	// ErrReadOnly means a write was attempted while read-only mode is on.
	ErrReadOnly = errors.New("read-only mode: write operations are disabled")
	// ErrConcurrencyForbidden means the CronJob has concurrencyPolicy Forbid
	// and a Job owned by it is still running.
	ErrConcurrencyForbidden = errors.New("concurrencyPolicy is Forbid and a job is still running")
	// ErrAlreadyExists means a Job with the generated name already exists.
	ErrAlreadyExists = errors.New("a job with the same name already exists, retry in a second")
	// ErrInvalidArgument means a query parameter or path value is not valid.
	ErrInvalidArgument = errors.New("invalid argument")
)

// Job status values derived from Job conditions.
const (
	JobRunning   = "Running"
	JobPending   = "Pending"
	JobSucceeded = "Succeeded"
	JobFailed    = "Failed"
	JobSuspended = "Suspended"
)

// Container is a container name and image from the CronJob job template.
type Container struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// CronJobSummary is the list view of a CronJob.
type CronJobSummary struct {
	Namespace          string     `json:"namespace"`
	Name               string     `json:"name"`
	Schedule           string     `json:"schedule"`
	TimeZone           string     `json:"timeZone,omitempty"`
	Suspend            bool       `json:"suspend"`
	ConcurrencyPolicy  string     `json:"concurrencyPolicy"`
	Active             int        `json:"active"`
	LastScheduleTime   *time.Time `json:"lastScheduleTime,omitempty"`
	LastSuccessfulTime *time.Time `json:"lastSuccessfulTime,omitempty"`
	NextScheduleTime   *time.Time `json:"nextScheduleTime,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
}

// CronJobDetail adds template and policy fields to the summary.
type CronJobDetail struct {
	CronJobSummary
	Labels                     map[string]string `json:"labels,omitempty"`
	StartingDeadlineSeconds    *int64            `json:"startingDeadlineSeconds,omitempty"`
	SuccessfulJobsHistoryLimit *int32            `json:"successfulJobsHistoryLimit,omitempty"`
	FailedJobsHistoryLimit     *int32            `json:"failedJobsHistoryLimit,omitempty"`
	BackoffLimit               *int32            `json:"backoffLimit,omitempty"`
	ActiveDeadlineSeconds      *int64            `json:"activeDeadlineSeconds,omitempty"`
	Containers                 []Container       `json:"containers"`
}

// JobSummary is one execution (Job) owned by a CronJob.
type JobSummary struct {
	Namespace      string     `json:"namespace"`
	Name           string     `json:"name"`
	CronJob        string     `json:"cronJob"`
	Status         string     `json:"status"`
	Manual         bool       `json:"manual"`
	CreatedAt      time.Time  `json:"createdAt"`
	StartTime      *time.Time `json:"startTime,omitempty"`
	CompletionTime *time.Time `json:"completionTime,omitempty"`
	Active         int32      `json:"active"`
	Succeeded      int32      `json:"succeeded"`
	Failed         int32      `json:"failed"`
	Message        string     `json:"message,omitempty"`
}

// LogOptions selects which pod and container to read and how much.
type LogOptions struct {
	Pod       string
	Container string
	TailLines int64
}

// LogResult is the log text plus what was selected, so a UI can offer the
// other pods and containers of the same Job.
type LogResult struct {
	Job        string   `json:"job"`
	Pod        string   `json:"pod"`
	Container  string   `json:"container"`
	Pods       []string `json:"pods"`
	Containers []string `json:"containers"`
	TailLines  int64    `json:"tailLines"`
	LimitBytes int64    `json:"limitBytes"`
	Truncated  bool     `json:"truncated"`
	Message    string   `json:"message,omitempty"`
	Log        string   `json:"log"`
}
