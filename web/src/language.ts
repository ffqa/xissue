// Pure extension mapping: keep Monaco out of the initial page chunk.
export const EXT_LANG: Record<string, string> = {
  ts: 'typescript', tsx: 'typescript', js: 'javascript', jsx: 'javascript', mjs: 'javascript', cjs: 'javascript',
  json: 'json', css: 'css', scss: 'scss', less: 'less', html: 'html', htm: 'html', vue: 'html', svelte: 'html',
  md: 'markdown', markdown: 'markdown', go: 'go', py: 'python', rs: 'rust', java: 'java', kt: 'kotlin',
  rb: 'ruby', php: 'php', c: 'c', h: 'c', cpp: 'cpp', cc: 'cpp', hpp: 'cpp', cs: 'csharp', swift: 'swift',
  sh: 'shell', bash: 'shell', zsh: 'shell', yml: 'yaml', yaml: 'yaml', toml: 'ini', ini: 'ini', xml: 'xml',
  sql: 'sql', dockerfile: 'dockerfile', graphql: 'graphql', lua: 'lua', dart: 'dart', proto: 'protobuf',
};

export function languageOf(path: string): string {
  const base = path.split('/').pop()?.toLowerCase() ?? '';
  if (base === 'dockerfile') return 'dockerfile';
  if (base === 'makefile') return 'plaintext';
  const ext = base.includes('.') ? base.split('.').pop()! : '';
  return EXT_LANG[ext] ?? 'plaintext';
}
