import { Link, useSearchParams } from 'react-router-dom';
import { NoticeBox, useCronJobActions } from '../actions';
import { api } from '../api';
import { useAppConfig } from '../config';
import { describeCron, formatDateTime } from '../format';
import { useLoader } from '../hooks';

export default function CronJobList() {
  const { readOnly } = useAppConfig();
  const [params] = useSearchParams();
  const namespace = params.get('namespace') ?? '';
  const list = useLoader(() => api.listCronJobs(namespace || undefined), [namespace]);
  const actions = useCronJobActions(list.reload);

  const items = list.data ?? [];
  const title = namespace ? `CronJob (${namespace})` : '전체 CronJob';

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">{title}</h1>
        <p className="page-subtitle">
          CronJob을 만들거나 고치는 일은 Git과 배포 파이프라인에서 하고, 여기서는 실행과 확인만 합니다.
        </p>
      </div>

      <NoticeBox notice={actions.notice} />
      {list.error && <div className="error">{list.error}</div>}

      <div className="card">
        <div className="card-header">
          <h2 className="card-title">{items.length}개</h2>
          <button className="btn btn-outline" onClick={list.reload} disabled={list.loading}>
            새로고침
          </button>
        </div>

        {list.loading && !list.data ? (
          <div className="loading">불러오는 중...</div>
        ) : items.length === 0 ? (
          <p className="empty">표시할 CronJob이 없습니다. 네임스페이스 허용 목록과 라벨 셀렉터를 확인하세요.</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>이름</th>
                <th>스케줄</th>
                <th>다음 실행</th>
                <th>마지막 성공</th>
                <th>상태</th>
                {!readOnly && <th>작업</th>}
              </tr>
            </thead>
            <tbody>
              {items.map((c) => {
                const key = `${c.namespace}/${c.name}`;
                return (
                  <tr key={key}>
                    <td>
                      <Link className="name-link" to={`/cronjobs/${c.namespace}/${c.name}`}>
                        {c.name}
                      </Link>
                      <div className="muted">{c.namespace}</div>
                    </td>
                    <td>
                      <code className="inline">{c.schedule}</code>
                      <div className="muted">
                        {describeCron(c.schedule)}
                        {c.timeZone ? ` (${c.timeZone})` : ' (UTC)'}
                      </div>
                    </td>
                    <td>{c.suspend ? '-' : formatDateTime(c.nextScheduleTime)}</td>
                    <td>{formatDateTime(c.lastSuccessfulTime)}</td>
                    <td>
                      {c.suspend ? (
                        <span className="status-badge status-warning">일시정지</span>
                      ) : c.active > 0 ? (
                        <span className="status-badge status-info">실행 중 {c.active}</span>
                      ) : (
                        <span className="status-badge status-success">활성</span>
                      )}
                    </td>
                    {!readOnly && (
                      <td>
                        <div className="actions">
                          <button
                            className="btn btn-outline btn-sm"
                            disabled={!!actions.busy}
                            onClick={() => actions.run(c.namespace, c.name)}
                          >
                            지금 실행
                          </button>
                          {c.suspend ? (
                            <button
                              className="btn btn-success btn-sm"
                              disabled={!!actions.busy}
                              onClick={() => actions.resume(c.namespace, c.name)}
                            >
                              재개
                            </button>
                          ) : (
                            <button
                              className="btn btn-secondary btn-sm"
                              disabled={!!actions.busy}
                              onClick={() => actions.suspend(c.namespace, c.name)}
                            >
                              일시정지
                            </button>
                          )}
                        </div>
                      </td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
