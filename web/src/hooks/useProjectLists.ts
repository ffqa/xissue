import { useEffect, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api, qk } from '../api';
import { readCache, writeCache } from '../logic/localCache';
import type { Issue, RemotePR } from '../types';

export function useProjectPRs(projectId: number, refetchInterval?: number) {
  const cacheKey = `project-prs:${projectId}:all`;
  const cached = useMemo(() => readCache<RemotePR[]>(cacheKey), [cacheKey]);
  const query = useQuery({
    queryKey: qk.projectPRs(projectId, 'all'),
    queryFn: () => api.projectPRs(projectId, 'all'),
    enabled: !!projectId,
    staleTime: 30_000,
    gcTime: 30 * 60_000,
    refetchInterval,
    initialData: cached?.data?.length ? cached.data : undefined,
    initialDataUpdatedAt: cached?.data?.length ? cached.at : undefined,
    refetchOnMount: 'always',
  });
  useEffect(() => {
    if (query.data?.length) writeCache(cacheKey, query.data);
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
