import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { DICTS, type Lang } from './translations';

export type { Lang } from './translations';

interface I18nContextProps {
  lang: Lang;
  setLang: (l: Lang) => void;
  t: (k: string) => string;
}

const I18nContext = createContext<I18nContextProps | null>(null);



export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [lang, setLangState] = useState<Lang>('en');

  useEffect(() => {
    const saved = localStorage.getItem('lang') as Lang | null;
    if (saved && saved in DICTS) setLangState(saved);
  }, []);

  const setLang = (l: Lang) => {
    setLangState(l);
    try { localStorage.setItem('lang', l); } catch {}
  };

  const t = useMemo(() => (k: string) => {
    const d = DICTS[lang] || DICTS.en;
    return d[k] || DICTS.en[k] || k;
  }, [lang]);

  return (
    <I18nContext.Provider value={{ lang, setLang, t }}>
      {children}
    </I18nContext.Provider>
  );
}

export function useI18n() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error('useI18n must be used within I18nProvider');
  return ctx;
}

/** Supported language options for language selector dropdowns. */
export const LANGUAGE_OPTIONS: { value: Lang; label: string; native: string }[] = [
  { value: 'en', label: 'English', native: 'English' },
  { value: 'ha', label: 'Hausa', native: 'Hausa' },
  { value: 'yo', label: 'Yoruba', native: 'Yorùbá' },
  { value: 'ig', label: 'Igbo', native: 'Igbo' },
  { value: 'pcm', label: 'Pidgin', native: 'Naija' },
];
