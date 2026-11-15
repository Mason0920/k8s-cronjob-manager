import { useState } from 'react';
import { api } from './api';

export interface Notice {
  kind: 'ok' | 'error';
  text: string;
}

// useCronJobActions wraps run / suspend / resume with a confirm dialog and a
// result notice, then calls onDone so the page can reload.
export function useCronJobActions(onDone: () => void) {
  const [busy, setBusy] = useState<string>();
  const [notice, setNotice] = useState<Notice>();

  const exec = async (key: string, question: string, fn: () => Promise<string>) => {
    if (!window.confirm(question)) return;
    setBusy(key);
    setNotice(undefined);
    try {
      setNotice({ kind: 'ok', text: await fn() });
      onDone();
    } catch (e) {
      setNotice({ kind: 'error', text: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(undefined);
    }
  };

  return {
    busy,
    notice,
    run: (ns: string, name: string) =>
      exec(`run:${ns}/${name}`, `${ns}/${name} 을(를) 지금 실행할까요? jobTemplate으로 Job 하나를 새로 만듭니다.`, async () => {
        const job = await api.runNow(ns, name);
        return `Job ${job.name} 을(를) 만들었습니다.`;
      }),
    suspend: (ns: string, name: string) =>
      exec(`suspend:${ns}/${name}`, `${ns}/${name} 의 예약 실행을 멈출까요? 이미 실행 중인 Job은 계속 돕니다.`, async () => {
        await api.suspend(ns, name);
        return `${name} 을(를) 일시정지했습니다.`;
      }),
    resume: (ns: string, name: string) =>
      exec(`resume:${ns}/${name}`, `${ns}/${name} 의 예약 실행을 다시 켤까요?`, async () => {
        await api.resume(ns, name);
        return `${name} 을(를) 재개했습니다.`;
      }),
  };
}

export function NoticeBox({ notice }: { notice?: Notice }) {
  if (!notice) return null;
  return <div className={notice.kind === 'ok' ? 'notice' : 'error'}>{notice.text}</div>;
}
