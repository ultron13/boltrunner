'use client';

import { useEffect, useRef, useState } from 'react';
import { getRun, GetRunResponse } from '@/lib/api-client';

const TERMINAL_STATUSES = new Set(['completed', 'failed', 'stopped']);

export function useRunPolling(runId: string, intervalMs = 1500) {
  const [data, setData] = useState<GetRunResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const stopped = useRef(false);

  useEffect(() => {
    stopped.current = false;

    async function poll() {
      if (stopped.current) return;
      try {
        const resp = await getRun(runId);
        if (stopped.current) return;
        setData(resp);
        if (!TERMINAL_STATUSES.has(resp.run.status)) {
          setTimeout(poll, intervalMs);
        }
      } catch (err) {
        if (!stopped.current) {
          setError(err instanceof Error ? err.message : 'failed to fetch run');
        }
      }
    }

    poll();
    return () => {
      stopped.current = true;
    };
  }, [runId, intervalMs]);

  return { data, error };
}
