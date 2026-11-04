/** Sub-route key under a project, or null when the path is not a project route. */
export function projectViewOf(pathname: string): string | null {
  const m = pathname.match(/^\/projects\/(\d+)(?:\/([^/]+))?/);
  return m ? m[2] ?? '' : null;
}
