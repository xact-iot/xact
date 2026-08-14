export interface VisualScriptCategoryColors {
  background: string;
  text: string;
  border: string;
}

const fallback: VisualScriptCategoryColors = { background: '#46515c', text: '#f7fafc', border: '#606d79' };

const categoryColors: Record<string, VisualScriptCategoryColors> = {
  triggers: { background: '#245c48', text: '#f2fff9', border: '#3b8066' },
  conditions: { background: '#66501f', text: '#fff9df', border: '#8c7130' },
  transforms: { background: '#334f7a', text: '#f3f7ff', border: '#4d70a3' },
  variables: { background: '#504461', text: '#fbf7ff', border: '#6b5a80' },
  actions: { background: '#673848', text: '#fff4f7', border: '#8d5064' },
};

export function visualScriptCategoryColors(category?: string): VisualScriptCategoryColors {
  return categoryColors[(category || '').trim().toLowerCase()] || fallback;
}

export function visualScriptCategoryStyle(category?: string): string {
  const colors = visualScriptCategoryColors(category);
  return `--vs-category-bg:${colors.background};--vs-category-text:${colors.text};--vs-category-border:${colors.border}`;
}
