const PREFIX = 'xissue:cache:v1:';
const MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;

export interface CachedValue<T> {
  data: T;
  at: number;
}

export function readCache<T>(key: string): CachedValue<T> | null {
  try {
    const raw = localStorage.getItem(PREFIX + key);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as CachedValue<T>;
    if (!parsed || typeof parsed.at !== 'number' || !('data' in parsed)) return null;
    if (Date.now() - parsed.at > MAX_AGE_MS) {
      localStorage.removeItem(PREFIX + key);
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

export function writeCache<T>(key: string, data: T): void {
  try {
    localStorage.setItem(PREFIX + key, JSON.stringify({ data, at: Date.now() } satisfies CachedValue<T>));
  } catch {
    // Storage may be full or disabled; cache is best-effort.
  }
}
