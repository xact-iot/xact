import { BaseComponent } from './base-component';

type DialogTone = 'default' | 'danger';
type DialogRequest =
  | {
      kind: 'alert';
      title: string;
      message: string;
      confirmLabel: string;
      tone: DialogTone;
      resolve: () => void;
    }
  | {
      kind: 'confirm';
      title: string;
      message: string;
      confirmLabel: string;
      cancelLabel: string;
      tone: DialogTone;
      resolve: (value: boolean) => void;
    }
  | {
      kind: 'choice';
      title: string;
      message: string;
      choices: DialogChoice[];
      tone: DialogTone;
      resolve: (value: string) => void;
    };

type AlertOptions = {
  title?: string;
  confirmLabel?: string;
  tone?: DialogTone;
};

type ConfirmOptions = {
  title?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: DialogTone;
};

export type DialogChoice = {
  value: string;
  label: string;
  role?: 'primary' | 'danger' | 'secondary';
};

type ChoiceOptions = {
  title?: string;
  choices: DialogChoice[];
  tone?: DialogTone;
};

export class AppDialog extends BaseComponent {
  private queue: DialogRequest[] = [];
  private active: DialogRequest | null = null;

  protected render(): void {
    if (!this.active) {
      this.className = 'hidden';
      this.innerHTML = '';
      return;
    }

    const isDanger = this.active.tone === 'danger';
    // Build tints from the accent so they harmonise with every theme.
    const accentFg = isDanger ? 'var(--danger-color, #dc2626)' : 'var(--accent-color)';
    const iconClasses = `background: color-mix(in srgb, ${accentFg} 12%, var(--modal-bg)); color: ${accentFg}; border-color: color-mix(in srgb, ${accentFg} 30%, var(--border-color));`;
    const confirmClasses = isDanger
      ? `background: color-mix(in srgb, var(--danger-color, #dc2626) 22%, var(--modal-bg)); color: var(--danger-color, #dc2626); border-color: color-mix(in srgb, var(--danger-color, #dc2626) 55%, var(--border-color));`
      : `background: color-mix(in srgb, var(--accent-color) 18%, var(--modal-bg)); color: var(--accent-text); border-color: color-mix(in srgb, var(--accent-color) 55%, var(--border-color));`;

    this.className = 'fixed inset-0 flex items-center justify-center px-4';
    this.style.zIndex = '25000';

    this.innerHTML = `
      <div id="dialog-backdrop" class="absolute inset-0" style="background: rgba(2, 6, 23, 0.66); backdrop-filter: blur(3px);"></div>
      <div class="relative w-full max-w-lg overflow-hidden rounded-2xl border shadow-2xl"
           style="background: var(--modal-bg); color: var(--modal-text); border-color: var(--border-color);">
        <div class="px-6 py-5 border-b" style="border-color: color-mix(in srgb, var(--border-color) 70%, transparent);">
          <div class="flex items-start gap-4">
            <div class="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border"
                 style="${iconClasses}">
              ${isDanger ? this.renderDangerIcon() : this.renderInfoIcon()}
            </div>
            <div class="min-w-0 flex-1">
              <h2 class="text-lg font-semibold leading-6">${this.escapeHtml(this.active.title)}</h2>
              <p class="mt-2 text-sm leading-6 opacity-80 whitespace-pre-wrap">${this.escapeHtml(this.active.message)}</p>
            </div>
          </div>
        </div>
        <div class="flex items-center justify-end gap-3 px-6 py-4"
             style="background: color-mix(in srgb, var(--content-bg) 45%, var(--modal-bg));">
          ${this.active.kind === 'confirm'
            ? `<button id="dialog-cancel" class="rounded-xl border px-4 py-2 text-sm font-medium transition-opacity hover:opacity-85"
                     style="background: color-mix(in srgb, var(--accent-color) 8%, var(--modal-bg)); border-color: color-mix(in srgb, var(--accent-color) 30%, var(--border-color)); color: var(--modal-text);">
                 ${this.escapeHtml(this.active.cancelLabel)}
               </button>`
            : ''}
          ${this.active.kind === 'choice'
            ? this.active.choices.map(choice => `
              <button class="dialog-choice rounded-xl border px-4 py-2 text-sm font-${choice.role === 'primary' ? 'semibold' : 'medium'} transition-opacity hover:opacity-85"
                      data-value="${this.escapeHtml(choice.value)}"
                      style="${this.choiceStyle(choice)}">
                ${this.escapeHtml(choice.label)}
              </button>
            `).join('')
            : `<button id="dialog-confirm" class="rounded-xl border px-4 py-2 text-sm font-semibold transition-opacity hover:opacity-85"
                    style="${confirmClasses}">
              ${this.escapeHtml(this.active.confirmLabel)}
            </button>`}
        </div>
      </div>
    `;
  }

  protected attachEventListeners(): void {
    this.querySelector('#dialog-backdrop')?.addEventListener('click', this.handleBackdropClick);
    this.querySelector('#dialog-cancel')?.addEventListener('click', this.handleCancel);
    this.querySelector('#dialog-confirm')?.addEventListener('click', this.handleConfirm);
    this.querySelectorAll('.dialog-choice').forEach(btn => btn.addEventListener('click', this.handleChoice));
    document.addEventListener('keydown', this.handleKeydown);
  }

