export function formatDateTime(iso?: string): string {
  if (!iso) return '-';
  return new Date(iso).toLocaleString('ko-KR', { hour12: false });
}

export function formatDuration(start?: string, end?: string): string {
  if (!start) return '-';
  const endMs = end ? new Date(end).getTime() : Date.now();
  let sec = Math.max(0, Math.round((endMs - new Date(start).getTime()) / 1000));
  const h = Math.floor(sec / 3600);
  sec -= h * 3600;
  const m = Math.floor(sec / 60);
  const s = sec - m * 60;
  const text = h > 0 ? `${h}h ${m}m` : m > 0 ? `${m}m ${s}s` : `${s}s`;
  return end ? text : `${text} (진행 중)`;
}

// describeCron covers the common shapes and falls back to the raw schedule.
export function describeCron(schedule: string): string {
  const presets: Record<string, string> = {
    '@hourly': '매시 정각',
    '@daily': '매일 0시',
    '@midnight': '매일 0시',
    '@weekly': '매주 일요일 0시',
    '@monthly': '매월 1일 0시',
    '@yearly': '매년 1월 1일 0시',
    '@annually': '매년 1월 1일 0시',
  };
  if (presets[schedule]) return presets[schedule];

  const parts = schedule.trim().split(/\s+/);
  if (parts.length !== 5) return schedule;
  const [min, hour, dom, mon, dow] = parts;
  const isNum = (v: string) => /^\d+$/.test(v);
  const hhmm = () => `${hour.padStart(2, '0')}:${min.padStart(2, '0')}`;

  if (/^\*\/\d+$/.test(min) && hour === '*' && dom === '*' && mon === '*' && dow === '*') {
    return `${min.slice(2)}분마다`;
  }
  if (isNum(min) && /^\*\/\d+$/.test(hour) && dom === '*' && mon === '*' && dow === '*') {
    return `${hour.slice(2)}시간마다 (${min}분)`;
  }
  if (isNum(min) && hour === '*' && dom === '*' && mon === '*' && dow === '*') {
    return `매시 ${min}분`;
  }
  if (isNum(min) && isNum(hour) && dom === '*' && mon === '*') {
    if (dow === '*') return `매일 ${hhmm()}`;
    if (dow === '1-5') return `평일 ${hhmm()}`;
    const days = ['일', '월', '화', '수', '목', '금', '토'];
    if (/^[0-6](,[0-6])*$/.test(dow)) {
      return `매주 ${dow.split(',').map((d) => days[Number(d)]).join('/')} ${hhmm()}`;
    }
  }
  if (isNum(min) && isNum(hour) && isNum(dom) && mon === '*' && dow === '*') {
    return `매월 ${dom}일 ${hhmm()}`;
  }
  return schedule;
}
