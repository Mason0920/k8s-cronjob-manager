import type { JobStatus } from '../types';

const classes: Record<JobStatus, string> = {
  Succeeded: 'status-success',
  Failed: 'status-error',
  Running: 'status-info',
  Pending: 'status-warning',
  Suspended: 'status-warning',
};

const labels: Record<JobStatus, string> = {
  Succeeded: '성공',
  Failed: '실패',
  Running: '실행 중',
  Pending: '대기',
  Suspended: '일시정지',
};

export default function StatusBadge({ status }: { status: JobStatus }) {
  return <span className={`status-badge ${classes[status] ?? 'status-warning'}`}>{labels[status] ?? status}</span>;
}
