const EYE_ICON = `
  <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor"
       stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
    <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/>
    <circle cx="12" cy="12" r="3"/>
  </svg>`;

const EYE_OFF_ICON = `
  <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor"
       stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
    <path d="m3 3 18 18"/>
    <path d="M10.6 10.6a2 2 0 0 0 2.8 2.8"/>
    <path d="M9.9 4.2A10.8 10.8 0 0 1 12 4c6.5 0 10 8 10 8a18.5 18.5 0 0 1-2.1 3.2"/>
    <path d="M6.6 6.6C3.5 8.7 2 12 2 12s3.5 8 10 8a9.8 9.8 0 0 0 4.1-.9"/>
  </svg>`;

/** Adds an accessible show/hide control to password inputs under `root`. */
export function enhancePasswordInputs(root: ParentNode): void {
  root.querySelectorAll<HTMLInputElement>('input[type="password"]').forEach(input => {
    if (input.dataset.passwordVisibility === 'true') return;
    input.dataset.passwordVisibility = 'true';

    const wrapper = document.createElement('div');
    wrapper.style.cssText = 'position:relative;width:100%;';
    input.parentNode?.insertBefore(wrapper, input);
    wrapper.appendChild(input);
    input.style.paddingRight = '2.5rem';

    const button = document.createElement('button');
    button.type = 'button';
    button.setAttribute('aria-label', 'Show password');
    button.setAttribute('aria-pressed', 'false');
    button.title = 'Show password';
    button.innerHTML = EYE_ICON;
    button.style.cssText = [
      'position:absolute', 'right:0.7rem', 'top:50%', 'transform:translateY(-50%)',
      'display:flex', 'align-items:center', 'justify-content:center', 'width:1.25rem',
      'height:1.25rem', 'padding:0', 'border:0', 'background:transparent',
      'color:inherit', 'opacity:0.55', 'cursor:pointer', 'z-index:1'
    ].join(';');
    button.querySelector('svg')?.setAttribute('style', 'width:100%;height:100%;');

    button.addEventListener('click', () => {
      const visible = input.type === 'text';
      input.type = visible ? 'password' : 'text';
      const action = visible ? 'Show password' : 'Hide password';
      button.setAttribute('aria-label', action);
      button.setAttribute('aria-pressed', String(!visible));
      button.title = action;
      button.innerHTML = visible ? EYE_ICON : EYE_OFF_ICON;
      button.querySelector('svg')?.setAttribute('style', 'width:100%;height:100%;');
    });
    wrapper.appendChild(button);
  });
}
