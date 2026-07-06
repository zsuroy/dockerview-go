// Parse size string like "1.2MB" to bytes number
export const parseSize = (s: string): number => {
  if (!s || typeof s !== 'string') return 0;
  const m = s.match(/^([\d.]+)\s*([a-zA-Z]+)$/);
  if (!m) return 0;
  const units: Record<string, number> = { 
    'B': 1, 'KB': 1024, 'MB': 1024 ** 2, 'GB': 1024 ** 3, 'TB': 1024 ** 4 
  };
  const unit = m[2].toUpperCase();
  return parseFloat(m[1]) * (units[unit] || 1);
};

// Format bytes back to human readable string
export const formatBytes = (b: number): string => {
  if (isNaN(b) || b === null || b === undefined) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let val = b;
  while (val >= 1024 && i < u.length - 1) {
    val /= 1024;
    i++;
  }
  return `${val.toFixed(1)} ${u[i]}`;
};

// Memory usage percent from backend stats, with fallback for older servers.
export const getMemoryPercent = (memory: string, memoryPercent?: number): number => {
  if (memoryPercent !== undefined && !isNaN(memoryPercent)) {
    return Math.min(Math.max(memoryPercent, 0), 100);
  }
  return Math.min((parseSize(memory) / (1024 * 1024 * 1024)) * 100, 100) || 0;
};

// Base path helper for subpath reverse proxies
const currentPath = window.location.pathname;
export const basePath = currentPath.endsWith('/') 
  ? currentPath 
  : currentPath.substring(0, currentPath.lastIndexOf('/') + 1);
