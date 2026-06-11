/**
 * scheduler-widget - Manages recurring scheduled tasks.
 *
 * Displays a table of all scheduled tasks for the current organisation with
 * their schedule, last-run status, and action buttons (run now, edit, delete,
 * expand history). An overlay form handles create/edit with a friendly
 * preset+time schedule picker that stores a 5-field cron expression.
 */

import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { registerPermissions } from '../../permissions/registry';
import { can } from '../../permissions/permissions';
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import {
  listScheduledTasks, createScheduledTask, updateScheduledTask,
  deleteScheduledTask, runScheduledTaskNow, getScheduleRunLog,
  listPDFTemplates, listNotificationProfiles,
  type ScheduledTask, type ScheduleRunLog, type PDFTemplate, type NotificationProfile,
} from '../../api';
import { showConfirm } from '../../components/app-dialog';

registerPermissions('scheduler', 'Scheduler', [
  { name: 'view', description: 'View scheduled tasks and run history' },
  { name: 'manage', description: 'Create, edit, and run scheduled tasks' },
], 'Controls access to the Scheduler widget - roles with view can inspect recurring jobs; roles with manage can create, edit, and run them.');

// ── Cron helpers ─────────────────────────────────────────────────────────────

type Frequency = 'hourly' | 'daily' | 'weekly' | 'monthly';

interface SchedulePreset {
  frequency: Frequency;
  hour: number;
  minute: number;
  weekday: number;  // 0=Sun … 6=Sat
  monthDay: number; // 1–28
}

function presetToCron(p: SchedulePreset): string {
  const h = String(p.hour).padStart(2, '0');
  const m = String(p.minute).padStart(2, '0');
  switch (p.frequency) {
    case 'hourly':  return `${m} * * * *`;
    case 'daily':   return `${m} ${h} * * *`;
    case 'weekly':  return `${m} ${h} * * ${p.weekday}`;
    case 'monthly': return `${m} ${h} ${p.monthDay} * *`;
  }
}

function cronToPreset(expr: string): SchedulePreset {
  const parts = expr.trim().split(/\s+/);
  const def: SchedulePreset = { frequency: 'daily', hour: 8, minute: 0, weekday: 1, monthDay: 1 };
  if (parts.length !== 5) return def;
  const [min, hour, dom, , dow] = parts;
  const m = parseInt(min) || 0;
  const h = parseInt(hour) || 0;
  if (hour === '*') return { ...def, frequency: 'hourly', minute: m };
  if (dom !== '*') return { ...def, frequency: 'monthly', hour: h, minute: m, monthDay: parseInt(dom) || 1 };
  if (dow !== '*') return { ...def, frequency: 'weekly', hour: h, minute: m, weekday: parseInt(dow) || 1 };
  return { ...def, frequency: 'daily', hour: h, minute: m };
}

