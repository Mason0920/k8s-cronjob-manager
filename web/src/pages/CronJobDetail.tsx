import type { ReactNode } from 'react';
import { Link, useParams } from 'react-router-dom';
import { NoticeBox, useCronJobActions } from '../actions';
import { api } from '../api';
import StatusBadge from '../components/StatusBadge';
import { useAppConfig } from '../config';
import { describeCron, formatDateTime, formatDuration } from '../format';
import { useInterval, useLoader } from '../hooks';

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <div className="detail-label">{label}</div>
      <div>{children}</div>
    </div>
  );
}

export default function CronJobDetail() {
  const { namespace = '', name = '' } = useParams();
  const { readOnly } = useAppConfig();
  const detail = useLoader(() => api.getCronJob(namespace, name), [namespace, name]);
  const history = useLoader(() => api.jobs(namespace, name, 30), [namespace, name]);
  const reloadAll = () => {
    detail.reload();
    history.reload();
  };
  const actions = useCronJobActions(reloadAll);

  // Poll while a Job is still running so the status changes without a click.
  const running = (history.data ?? []).some((j) => j.status === 'Running' || j.status === 'Pending');
  useInterval(reloadAll, 5000, running);

  if (detail.loading && !detail.data) return <div className="loading">불러오는 중...</div>;
  if (detail.error || !detail.data) return <div className="error">{detail.error ?? 'CronJob을 찾을 수 없습니다.'}</div>;

  const c = detail.data;
  const jobs = history.data ?? [];

  return (
    <div>
      <div className="page-header">
        <Link to={`/cronjobs?namespace=${encodeURIComponent(c.namespace)}`} className="back-link">
          &larr; {c.namespace} 목록
        </Link>
        <h1 className="page-title">{c.name}</h1>
        <p className="page-subtitle">
          {c.namespace} / 생성 {formatDateTime(c.createdAt)}
        </p>
      </div>

      <NoticeBox notice={actions.notice} />

      <div className="card">
        <div className="card-header">
          <h2 className="card-title">설정</h2>
          <div className="actions">
            {c.suspend ? (
              <span className="status-badge status-warning">일시정지</span>
            ) : (
              <span className="status-badge status-success">활성</span>
            )}
            {!readOnly && (
              <>
                <button className="btn btn-primary" disabled={!!actions.busy} onClick={() => actions.run(c.namespace, c.name)}>
                  지금 실행
                </button>
                {c.suspend ? (
                  <button className="btn btn-success" disabled={!!actions.busy} onClick={() => actions.resume(c.namespace, c.name)}>
                    재개
                  </button>
                ) : (
                  <button className="btn btn-secondary" disabled={!!actions.busy} onClick={() => actions.suspend(c.namespace, c.name)}>
                    일시정지
                  </button>
                )}
              </>
            )}
          </div>
        </div>

        <div className="detail-grid">
          <Field label="스케줄">
            <code className="inline">{c.schedule}</code> <span className="muted">{describeCron(c.schedule)}</span>
          </Field>
          <Field label="타임존">{c.timeZone ?? 'UTC (spec.timeZone 없음)'}</Field>
          <Field label="다음 실행">{c.suspend ? '-' : formatDateTime(c.nextScheduleTime)}</Field>
          <Field label="마지막 예약 실행">{formatDateTime(c.lastScheduleTime)}</Field>
          <Field label="마지막 성공">{formatDateTime(c.lastSuccessfulTime)}</Field>
          <Field label="동시 실행 정책">{c.concurrencyPolicy}</Field>
          <Field label="실행 중 Job">{c.active}</Field>
          <Field label="startingDeadlineSeconds">{c.startingDeadlineSeconds ?? '-'}</Field>
          <Field label="보관 이력 (성공/실패)">
            {c.successfulJobsHistoryLimit ?? 3} / {c.failedJobsHistoryLimit ?? 1}
          </Field>
          <Field label="backoffLimit">{c.backoffLimit ?? 6}</Field>
          <Field label="activeDeadlineSeconds">{c.activeDeadlineSeconds ?? '-'}</Field>
        </div>

        <h3 className="detail-label" style={{ marginTop: '1.5rem' }}>
          컨테이너
        </h3>
        {c.containers.map((ct) => (
          <div key={ct.name}>
            <code className="inline">{ct.name}</code> <span className="muted">{ct.image}</span>
          </div>
        ))}

        {c.labels && Object.keys(c.labels).length > 0 && (
          <>
            <h3 className="detail-label" style={{ marginTop: '1rem' }}>
              라벨
            </h3>
            <div className="actions">
              {Object.entries(c.labels).map(([k, v]) => (
                <code className="inline" key={k}>
                  {k}={v}
                </code>
              ))}
            </div>
          </>
        )}
      </div>

      <div className="card">
        <div className="card-header">
          <h2 className="card-title">실행 이력</h2>
          <button className="btn btn-outline" onClick={reloadAll}>
            새로고침
          </button>
        </div>
        <p className="muted">
          클러스터에 남아 있는 Job만 보입니다. 보관 개수(history limit)나 ttlSecondsAfterFinished로 지워진 Job은 나오지 않습니다.
        </p>

        {history.error && <div className="error">{history.error}</div>}
        {jobs.length === 0 ? (
          <p className="empty">남아 있는 실행 이력이 없습니다.</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Job</th>
                <th>상태</th>
                <th>시작</th>
                <th>소요 시간</th>
                <th>Pod (성공/실패)</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {jobs.map((j) => (
                <tr key={j.name}>
                  <td>
                    <code className="inline">{j.name}</code>
                    {j.manual && <span className="tag">수동</span>}
                  </td>
                  <td>
                    <StatusBadge status={j.status} />
                    {j.message && (
                      <div className="muted" style={{ color: 'var(--error-color)' }}>
                        {j.message}
                      </div>
                    )}
                  </td>
                  <td>{formatDateTime(j.startTime ?? j.createdAt)}</td>
                  <td>{formatDuration(j.startTime, j.completionTime)}</td>
                  <td>
                    {j.succeeded} / {j.failed}
                  </td>
                  <td>
                    <Link className="btn btn-outline btn-sm" to={`/cronjobs/${c.namespace}/${c.name}/jobs/${j.name}/logs`}>
                      로그
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
