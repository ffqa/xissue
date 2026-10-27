// isLocalGitPath reports whether a repo remote URL points at a filesystem path
// that this server can use directly (as opposed to a hosted Git URL).
export function isLocalGitPath(url: string): boolean {
  const v = url.trim();
  return (
    v.startsWith('/') ||
    v.startsWith('./') ||
    v.startsWith('../') ||
    v.startsWith('~/') ||
    v.startsWith('file://') ||
    /^[a-zA-Z]:[\\/]/.test(v)
  );
}
