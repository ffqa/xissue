import { useEffect, useMemo } from 'react';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { api, qk } from '../api';
import { readCache, writeCache } from '../logic/localCache';
import type { Issue, ProjectPRPage } from '../types';

export function useProjectPRPages(projectId: number, refetchInterval?: number) {
  const cacheKey = `project-pr-pages:${projectId}:all`;
  const cached = useMemo(() => readCache<ProjectPRPage>(cacheKey), [cacheKey]);
  const query = useInfiniteQuery({
    queryKey: qk.projectPRPages(projectId, 'all'),
    queryFn: ({ pageParam }) => api.projectPRPage(projectId, 'all', pageParam as string),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor || undefined,
    enabled: !!projectId,
    staleTime: 30_000,
    gcTime: 30 * 60_000,
    refetchInterval,
    initialData: cached?.data?.prs ? { pages: [cached.data], pageParams: [''] } : undefined,
    initialDataUpdatedAt: cached?.at,
    refetchOnMount: 'always',
  });
  useEffect(() => {
    const first = query.data?.pages[0];
    if (first?.prs?.length) writeCache(cacheKey, first);
  }, [cacheKey, query.data]);
  return query;
}

export function useProjectIssues(projectId: number) {
  const cacheKey = `project-issues:${projectId}`;
  const cached = useMemo(() => readCache<Issue[]>(cacheKey), [cacheKey]);
  const query = useQuery({
    queryKey: qk.issues(projectId),
    queryFn: () => api.issues(projectId),
    enabled: !!projectId,
    staleTime: 15_000,
    initialData: cached?.data?.length ? cached.data : undefined,
    initialDataUpdatedAt: cached?.data?.length ? cached.at : undefined,
    refetchOnMount: 'always',
  });
  useEffect(() => {
    if (query.data?.length) writeCache(cacheKey, query.data);
  }, [cacheKey, query.data]);
  return query;
}