function describeCron(expr: string): string {
  const p = cronToPreset(expr);
  const pad = (n: number) => String(n).padStart(2, '0');
  const time = `${pad(p.hour)}:${pad(p.minute)}`;
  const days = ['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
  switch (p.frequency) {
    case 'hourly':  return `Every hour at :${pad(p.minute)}`;
    case 'daily':   return `Daily at ${time}`;
    case 'weekly':  return `Weekly on ${days[p.weekday]} at ${time}`;
    case 'monthly': return `Monthly on day ${p.monthDay} at ${time}`;
  }
}

const TASK_TYPE_LABELS: Record<string, string> = {
  report: '📄 Report',
  backup: '💾 Backup',
  shell:  '⌨ Shell',
  yaegi:  '⚙ Script',
  command: '▣ Command',
};

// ── Widget ───────────────────────────────────────────────────────────────────

interface State {
  tasks: ScheduledTask[];
  loading: boolean;
  error: string;
  expandedId: string | null;
  history: Record<string, ScheduleRunLog[]>;
  runningId: string | null;
  overlay: OverlayState | null;
  templates: PDFTemplate[];
  profiles: NotificationProfile[];
}

interface OverlayState {
  mode: 'create' | 'edit';
  task: Partial<ScheduledTask>;
  preset: SchedulePreset;
  saving: boolean;
  error: string;
}

export class SchedulerWidget extends BaseComponent {
  private state: State = {
    tasks: [], loading: true, error: '', expandedId: null,
    history: {}, runningId: null, overlay: null, templates: [], profiles: [],
  };
  private _handlers: Array<[EventTarget, string, EventListener]> = [];
  private permitted = false;
  private canManage = false;

  protected render(): void {
    this.innerHTML = `
      <style>
        .sw-root { height: 100%; display: flex; flex-direction: column; overflow: hidden; font-size: 0.75rem; color: var(--content-text); }
        .sw-toolbar { display: flex; align-items: center; justify-content: space-between; padding: 6px 12px; border-bottom: 1px solid var(--widget-border); background: var(--widget-header-bg); flex-shrink: 0; }
        .sw-toolbar-title { font-weight: 600; font-size: 0.8rem; color: var(--widget-header-text); }
        .sw-btn { padding: 4px 10px; border-radius: 4px; border: 1px solid var(--accent-color); background: transparent; color: var(--accent-color); cursor: pointer; font-size: 0.7rem; transition: background 0.12s; }
        .sw-btn:hover { background: color-mix(in srgb, var(--accent-color) 15%, transparent); }
        .sw-btn-danger { border-color: var(--danger-color); color: var(--danger-color); }
        .sw-btn-danger:hover { background: color-mix(in srgb, var(--danger-color) 15%, transparent); }
        .sw-btn-icon { padding: 3px 7px; font-size: 0.75rem; }
        .sw-table-wrap { flex: 1; overflow-y: auto; }
        table.sw-table { width: 100%; border-collapse: collapse; }
        .sw-table th { text-align: left; padding: 6px 10px; font-size: 0.65rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: color-mix(in srgb, var(--content-text) 55%, transparent); border-bottom: 1px solid var(--widget-border); background: var(--widget-header-bg); position: sticky; top: 0; }
        .sw-table td { padding: 6px 10px; border-bottom: 1px solid color-mix(in srgb, var(--widget-border) 50%, transparent); vertical-align: middle; }
        .sw-table tr:hover td { background: color-mix(in srgb, var(--accent-color) 4%, transparent); }
        .sw-status { display: inline-flex; align-items: center; gap: 4px; }
        .sw-status-cell { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
        .sw-status-message { max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.62rem; color: color-mix(in srgb, var(--content-text) 58%, transparent); }
        .sw-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
        .sw-dot-ok { background: var(--status-good-color); }
        .sw-dot-error { background: var(--status-bad-color); }
        .sw-dot-running { background: var(--status-warn-color); animation: sw-pulse 1s ease-in-out infinite; }
        .sw-dot-none { background: var(--inactive-dot, #444); }
        @keyframes sw-pulse { 0%,100% { opacity:1; } 50% { opacity:0.4; } }
        .sw-actions { display: flex; gap: 4px; align-items: center; }
        .sw-history-row td { padding: 0 !important; }
        .sw-history { background: var(--widget-header-bg); padding: 8px 12px 10px 28px; }
        .sw-history-entry { display: grid; grid-template-columns: 164px 50px 1fr; gap: 6px; padding: 3px 0; border-bottom: 1px solid color-mix(in srgb, var(--widget-border) 40%, transparent); font-size: 0.65rem; }
        .sw-history-entry:last-child { border: none; }
        .sw-history-label { font-weight: 600; font-size: 0.65rem; color: color-mix(in srgb, var(--content-text) 55%, transparent); margin-bottom: 4px; }
        .sw-empty { padding: 24px; text-align: center; color: color-mix(in srgb, var(--content-text) 40%, transparent); font-style: italic; }
        .sw-error { padding: 12px; color: var(--error-color); background: var(--error-bg); border: 1px solid var(--error-border); border-radius: 6px; margin: 8px; }
        /* ── Overlay ── */
        .sw-overlay { position: fixed; inset: 0; z-index: 19000; display: flex; align-items: center; justify-content: center; background: rgba(0,0,0,0.55); }
        .sw-modal { background: var(--modal-bg); color: var(--modal-text); border: 1px solid var(--border-color); border-radius: 10px; width: min(560px, 92vw); max-height: 90vh; display: flex; flex-direction: column; box-shadow: 0 8px 40px rgba(0,0,0,0.4); }
        .sw-modal-header { display: flex; align-items: center; justify-content: space-between; padding: 14px 18px; border-bottom: 1px solid var(--border-color); }
        .sw-modal-title { font-size: 0.9rem; font-weight: 600; }
        .sw-modal-close { background: none; border: none; cursor: pointer; color: inherit; opacity: 0.55; font-size: 1rem; padding: 2px 6px; }
        .sw-modal-close:hover { opacity: 1; }
        .sw-modal-body { overflow-y: auto; padding: 16px 18px; flex: 1; }
        .sw-modal-footer { padding: 12px 18px; border-top: 1px solid var(--border-color); display: flex; justify-content: flex-end; gap: 8px; }
        .sw-field { margin-bottom: 14px; }
        .sw-field label { display: block; font-size: 0.68rem; font-weight: 600; margin-bottom: 4px; color: color-mix(in srgb, var(--modal-text) 70%, transparent); }
        .sw-input, .sw-select, .sw-textarea { width: 100%; padding: 7px 10px; font-size: 0.75rem; background: var(--input-bg, rgba(255,255,255,0.05)); color: var(--modal-text); border: 1px solid var(--border-color); border-radius: 5px; box-sizing: border-box; outline: none; font-family: inherit; }
        .sw-input:focus, .sw-select:focus, .sw-textarea:focus { border-color: var(--accent-color); }
        .sw-select option { background: var(--modal-bg, #1a1a2e); color: var(--modal-text, #e0e0e0); }
        .sw-textarea { resize: vertical; min-height: 80px; font-family: 'SF Mono','Cascadia Code',Consolas,monospace; font-size: 0.7rem; }
        .sw-segments { display: flex; gap: 0; border: 1px solid var(--border-color); border-radius: 5px; overflow: hidden; }
        .sw-seg { flex: 1; padding: 6px; text-align: center; font-size: 0.7rem; cursor: pointer; border: none; background: transparent; color: var(--modal-text); transition: background 0.1s; }
        .sw-seg.active { background: var(--accent-color); color: var(--accent-text, #000); }
        .sw-seg:not(.active):hover { background: color-mix(in srgb, var(--accent-color) 12%, transparent); }
        .sw-row { display: flex; gap: 10px; }
        .sw-row .sw-field { flex: 1; }
        .sw-cron-preview { margin-top: 6px; font-size: 0.65rem; color: color-mix(in srgb, var(--modal-text) 55%, transparent); }
        .sw-cron-preview code { font-family: monospace; color: var(--accent-color); }
        .sw-toggle-row { display: flex; align-items: center; gap: 10px; }
        .sw-toggle { position: relative; width: 36px; height: 20px; flex-shrink: 0; }
        .sw-toggle input { opacity: 0; width: 0; height: 0; position: absolute; }
        .sw-toggle-slider { position: absolute; inset: 0; background: var(--border-color); border-radius: 20px; cursor: pointer; transition: background 0.15s; }
        .sw-toggle input:checked + .sw-toggle-slider { background: var(--accent-color); }
        .sw-toggle-slider::before { content: ''; position: absolute; width: 14px; height: 14px; background: #fff; border-radius: 50%; top: 3px; left: 3px; transition: transform 0.15s; }
        .sw-toggle input:checked + .sw-toggle-slider::before { transform: translateX(16px); }
        .sw-modal-error { color: var(--error-color); font-size: 0.7rem; margin-bottom: 10px; }
        .sw-section-label { font-size: 0.65rem; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase; color: var(--accent-color); margin: 16px 0 10px; }
      </style>
      <div class="sw-root" id="sw-root">
        ${this.renderBody()}
      </div>
      ${this.state.overlay ? this.renderOverlay() : ''}
    `;
  }

  private renderBody(): string {
    if (this.state.loading) return '<div class="sw-empty">Loading…</div>';
    if (!this.permitted) return '<div class="sw-empty">No permission to view the scheduler.</div>';
    if (this.state.error) return `<div class="sw-error">${this.state.error}</div>`;
    return `
      <div class="sw-toolbar">
        <span class="sw-toolbar-title">Scheduled Tasks</span>
        ${this.canManage ? `<button class="sw-btn" id="sw-add">+ Add Task</button>` : `<span class="sw-empty" style="padding:0;font-style:normal">Read only</span>`}
      </div>
      <div class="sw-table-wrap">
        ${this.state.tasks.length === 0 ? '<div class="sw-empty">No scheduled tasks - click Add Task to create one.</div>' : `
        <table class="sw-table">
          <thead><tr>
            <th>Name</th><th>Type</th><th>Schedule</th><th>Last Run</th><th>Actions</th>
          </tr></thead>
          <tbody>${this.state.tasks.map(t => this.renderTaskRow(t)).join('')}</tbody>
        </table>`}
      </div>
    `;
  }

  private renderTaskRow(t: ScheduledTask): string {
    const isExpanded = this.state.expandedId === t.id;
    const history = this.state.history[t.id] || [];
    const statusDot = this.statusDot(t.lastRunStatus);
    const isRunning = this.state.runningId === t.id;
    const disabledLabel = !t.enabled ? ' <span style="opacity:0.4;font-size:0.6rem">(disabled)</span>' : '';

    return `
      <tr data-id="${t.id}">
        <td><strong>${_esc(t.name)}</strong>${disabledLabel}</td>
        <td>${TASK_TYPE_LABELS[t.taskType] ?? t.taskType}</td>
        <td>${describeCron(t.schedule)}</td>
        <td>
          <span class="sw-status-cell">
            <span class="sw-status">
              <span class="sw-dot ${statusDot}"></span>
              ${t.lastRunStatus || '-'}
            </span>
            ${t.lastRunMessage ? `<span class="sw-status-message" title="${_esc(t.lastRunMessage)}">${_esc(t.lastRunMessage)}</span>` : ''}
          </span>
        </td>
        <td>
          <div class="sw-actions">
            ${this.canManage ? `<button class="sw-btn sw-btn-icon sw-run" data-id="${t.id}" title="Run now" ${isRunning ? 'disabled' : ''}>▶</button>` : ''}
            <button class="sw-btn sw-btn-icon sw-edit" data-id="${t.id}" title="${this.canManage ? 'Edit' : 'View'}">✎</button>
            ${this.canManage ? `<button class="sw-btn sw-btn-icon sw-btn-danger sw-delete" data-id="${t.id}" title="Delete">🗑</button>` : ''}
            <button class="sw-btn sw-btn-icon sw-expand" data-id="${t.id}" title="${isExpanded ? 'Collapse' : 'History'}">${isExpanded ? '▲' : '⌄'}</button>
          </div>
        </td>
      </tr>
      ${isExpanded ? `<tr class="sw-history-row"><td colspan="5"><div class="sw-history">
        <div class="sw-history-label">Run History</div>
        ${history.length === 0
          ? '<div style="opacity:0.5;font-size:0.65rem">No runs recorded.</div>'
          : history.map(e => `
            <div class="sw-history-entry">
              <span>${new Date(e.firedAt).toLocaleString()}</span>
              <span class="sw-status"><span class="sw-dot ${e.status === 'ok' ? 'sw-dot-ok' : 'sw-dot-error'}"></span>${e.status}</span>
              <span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-family:${e.outputPath ? 'monospace' : 'inherit'}" title="${_esc(e.message || e.outputPath)}">${_esc(e.message || e.outputPath || '-')}</span>
            </div>`).join('')}
      </div></td></tr>` : ''}
    `;
  }

  private statusDot(status: string): string {
    if (status === 'ok') return 'sw-dot-ok';
    if (status === 'error') return 'sw-dot-error';
    if (status === 'running') return 'sw-dot-running';
    return 'sw-dot-none';
  }

  private renderOverlay(): string {
    const ov = this.state.overlay!;
    const t = ov.task;
    const p = ov.preset;
    const cronExpr = presetToCron(p);
    const disabled = this.canManage ? '' : 'disabled';

    const freqOpts: Frequency[] = ['hourly', 'daily', 'weekly', 'monthly'];
    const freqSeg = freqOpts.map(f => `
      <button class="sw-seg ${p.frequency === f ? 'active' : ''}" data-freq="${f}" ${disabled}>${f.charAt(0).toUpperCase() + f.slice(1)}</button>
    `).join('');

    const hours = Array.from({length:24}, (_,i) => `<option value="${i}" ${p.hour===i?'selected':''}>${String(i).padStart(2,'0')}</option>`).join('');
    const mins  = Array.from({length:60}, (_,i) => `<option value="${i}" ${p.minute===i?'selected':''}>${String(i).padStart(2,'0')}</option>`).join('');
    const wdays = ['Sun','Mon','Tue','Wed','Thu','Fri','Sat'].map((d,i) => `<option value="${i}" ${p.weekday===i?'selected':''}>${d}</option>`).join('');
    const mdays = Array.from({length:28}, (_,i) => `<option value="${i+1}" ${p.monthDay===i+1?'selected':''}>${i+1}</option>`).join('');

    const templates = this.state.templates;
    const profiles  = this.state.profiles;
    const cfg = (t.taskConfig || {}) as Record<string, any>;

    const typeConfig = (): string => {
      switch (t.taskType) {
        case 'report': return `
          <div class="sw-field">
            <label>Report Template</label>
            <select class="sw-select" id="sw-tmpl" ${disabled}>
              <option value="">- select -</option>
              ${templates.map(tm => `<option value="${tm.id}" ${cfg.templateId===tm.id?'selected':''}>${_esc(tm.name)}</option>`).join('')}
            </select>
          </div>
          <div class="sw-field">
            <label>Output Directory</label>
            <input class="sw-input" id="sw-outdir" placeholder="/var/xact/reports" value="${_esc(cfg.outputDir||'')}" ${disabled}>
          </div>`;
        case 'backup': return `
          <div class="sw-field">
            <label>Output Directory</label>
            <input class="sw-input" id="sw-outdir" placeholder="backups" value="${_esc(cfg.outputDir||'backups')}" ${disabled}>
          </div>
          <div class="sw-field">
            <label>Number of backups to keep (0 = unlimited)</label>
            <input class="sw-input" id="sw-keepcount" type="number" min="0" value="${cfg.keepCount||0}" ${disabled}>
          </div>`;
        case 'shell': return `
          <div class="sw-field">
            <label>Shell Command</label>
            <textarea class="sw-textarea" id="sw-cmd" placeholder="e.g. /usr/local/bin/my-script.sh" ${disabled}>${_esc(cfg.command||'')}</textarea>
          </div>
          <div class="sw-field">
            <label>Timeout (seconds, 0 = 300)</label>
            <input class="sw-input" id="sw-timeout" type="number" min="0" value="${cfg.timeout||0}" ${disabled}>
          </div>`;
        case 'yaegi': return `
          <div class="sw-field">
            <label>Go Script (package script; func Run() error)</label>
            <textarea class="sw-textarea" id="sw-script" style="min-height:120px" placeholder='package script\n\nfunc Run() error {\n\treturn nil\n}' ${disabled}>${_esc(cfg.script||'')}</textarea>
          </div>
          <div class="sw-field">
            <label>Timeout (seconds, 0 = 60)</label>
            <input class="sw-input" id="sw-timeout" type="number" min="0" value="${cfg.timeout||0}" ${disabled}>
          </div>`;
        case 'command': return `
            <div class="sw-field">
              <label>Device path</label>
              <div style="display:flex;gap:6px;">
                <input class="sw-input" id="sw-device" placeholder="e.g. PS_01" value="${_esc(cfg.deviceName||'')}" ${disabled}>
                <button class="sw-btn sw-btn-icon" id="sw-device-browse" type="button" title="Browse devices" ${disabled}>…</button>
              </div>
            </div>
          <div class="sw-field">
            <label>Tag Path</label>
            <div style="display:flex;gap:6px;">
              <input class="sw-input" id="sw-tagpath" placeholder="e.g. pumps.1.status" value="${_esc(cfg.tagPath||'')}" ${disabled}>
              <button class="sw-btn sw-btn-icon" id="sw-tagpath-browse" type="button" title="Browse tags" ${disabled}>…</button>
            </div>
          </div>
            <div class="sw-field">
              <label>Timeout (seconds, 0 = 10)</label>
              <input class="sw-input" id="sw-timeout" type="number" min="0" value="${cfg.timeout||0}" ${disabled}>
          </div>
          <div class="sw-field">
            <label>Value</label>
            <input class="sw-input" id="sw-value" value="${_esc(formatCommandValue(cfg.value))}" ${disabled}>
          </div>`;
        default: return '';
      }
    };

    return `
      <div class="sw-overlay" id="sw-overlay">
        <div class="sw-modal">
          <div class="sw-modal-header">
            <span class="sw-modal-title">${ov.mode === 'create' ? 'New Scheduled Task' : (this.canManage ? 'Edit Task' : 'View Task')}</span>
            <button class="sw-modal-close" id="sw-overlay-close">✕</button>
          </div>
          <div class="sw-modal-body">
            ${ov.error ? `<div class="sw-modal-error">${_esc(ov.error)}</div>` : ''}

            <div class="sw-section-label">General</div>
            <div class="sw-row">
              <div class="sw-field">
                <label>Name</label>
                <input class="sw-input" id="sw-name" value="${_esc(t.name||'')}" placeholder="My Task" ${disabled}>
              </div>
              <div class="sw-field" style="flex:0 0 auto;width:auto;display:flex;align-items:flex-end;padding-bottom:1px">
                <div class="sw-toggle-row">
                  <label class="sw-toggle">
                    <input type="checkbox" id="sw-enabled" ${t.enabled!==false?'checked':''} ${disabled}>
                    <span class="sw-toggle-slider"></span>
                  </label>
                  <span style="font-size:0.7rem">Enabled</span>
                </div>
              </div>
            </div>
            <div class="sw-field">
              <label>Description</label>
              <input class="sw-input" id="sw-desc" value="${_esc(t.description||'')}" placeholder="Optional description" ${disabled}>
            </div>

            <div class="sw-section-label">Task Type</div>
            <div class="sw-segments" id="sw-type-segs">
              ${['report','backup','shell','yaegi','command'].map(ty =>
                `<button class="sw-seg ${t.taskType===ty?'active':''}" data-type="${ty}" ${disabled}>${TASK_TYPE_LABELS[ty]}</button>`
              ).join('')}
            </div>

            <div class="sw-section-label">Task Config</div>
            <div id="sw-type-config">${typeConfig()}</div>

            <div class="sw-section-label">Schedule</div>
            <div class="sw-field">
              <label>Frequency</label>
              <div class="sw-segments" id="sw-freq-segs">${freqSeg}</div>
            </div>
            ${p.frequency !== 'hourly' ? `
            <div class="sw-row">
              <div class="sw-field">
                <label>Hour</label>
                <select class="sw-select" id="sw-hour" ${disabled}>${hours}</select>
              </div>
              <div class="sw-field">
                <label>Minute</label>
                <select class="sw-select" id="sw-minute" ${disabled}>${mins}</select>
              </div>
              ${p.frequency === 'weekly' ? `
              <div class="sw-field">
                <label>On</label>
                <select class="sw-select" id="sw-weekday" ${disabled}>${wdays}</select>
              </div>` : ''}
              ${p.frequency === 'monthly' ? `
              <div class="sw-field">
                <label>Day of month</label>
                <select class="sw-select" id="sw-monthday" ${disabled}>${mdays}</select>
              </div>` : ''}
            </div>` : `
            <div class="sw-row">
              <div class="sw-field">
                <label>Minute</label>
                <select class="sw-select" id="sw-minute" ${disabled}>${mins}</select>
              </div>
            </div>`}
            <div class="sw-cron-preview">Cron: <code>${_esc(cronExpr)}</code> - ${describeCron(cronExpr)}</div>

            <div class="sw-section-label">Output</div>
            <div class="sw-field">
              <label>Email Profile (optional)</label>
              <select class="sw-select" id="sw-email-profile" ${disabled}>
                <option value="">- none -</option>
                ${profiles.map(pr => `<option value="${pr.id}" ${cfg.emailProfileId===pr.id?'selected':''}>${_esc(pr.name)}</option>`).join('')}
              </select>
            </div>
          </div>
          <div class="sw-modal-footer">
            <button class="sw-btn" id="sw-overlay-cancel">${this.canManage ? 'Cancel' : 'Close'}</button>
            ${this.canManage ? `<button class="sw-btn" id="sw-overlay-save" ${ov.saving?'disabled':''}>
              ${ov.saving ? 'Saving…' : 'Save'}
            </button>` : ''}
          </div>
        </div>
      </div>
    `;
  }

  // ── Lifecycle ──────────────────────────────────────────────────────────────

  protected detachEventListeners(): void {
    for (const [el, ev, fn] of this._handlers) el.removeEventListener(ev, fn);
    this._handlers = [];
  }

  private _on(el: EventTarget, ev: string, fn: EventListener): void {
    el.addEventListener(ev, fn);
    this._handlers.push([el, ev, fn]);
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  // ── Init ───────────────────────────────────────────────────────────────────

  connectedCallback(): void {
    this.render();
    this.attachEventListeners();
    this.init();
  }

  disconnectedCallback(): void {
    this.detachEventListeners();
  }

  private async init(): Promise<void> {
    const [canView, canManage] = await Promise.all([can('scheduler.view'), can('scheduler.manage')]);
    this.canManage = canManage;
    this.permitted = canView || canManage;
    if (!this.permitted) {
      this.state.loading = false;
      this.rerender();
      return;
    }
    await this.load();
  }

  private async load(): Promise<void> {
    this.state.loading = true;
    this.state.error = '';
    try {
      const [tasks, templates, profiles] = await Promise.all([
        listScheduledTasks(),
        this.canManage ? listPDFTemplates() : Promise.resolve([]),
        this.canManage ? listNotificationProfiles() : Promise.resolve([]),
      ]);
      this.state.tasks = tasks;
      this.state.templates = templates;
      this.state.profiles = profiles;
    } catch (e: any) {
      this.state.error = e?.message ?? 'Failed to load';
    }
    this.state.loading = false;
    this.rerender();
  }

  // ── Event handling ─────────────────────────────────────────────────────────

  private handleClick = (e: MouseEvent): void => {
    const target = e.target as HTMLElement;

    // Toolbar
    if (target.id === 'sw-add' && this.canManage) { this.openOverlay('create'); return; }

    // Table row actions
    const runBtn    = target.closest('.sw-run') as HTMLElement | null;
    const editBtn   = target.closest('.sw-edit') as HTMLElement | null;
    const deleteBtn = target.closest('.sw-delete') as HTMLElement | null;
    const expandBtn = target.closest('.sw-expand') as HTMLElement | null;

    if (runBtn && this.canManage)    { this.handleRunNow(runBtn.dataset.id!); return; }
    if (editBtn)   { this.handleEdit(editBtn.dataset.id!); return; }
    if (deleteBtn && this.canManage) { this.handleDelete(deleteBtn.dataset.id!); return; }
    if (expandBtn) { this.handleExpand(expandBtn.dataset.id!); return; }

    // Overlay controls
    if (target.id === 'sw-overlay-close' || target.id === 'sw-overlay-cancel') {
      this.state.overlay = null; this.rerender(); return;
    }
    if (target.id === 'sw-overlay-save' && this.canManage) { this.handleSave(); return; }
    if (target.id === 'sw-device-browse' && this.canManage) { this.openCommandDevicePicker(); return; }
    if (target.id === 'sw-tagpath-browse' && this.canManage) { this.openCommandTagPicker(); return; }

    // Overlay: type segment
    const typeSeg = target.closest('[data-type]') as HTMLElement | null;
    if (typeSeg && this.state.overlay && this.canManage) {
      this.state.overlay.task.taskType = typeSeg.dataset.type as any;
      this.state.overlay.task.taskConfig = {};
      this.rerender(); return;
    }

    // Overlay: frequency segment
    const freqSeg = target.closest('[data-freq]') as HTMLElement | null;
    if (freqSeg && this.state.overlay && this.canManage) {
      this.syncOverlayTaskFromForm();
      this.state.overlay.preset.frequency = freqSeg.dataset.freq as Frequency;
      this.rerender(); return;
    }

    // Overlay: time pickers (change events - not clicks, handled separately)
  };

  connectedCallback2 = (): void => {}; // unused, connectedCallback is defined above

  protected attachOverlayChangeListeners(): void {
    const onChange = (id: string, fn: (val: string) => void) => {
      const el = this.querySelector(`#${id}`);
      if (!el) return;
      const handler = (e: Event) => fn((e.target as HTMLInputElement).value);
      el.addEventListener('change', handler);
      this._handlers.push([el, 'change', handler as EventListener]);
    };

    onChange('sw-hour',     v => { if (this.state.overlay) { this.state.overlay.preset.hour = parseInt(v); this.updateCronPreview(); } });
    onChange('sw-minute',   v => { if (this.state.overlay) { this.state.overlay.preset.minute = parseInt(v); this.updateCronPreview(); } });
    onChange('sw-weekday',  v => { if (this.state.overlay) { this.state.overlay.preset.weekday = parseInt(v); this.updateCronPreview(); } });
    onChange('sw-monthday', v => { if (this.state.overlay) { this.state.overlay.preset.monthDay = parseInt(v); this.updateCronPreview(); } });
  }

  protected attachEventListeners(): void {
    this._on(this, 'click', this.handleClick as EventListener);
    if (this.state.overlay) {
      this.attachOverlayChangeListeners();
    }
  }

  private updateCronPreview(): void {
    const p = this.state.overlay?.preset;
    if (!p) return;
    const expr = presetToCron(p);
    const preview = this.querySelector('.sw-cron-preview');
    if (preview) preview.innerHTML = `Cron: <code>${_esc(expr)}</code> - ${describeCron(expr)}`;
  }

  private openCommandDevicePicker(): void {
    const input = this.querySelector<HTMLInputElement>('#sw-device');
    const selectedPath = input?.value.trim() ?? '';
    getTreeBrowserDialog().open('', 'Select Command Device', (path) => {
      if (input) input.value = path;
    }, false, selectedPath, selectedPath);
  }

  private openCommandTagPicker(): void {
    const input = this.querySelector<HTMLInputElement>('#sw-tagpath');
    const selectedPath = input?.value.trim() ?? '';
    getTreeBrowserDialog().open('', 'Select Command Tag', (path) => {
      if (input) input.value = path;
    }, true, selectedPath, selectedPath);
  }

  // ── Actions ────────────────────────────────────────────────────────────────

  private syncOverlayTaskFromForm(): void {
    if (!this.state.overlay) return;
    const values = this.collectOverlayValues();
    this.state.overlay.task = {
      ...this.state.overlay.task,
      ...values,
    };
  }

  private openOverlay(mode: 'create' | 'edit', task: Partial<ScheduledTask> = {}): void {
    if (mode === 'create' && !this.canManage) return;
    const preset = task.schedule ? cronToPreset(task.schedule) : { frequency: 'daily' as Frequency, hour: 8, minute: 0, weekday: 1, monthDay: 1 };
    this.state.overlay = {
      mode,
      task: { taskType: 'shell', enabled: true, taskConfig: {}, ...task },
      preset,
      saving: false,
      error: '',
    };
    this.rerender();
  }

  private handleEdit(id: string): void {
    const task = this.state.tasks.find(t => t.id === id);
    if (task) this.openOverlay('edit', { ...task });
  }

  private async handleExpand(id: string): Promise<void> {
    if (this.state.expandedId === id) {
      this.state.expandedId = null;
      this.rerender();
      return;
    }
    this.state.expandedId = id;
    this.rerender();
    if (!this.state.history[id]) {
      try {
        this.state.history[id] = await getScheduleRunLog(id);
      } catch {
        this.state.history[id] = [];
      }
      this.rerender();
    }
  }

  private async handleRunNow(id: string): Promise<void> {
    if (!this.canManage) return;
    this.state.runningId = id;
    this.rerender();
    let finished = false;
    const refreshTask = async (): Promise<ScheduledTask | undefined> => {
      this.state.tasks = await listScheduledTasks();
      if (this.state.expandedId === id) {
        this.state.history[id] = await getScheduleRunLog(id);
      }
      this.rerender();
      return this.state.tasks.find(t => t.id === id);
    };
    const refreshWhileRunning = async () => {
      while (!finished && this.isConnected) {
        await sleep(2000);
        if (finished || !this.isConnected) return;
        try {
          await refreshTask();
        } catch {
          // The original run request remains authoritative; polling is best-effort progress UI.
        }
      }
    };
    const progressPoll = refreshWhileRunning();
    try {
      await runScheduledTaskNow(id);
      finished = true;
      await progressPoll;
      let task = await refreshTask();
      while (task?.lastRunStatus === 'running' && this.isConnected) {
        await sleep(2000);
        task = await refreshTask();
      }
    } catch (e: any) {
      finished = true;
      await progressPoll;
      this.state.error = e?.message ?? 'Run failed';
    }
    this.state.runningId = null;
    this.rerender();
  }

  private async handleDelete(id: string): Promise<void> {
    if (!this.canManage) return;
    const task = this.state.tasks.find(t => t.id === id);
    if (!task) return;
    const confirmed = await showConfirm(`Delete scheduled task "${task.name}"?`, {
      title: 'Delete scheduled task',
      confirmLabel: 'Delete',
      cancelLabel: 'Keep',
      tone: 'danger',
    });
    if (!confirmed) return;
    try {
      await deleteScheduledTask(id);
      this.state.tasks = this.state.tasks.filter(t => t.id !== id);
      if (this.state.expandedId === id) this.state.expandedId = null;
    } catch (e: any) {
      this.state.error = e?.message ?? 'Delete failed';
    }
    this.rerender();
  }

  private collectOverlayValues(): Partial<ScheduledTask> {
    const ov = this.state.overlay!;
    const name    = (this.querySelector('#sw-name') as HTMLInputElement)?.value.trim() ?? '';
    const desc    = (this.querySelector('#sw-desc') as HTMLInputElement)?.value.trim() ?? '';
    const enabled = (this.querySelector('#sw-enabled') as HTMLInputElement)?.checked ?? true;

    // Task config
    const cfg: Record<string, any> = {};
    const type = ov.task.taskType!;
    if (type === 'report') {
      cfg.templateId = (this.querySelector('#sw-tmpl') as HTMLSelectElement)?.value ?? '';
      cfg.outputDir  = (this.querySelector('#sw-outdir') as HTMLInputElement)?.value.trim() ?? '';
    } else if (type === 'backup') {
      cfg.outputDir  = (this.querySelector('#sw-outdir') as HTMLInputElement)?.value.trim() ?? '';
      cfg.keepCount  = parseInt((this.querySelector('#sw-keepcount') as HTMLInputElement)?.value ?? '0') || 0;
    } else if (type === 'shell') {
      cfg.command = (this.querySelector('#sw-cmd') as HTMLTextAreaElement)?.value ?? '';
      cfg.timeout = parseInt((this.querySelector('#sw-timeout') as HTMLInputElement)?.value ?? '0') || 0;
    } else if (type === 'yaegi') {
      cfg.script  = (this.querySelector('#sw-script') as HTMLTextAreaElement)?.value ?? '';
      cfg.timeout = parseInt((this.querySelector('#sw-timeout') as HTMLInputElement)?.value ?? '0') || 0;
    } else if (type === 'command') {
      cfg.deviceName = (this.querySelector('#sw-device') as HTMLInputElement)?.value.trim() ?? '';
      cfg.tagPath = (this.querySelector('#sw-tagpath') as HTMLInputElement)?.value.trim() ?? '';
      cfg.value = parseCommandValue(commandValueText(this));
      cfg.timeout = parseInt((this.querySelector('#sw-timeout') as HTMLInputElement)?.value ?? '0') || 0;
    }

    const epVal = (this.querySelector('#sw-email-profile') as HTMLSelectElement)?.value;
    if (epVal) cfg.emailProfileId = parseInt(epVal);

    return {
      name, description: desc, enabled,
      taskType: type,
      taskConfig: cfg as any,
      schedule: presetToCron(ov.preset),
    };
  }

  private async handleSave(): Promise<void> {
    if (!this.canManage) return;
    if (!this.state.overlay) return;
    const ov = this.state.overlay;
    const data = this.collectOverlayValues();
    if (!data.name) { ov.error = 'Name is required.'; this.rerender(); return; }
    if (data.taskType === 'command') {
      const cfg = (data.taskConfig || {}) as Record<string, any>;
      if (!cfg.deviceName) { ov.error = 'Device Name is required.'; this.rerender(); return; }
      if (!cfg.tagPath) { ov.error = 'Tag Path is required.'; this.rerender(); return; }
      if (commandValueText(this).trim() === '') { ov.error = 'Value is required.'; this.rerender(); return; }
    }

    ov.saving = true;
    ov.error = '';
    this.rerender();

    try {
      if (ov.mode === 'create') {
        await createScheduledTask(data);
      } else {
        await updateScheduledTask(ov.task.id!, data);
      }
      this.state.overlay = null;
      await this.load();
    } catch (e: any) {
      ov.saving = false;
      ov.error = e?.message ?? 'Save failed';
      this.rerender();
    }
  }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function _esc(s: string): string {
  return String(s ?? '')
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function formatCommandValue(value: any): string {
  if (value === undefined) return '';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value);
  } catch {
    return String(value ?? '');
  }
}

function parseCommandValue(raw: string): any {
  const text = raw.trim();
  if (!text) return '';
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

function commandValueText(root: ParentNode): string {
  return (root.querySelector('#sw-value') as HTMLInputElement | null)?.value ?? '';
}

// ── Registration ──────────────────────────────────────────────────────────────

registerWidgetType({
  type:     'scheduler-widget',
  name:     'Scheduler',
  icon:     '⏱',
  category: 'System',
  defaultW: 24,
  defaultH: 16,
  minW:     12,
  minH:     8,
});

customElements.define('scheduler-widget', SchedulerWidget);
