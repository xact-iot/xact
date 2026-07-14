import { describe, expect, it } from 'vitest';
import { enhancePasswordInputs } from '../src/components/password-visibility';

describe('password visibility control', () => {
  it('toggles a password without changing its value', () => {
    const root = document.createElement('div');
    root.innerHTML = '<input id="password" type="password" value="secret">';

    enhancePasswordInputs(root);
    const input = root.querySelector('input')!;
    const button = root.querySelector('button')!;

    expect(button.getAttribute('aria-label')).toBe('Show password');
    button.click();
    expect(input.type).toBe('text');
    expect(input.value).toBe('secret');
    expect(button.getAttribute('aria-label')).toBe('Hide password');
    expect(button.getAttribute('aria-pressed')).toBe('true');

    button.click();
    expect(input.type).toBe('password');
    expect(input.value).toBe('secret');
    expect(button.getAttribute('aria-label')).toBe('Show password');
    expect(button.getAttribute('aria-pressed')).toBe('false');
  });

  it('does not add duplicate controls when enhanced again', () => {
    const root = document.createElement('div');
    root.innerHTML = '<input type="password">';

    enhancePasswordInputs(root);
    enhancePasswordInputs(root);

    expect(root.querySelectorAll('button')).toHaveLength(1);
  });
});
