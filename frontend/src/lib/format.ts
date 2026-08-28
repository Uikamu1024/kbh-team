export function formatSecondsAsClock(totalSeconds: number): string {
  const safeSeconds = Math.max(0, Math.floor(totalSeconds));
  const minutes = Math.floor(safeSeconds / 60);
  const seconds = safeSeconds % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

export function formatMinutesLabel(totalSeconds: number): string {
  return `約${Math.max(1, Math.round(totalSeconds / 60))}分`;
}

export function formatDateLabel(isoDate: string): string {
  const date = new Date(isoDate);
  const weekday = ["日", "月", "火", "水", "木", "金", "土"][date.getDay()];
  return `${date.getMonth() + 1}月${date.getDate()}日(${weekday})`;
}
