export interface VisualScriptCategoryColors {
  background: string;
  text: string;
  border: string;
}

const fallback: VisualScriptCategoryColors = { background: '#46515c', text: '#f7fafc', border: '#606d79' };

const categoryColors: Record<string, VisualScriptCategoryColors> = {
  triggers: { background: '#31565b', text: '#f4fbfb', border: '#456c71' },
  conditions: { background: '#5a4d32', text: '#fff8e1', border: '#756644' },
  transforms: { background: '#3b506d', text: '#f4f8ff', border: '#526a89' },
  context: { background: '#504461', text: '#fbf7ff', border: '#6b5a80' },
  actions: { background: '#5b414b', text: '#fff7fa', border: '#755663' },
};

export function visualScriptCategoryColors(category?: string): VisualScriptCategoryColors {
  return categoryColors[(category || '').trim().toLowerCase()] || fallback;
}

export function visualScriptCategoryStyle(category?: string): string {
  const colors = visualScriptCategoryColors(category);
  return `--vs-category-bg:${colors.background};--vs-category-text:${colors.text};--vs-category-border:${colors.border}`;
}
