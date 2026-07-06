import { useState, useEffect } from 'react';
import type { Container } from '../types';
import { parseSize, basePath, getMemoryPercent } from '../utils';

export function useTelemetry(serverToken: string) {
  const [containers, setContainers] = useState<Container[]>([]);
  const [lastUpdate, setLastUpdate] = useState<string>('--:--:--');
  const [searchQuery, setSearchQuery] = useState<string>('');
  const [sortKey, setSortKey] = useState<'name' | 'cpu' | 'ram'>('name');
  const [filterKey, setFilterKey] = useState<'all' | 'running' | 'stopped'>('all');
  
  // Telemetry metric history for sparklines
  const [historyData, setHistoryData] = useState<Record<string, { cpu: number[]; ram: number[] }>>({});

  // EventSource stream with exponential-backoff reconnect
  useEffect(() => {
    let es: EventSource | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let retryDelay = 1000;
    const maxRetryDelay = 30000;
    let disposed = false;

    const handleMessage = (e: MessageEvent) => {
      retryDelay = 1000;
      try {
        const raw: any[] = JSON.parse(e.data);
        const normalized: Container[] = raw.map(c => {
          const norm: any = {};
          for (const key in c) {
            norm[key.toLowerCase()] = c[key];
          }
          return norm as Container;
        });

        setContainers(normalized);
        const now = new Date();
        setLastUpdate(now.toTimeString().split(' ')[0]);

        setHistoryData(prev => {
          const updated = { ...prev };
          normalized.forEach(c => {
            if (!c.id) return;
            if (!updated[c.id]) {
              updated[c.id] = { cpu: [], ram: [] };
            }
            let cpuVal = parseFloat(c.cpu);
            if (isNaN(cpuVal)) cpuVal = 0;
            const ramPercent = getMemoryPercent(c.memory, c.memorypercent);

            updated[c.id].cpu = [...updated[c.id].cpu, cpuVal].slice(-20);
            updated[c.id].ram = [...updated[c.id].ram, ramPercent].slice(-20);
          });
          return updated;
        });
      } catch (err) {
        console.error('Failed to parse SSE data', err);
      }
    };

    const connect = () => {
      if (disposed) return;
      const urlSuffix = serverToken ? `?token=${serverToken}` : '';
      es = new EventSource(`${basePath}stream${urlSuffix}`);
      es.onmessage = handleMessage;
      es.onerror = () => {
        es?.close();
        es = null;
        if (disposed) return;
        reconnectTimer = setTimeout(() => {
          retryDelay = Math.min(retryDelay * 2, maxRetryDelay);
          connect();
        }, retryDelay);
      };
    };

    connect();

    return () => {
      disposed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      es?.close();
    };
  }, [serverToken]);

  // UI Filtering & Sorting logic
  const isRunning = (c: Container) => c.status && !c.status.toLowerCase().includes('exit');
  
  const filteredContainers = containers
    .filter(c => {
      if (filterKey === 'running') return isRunning(c);
      if (filterKey === 'stopped') return !isRunning(c);
      return true;
    })
    .filter(c => {
      const query = searchQuery.toLowerCase();
      return (c.name && c.name.toLowerCase().includes(query)) || (c.id && c.id.toLowerCase().includes(query));
    })
    .sort((a, b) => {
      if (sortKey === 'cpu') {
        const cpuA = parseFloat(a.cpu) || 0;
        const cpuB = parseFloat(b.cpu) || 0;
        return cpuB - cpuA;
      }
      if (sortKey === 'ram') {
        const ramA = getMemoryPercent(a.memory, a.memorypercent);
        const ramB = getMemoryPercent(b.memory, b.memorypercent);
        return ramB - ramA;
      }
      return (a.name || '').localeCompare(b.name || '');
    });

  const runningCount = containers.filter(isRunning).length;
  const stoppedCount = containers.length - runningCount;

  // Health status counts
  const healthyCount = containers.filter(c => c.healthstatus === 'healthy').length;
  const warningCount = containers.filter(c => c.healthstatus === 'warning').length;
  const dangerousCount = containers.filter(c => c.healthstatus === 'dangerous').length;

  // Aggregate Stats
  const avgCpu = containers.length 
    ? (containers.reduce((acc, c) => acc + (parseFloat(c.cpu) || 0), 0) / containers.length).toFixed(1)
    : '0';
  const peakMemory = containers.length 
    ? Math.max(...containers.map(c => parseSize(c.memory))) 
    : 0;

  return {
    containers,
    filteredContainers,
    runningCount,
    stoppedCount,
    healthyCount,
    warningCount,
    dangerousCount,
    avgCpu,
    peakMemory,
    lastUpdate,
    searchQuery,
    setSearchQuery,
    sortKey,
    setSortKey,
    filterKey,
    setFilterKey,
    historyData,
    isRunning
  };
}
