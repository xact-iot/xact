import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const apiMock = vi.hoisted(() => ({
  listScheduledTasks: vi.fn(),
  getScheduleRunLog: vi.fn(),
  listPDFTemplates: vi.fn(),
  listNotificationProfiles: vi.fn(),
}));

vi.mock('../src/api', () => ({
  ...apiMock,
  createScheduledTask: vi.fn(),
  updateScheduledTask: vi.fn(),
  deleteScheduledTask: vi.fn(),
  runScheduledTaskNow: vi.fn(),
}));

vi.mock('../src/permissions/permissions', () => ({
  can: vi.fn(async () => true),
}));

vi.mock('../src/store/ui-store', () => ({
  getUiStore: () => ({ get: () => 'America/Dominica' }),
}));

import {
  cronToPreset,
  describeCron,
  presetToCron,
  serverTimezoneLabel,
} from '../src/dashboards/widgets/scheduler-widget';

const scheduledTask = (status: string) => ({
  id: 'backup-1',
  orgName: 'default',
  name: 'Daily backup',
  description: '',
  taskType: 'backup',
  taskConfig: { outputDir: 'backups', keepCount: 3 },
  schedule: '00 10 * * *',
  enabled: true,
  lastRunStatus: status,
  lastRunMessage: status === 'running' ? 'Backup exporting table 1/3: users' : '',
  createdAt: '2026-07-27T00:00:00Z',
  updatedAt: '2026-07-27T00:00:00Z',
});

async function flushPromises(): Promise<void> {
  for (let i = 0; i < 6; i++) await Promise.resolve();
}

beforeEach(() => {
  vi.useFakeTimers();
  apiMock.listScheduledTasks.mockReset().mockResolvedValue([scheduledTask('ok')]);
  apiMock.getScheduleRunLog.mockReset().mockResolvedValue([]);
  apiMock.listPDFTemplates.mockReset().mockResolvedValue([]);
  apiMock.listNotificationProfiles.mockReset().mockResolvedValue([]);
});

afterEach(() => {
  document.body.innerHTML = '';
  vi.useRealTimers();
});

describe('scheduler-widget cron helpers', () => {
  it('keeps Sunday as day zero when parsing weekly cron schedules', () => {
    const preset = cronToPreset('00 08 * * 0');

    expect(preset).toMatchObject({
      frequency: 'weekly',
      hour: 8,
      minute: 0,
      weekday: 0,
    });
    expect(describeCron('00 08 * * 0')).toBe('Weekly on Sunday at 08:00');
    expect(presetToCron(preset)).toBe('00 08 * * 0');
  });

  it('labels the timezone as the server timezone', () => {
    expect(serverTimezoneLabel('America/Dominica')).toBe('Server timezone: America/Dominica');
    expect(serverTimezoneLabel('')).toBe('Server timezone unavailable');
  });

  it('live-refreshes automatic task status and expanded history', async () => {
    const widget = document.createElement('scheduler-widget');
    document.body.appendChild(widget);
    await flushPromises();

    expect(widget.textContent).toContain('Daily backup');
    expect(widget.textContent).toContain('Server timezone: America/Dominica');

    (widget.querySelector('.sw-expand') as HTMLButtonElement).click();
    await flushPromises();

    apiMock.listScheduledTasks.mockResolvedValue([scheduledTask('running')]);
    apiMock.getScheduleRunLog.mockResolvedValue([{
      id: 1,
      scheduleId: 'backup-1',
      orgName: 'default',
      firedAt: '2026-07-27T10:00:00Z',
      status: 'running',
      message: '',
      outputPath: '',
    }]);
    await vi.advanceTimersByTimeAsync(2000);

    expect(widget.textContent).toContain('Backup exporting table 1/3: users');
    expect(widget.querySelector('.sw-history-entry .sw-dot-running')).not.toBeNull();

    apiMock.listScheduledTasks.mockResolvedValue([scheduledTask('ok')]);
    apiMock.getScheduleRunLog.mockResolvedValue([{
      id: 1,
      scheduleId: 'backup-1',
      orgName: 'default',
      firedAt: '2026-07-27T10:00:00Z',
      completedAt: '2026-07-27T10:00:05Z',
      status: 'ok',
      message: '',
      outputPath: '/backups/backup-20260727-100000.tar.gz',
    }]);
    await vi.advanceTimersByTimeAsync(2000);

    expect(widget.textContent).toContain('/backups/backup-20260727-100000.tar.gz');
    expect(widget.querySelector('.sw-history-entry .sw-dot-ok')).not.toBeNull();
  });
});
