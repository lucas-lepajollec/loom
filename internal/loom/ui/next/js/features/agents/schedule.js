// Convert the part of five-field cron that Codeman can store without
// changing its meaning: a fixed wall-clock time on any or selected weekdays.
const WEEKDAYS = ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT'];
const weekday = value => /^\d$/.test(value) ? Number(value) : WEEKDAYS.indexOf(value);

export function parseCron(expression) {
  const fields = String(expression || '').trim().toUpperCase().split(/\s+/);
  if (fields.length !== 5) return null;
  const [minute, hour, day, month, weekdays] = fields;
  if (day !== '*' || month !== '*' || !/^\d{1,2}$/.test(minute) || !/^\d{1,2}$/.test(hour) ||
      Number(minute) > 59 || Number(hour) > 23) return null;
  const time = hour.padStart(2, '0') + ':' + minute.padStart(2, '0');
  if (weekdays === '*') return { scheduleType: 'daily', dailyTime: time };

  const days = new Set();
  for (const range of weekdays.split(',')) {
    const match = /^([0-7]|[A-Z]{3})(?:-([0-7]|[A-Z]{3}))?$/.exec(range);
    if (!match) return null;
    const start = weekday(match[1]);
    const end = match[2] ? weekday(match[2]) : start;
    if (start < 0 || end < start) return null;
    for (let d = start; d <= end; d++) days.add(d % 7);
  }
  if (days.size === 7) return { scheduleType: 'daily', dailyTime: time };
  return { scheduleType: 'weekly', weeklyDays: [...days].sort((a, b) => a - b), weeklyTime: time };
}
