// Mirrors the JSON returned by the Go API (internal/manager/types.go).

export interface AppConfig {
  namespaces: string[];
  readOnly: boolean;
  labelSelector: string;
}

export interface CronJobSummary {
  namespace: string;
  name: string;
  schedule: string;
  timeZone?: string;
  suspend: boolean;
  concurrencyPolicy: string;
  active: number;
  lastScheduleTime?: string;
  lastSuccessfulTime?: string;
  nextScheduleTime?: string;
  createdAt: string;
}

export interface Container {
  name: string;
  image: string;
}

export interface CronJobDetail extends CronJobSummary {
  labels?: Record<string, string>;
  startingDeadlineSeconds?: number;
  successfulJobsHistoryLimit?: number;
  failedJobsHistoryLimit?: number;
  backoffLimit?: number;
  activeDeadlineSeconds?: number;
  containers: Container[];
}

export type JobStatus = 'Running' | 'Pending' | 'Succeeded' | 'Failed' | 'Suspended';

export interface JobSummary {
  namespace: string;
  name: string;
  cronJob: string;
  status: JobStatus;
  manual: boolean;
  createdAt: string;
  startTime?: string;
  completionTime?: string;
  active: number;
  succeeded: number;
  failed: number;
  message?: string;
}

export interface LogResult {
  job: string;
  pod: string;
  container: string;
  pods: string[];
  containers: string[];
  tailLines: number;
  limitBytes: number;
  truncated: boolean;
  message?: string;
  log: string;
}