  protected detachEventListeners(): void {
    this.querySelector('#dialog-backdrop')?.removeEventListener('click', this.handleBackdropClick);
    this.querySelector('#dialog-cancel')?.removeEventListener('click', this.handleCancel);
    this.querySelector('#dialog-confirm')?.removeEventListener('click', this.handleConfirm);
    this.querySelectorAll('.dialog-choice').forEach(btn => btn.removeEventListener('click', this.handleChoice));
    document.removeEventListener('keydown', this.handleKeydown);
  }

  openAlert(message: string, options: AlertOptions = {}): Promise<void> {
    return new Promise(resolve => {
      this.queue.push({
        kind: 'alert',
        title: options.title ?? 'Notice',
        message,
        confirmLabel: options.confirmLabel ?? 'OK',
        tone: options.tone ?? 'default',
        resolve,
      });
      this.showNext();
    });
  }

  openConfirm(message: string, options: ConfirmOptions = {}): Promise<boolean> {
    return new Promise(resolve => {
      this.queue.push({
        kind: 'confirm',
        title: options.title ?? 'Confirm action',
        message,
        confirmLabel: options.confirmLabel ?? 'Confirm',
        cancelLabel: options.cancelLabel ?? 'Cancel',
        tone: options.tone ?? 'default',
        resolve,
      });
      this.showNext();
    });
  }

  openChoice(message: string, options: ChoiceOptions): Promise<string> {
    return new Promise(resolve => {
      this.queue.push({
        kind: 'choice',
        title: options.title ?? 'Choose an action',
        message,
        choices: options.choices,
        tone: options.tone ?? 'default',
        resolve,
      });
      this.showNext();
    });
  }

  private showNext(): void {
    if (this.active || this.queue.length === 0) return;
    this.active = this.queue.shift()!;
    this.rerender();
    requestAnimationFrame(() => {
      const button = this.querySelector<HTMLElement>('#dialog-confirm');
      (button ?? this.querySelector<HTMLElement>('.dialog-choice'))?.focus();
    });
  }

  private settle(value?: boolean | string): void {
    if (!this.active) return;
    const current = this.active;
    this.active = null;
    if (current.kind === 'confirm') {
      current.resolve(Boolean(value));
    } else if (current.kind === 'choice') {
      current.resolve(String(value ?? current.choices[0]?.value ?? ''));
    } else {
      current.resolve();
    }
    this.rerender();
    this.showNext();
  }

  private handleBackdropClick = (): void => {
    if (this.active?.kind === 'confirm') {
      this.settle(false);
    } else if (this.active?.kind === 'choice') {
      this.settle(this.active.choices[0]?.value);
    } else {
      this.settle();
    }
  };

  private handleCancel = (): void => {
    this.settle(false);
  };

  private handleConfirm = (): void => {
    this.settle(true);
  };

  private handleChoice = (event: Event): void => {
    const value = (event.currentTarget as HTMLElement).dataset.value;
    this.settle(value);
  };

  private handleKeydown = (event: KeyboardEvent): void => {
    if (!this.active) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      if (this.active.kind === 'confirm') this.settle(false);
      else if (this.active.kind === 'choice') this.settle(this.active.choices[0]?.value);
      else this.settle();
    } else if (event.key === 'Enter') {
      const target = event.target as HTMLElement | null;
      if (target && (target.tagName === 'TEXTAREA' || target.getAttribute('contenteditable') === 'true')) return;
      event.preventDefault();
      if (this.active.kind === 'choice') {
        this.settle(this.active.choices.find(choice => choice.role === 'primary')?.value ?? this.active.choices[0]?.value);
      } else {
        this.settle(true);
      }
    }
  };

  private choiceStyle(choice: DialogChoice): string {
    if (choice.role === 'primary') {
      return 'background: color-mix(in srgb, var(--accent-color) 18%, var(--modal-bg)); color: var(--accent-text); border-color: color-mix(in srgb, var(--accent-color) 55%, var(--border-color));';
    }
    if (choice.role === 'danger') {
      return 'background: color-mix(in srgb, var(--danger-color, #dc2626) 18%, var(--modal-bg)); color: var(--danger-color, #dc2626); border-color: color-mix(in srgb, var(--danger-color, #dc2626) 50%, var(--border-color));';
    }
    return 'background: color-mix(in srgb, var(--accent-color) 8%, var(--modal-bg)); border-color: color-mix(in srgb, var(--accent-color) 30%, var(--border-color)); color: var(--modal-text);';
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  private escapeHtml(value: string): string {
    return value
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  private renderInfoIcon(): string {
    return `
      <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M12 8h.01M11 12h1v4h1m-1 5a9 9 0 100-18 9 9 0 000 18z"/>
      </svg>
    `;
  }

  private renderDangerIcon(): string {
    return `
      <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M12 9v4m0 4h.01M10.29 3.86l-7.4 12.82A2 2 0 004.62 20h14.76a2 2 0 001.73-3.32l-7.39-12.82a2 2 0 00-3.46 0z"/>
      </svg>
    `;
  }
}

if (!customElements.get('app-dialog')) {
  customElements.define('app-dialog', AppDialog);
}

function ensureDialog(): AppDialog {
  let dialog = document.querySelector('app-dialog') as AppDialog | null;
  if (!dialog) {
    dialog = document.createElement('app-dialog') as AppDialog;
    document.body.appendChild(dialog);
  }
  return dialog;
}

export function showAlert(message: string, options?: AlertOptions): Promise<void> {
  return ensureDialog().openAlert(message, options);
}

export function showConfirm(message: string, options?: ConfirmOptions): Promise<boolean> {
  return ensureDialog().openConfirm(message, options);
}

export function showChoice(message: string, options: ChoiceOptions): Promise<string> {
  return ensureDialog().openChoice(message, options);
}
