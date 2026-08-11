import { Search, Languages, Sun, Moon, Trash2 } from 'lucide-react';
import { basePath } from '../utils';
import { useTranslation } from '../i18n';
import type { Theme } from '../hooks/useTheme';

interface HeaderProps {
  totalCount: number;
  runningCount: number;
  stoppedCount: number;
  searchQuery: string;
  setSearchQuery: (q: string) => void;
  sortKey: 'name' | 'cpu' | 'ram';
  setSortKey: (k: 'name' | 'cpu' | 'ram') => void;
  filterKey: 'all' | 'running' | 'stopped';
  setFilterKey: (f: 'all' | 'running' | 'stopped') => void;
  theme: Theme;
  onToggleTheme: () => void;
  onCleanup: () => void;
}

export function Header({
  totalCount, runningCount, stoppedCount,
  searchQuery, setSearchQuery,
  sortKey, setSortKey,
  filterKey, setFilterKey,
  theme, onToggleTheme, onCleanup
}: HeaderProps) {
  const { t, language, toggleLanguage } = useTranslation();

  return (
    <div className="sticky top-[25px] z-50 flex flex-wrap justify-between items-center gap-5 p-3.5 px-7 rounded-3xl glass-panel shadow-lg mb-[50px]">
      <div className="flex items-center gap-3">
        <img src={`${basePath}logo.svg`} className="w-7 h-7 rounded-lg object-contain shadow-md" alt="DockerView Logo" />
        <div className="flex flex-col justify-start">
          <h1 className="text-[13px] font-extrabold m-0 leading-tight tracking-[1.5px] text-text">
            {t('header.title')} <span className="text-accent-cyan">{t('header.subtitle')}</span>
          </h1>
          <div className="flex items-center gap-1 mt-0.5 text-[9px] font-bold text-accent-cyan tracking-wide">
            <span className="w-1.5 h-1.5 rounded-full bg-accent-cyan live-pulse" />
            <span>{t('header.liveTelemetry')}</span>
          </div>
        </div>
      </div>

      {/* Filter Tabs */}
      <div className="flex gap-1.5 bg-surface-1 border border-border-subtle p-0.5 rounded-xl">
        <button
          onClick={() => setFilterKey('all')}
          className={`flex items-center gap-1.5 px-3 py-1.5 text-[10px] font-bold tracking-wide rounded-lg transition-all ${
            filterKey === 'all' ? 'bg-surface-3 border border-surface-5 text-text' : 'text-text-dim border border-transparent hover:text-text'
          }`}
        >
          {t('header.filterAll')} <span className="bg-surface-2 border border-border-subtle text-[9px] px-1 py-0.2 rounded font-mono">{totalCount}</span>
        </button>
        <button
          onClick={() => setFilterKey('running')}
          className={`flex items-center gap-1.5 px-3 py-1.5 text-[10px] font-bold tracking-wide rounded-lg transition-all ${
            filterKey === 'running' ? 'bg-surface-3 border border-surface-5 text-text' : 'text-text-dim border border-transparent hover:text-text'
          }`}
        >
          {t('header.filterRunning')} <span className="bg-surface-2 border border-border-subtle text-[9px] px-1 py-0.2 rounded font-mono">{runningCount}</span>
        </button>
        <button
          onClick={() => setFilterKey('stopped')}
          className={`flex items-center gap-1.5 px-3 py-1.5 text-[10px] font-bold tracking-wide rounded-lg transition-all ${
            filterKey === 'stopped' ? 'bg-surface-3 border border-surface-5 text-text' : 'text-text-dim border border-transparent hover:text-text'
          }`}
        >
          {t('header.filterStopped')} <span className="bg-surface-2 border border-border-subtle text-[9px] px-1 py-0.2 rounded font-mono">{stoppedCount}</span>
        </button>
      </div>

      {/* Search Input */}
      <div className="relative flex items-center grow md:max-w-[340px]">
        <Search className="absolute left-3 w-3.5 h-3.5 text-text-dim pointer-events-none" />
        <input
          type="text"
          placeholder={t('header.searchPlaceholder')}
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="w-full bg-surface-2 hover:bg-surface-4 border border-border-light hover:border-border-default rounded-xl py-2 pl-9 pr-4 text-text text-[12px] font-semibold transition-all focus:outline-none focus:border-accent-cyan/40 focus:bg-surface-4"
        />
      </div>

      {/* Sorting selectors */}
      <div className="flex items-center gap-3 text-[10px] font-bold tracking-wide text-text-dim">
        {t('header.sortBy')}
        <div className="flex bg-surface-1 border border-border-subtle p-0.5 rounded-lg">
          <button
            onClick={() => setSortKey('name')}
            className={`px-2.5 py-1 rounded text-[9px] font-bold ${sortKey === 'name' ? 'bg-surface-2 text-text' : 'hover:text-text'}`}
          >
            {t('header.sortName')}
          </button>
          <button
            onClick={() => setSortKey('cpu')}
            className={`px-2.5 py-1 rounded text-[9px] font-bold ${sortKey === 'cpu' ? 'bg-surface-2 text-text' : 'hover:text-text'}`}
          >
            {t('header.sortCpu')}
          </button>
          <button
            onClick={() => setSortKey('ram')}
            className={`px-2.5 py-1 rounded text-[9px] font-bold ${sortKey === 'ram' ? 'bg-surface-2 text-text' : 'hover:text-text'}`}
          >
            {t('header.sortRam')}
          </button>
        </div>
      </div>

      {/* Language & Theme Controls */}
      <div className="flex items-center gap-2 break-inside-avoid">
        <button
          onClick={onCleanup}
          className="flex items-center gap-1.5 px-3 py-1.5 text-[10px] font-bold tracking-wide rounded-lg bg-danger/10 hover:bg-danger/20 border border-danger/30 hover:border-danger/50 text-danger transition-all cursor-pointer"
          title={t('header.cleanupTooltip')}
        >
          <Trash2 className="w-3.5 h-3.5" />
          {t('header.cleanup')}
        </button>
        <button
          onClick={toggleLanguage}
          className="flex items-center gap-1.5 px-3 py-1.5 text-[10px] font-bold tracking-wide rounded-lg bg-surface-1 hover:bg-surface-2 border border-border-subtle hover:border-border-default text-text-dim hover:text-text transition-all cursor-pointer"
          title={language === 'en' ? '切换到中文' : 'Switch to English'}
        >
          <Languages className="w-3.5 h-3.5" />
          {language === 'en' ? '中文' : 'EN'}
        </button>
        <button
          onClick={onToggleTheme}
          className="theme-toggle-btn"
          title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          aria-label="Toggle theme"
        >
          {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
        </button>
      </div>
    </div>
  );
}
