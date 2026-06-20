import { describe, expect, it } from 'vitest';
import {
  cronToPreset,
  describeCron,
  presetToCron,
} from '../src/dashboards/widgets/scheduler-widget';

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
});
