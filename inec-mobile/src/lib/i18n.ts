/**
 * Minimal i18n for the INEC mobile app (R5-116).
 *
 * Scope: the EC8A field flow (reports + result capture) — the screens an
 * ad-hoc officer at a rural PU must operate under pressure. English and
 * Hausa ship in-tree; the remaining screens are tracked as follow-up.
 *
 * Usage:
 *   const { t } = useI18n();
 *   <Text>{t('reports.submit')}</Text>
 */
import { useCallback, useEffect, useState } from 'react';
import AsyncStorage from '@react-native-async-storage/async-storage';

export type Language = 'en' | 'ha';

const LANG_KEY = 'inec_app_language';

const STRINGS: Record<string, { en: string; ha: string }> = {
  // ── Observer reports (EC8A photo flow) ──
  'reports.title': { en: 'New Report', ha: 'Sabuwar Rahoto' },
  'reports.puCode': { en: 'Polling Unit Code', ha: 'Lambar Sansanin Ƙuri\'a' },
  'reports.puCodePlaceholder': { en: 'e.g. PU-23-014-001', ha: 'misali PU-23-014-001' },
  'reports.description': { en: 'Description (optional)', ha: 'Bayani (abin zaɓi)' },
  'reports.descriptionPlaceholder': {
    en: 'Any observations about the results or process...',
    ha: 'Duk abin da ka lura game da sakamako ko tsari...',
  },
  'reports.takePhoto': { en: 'Take Photo of EC8A Form', ha: 'Ɗauki Hoto na Fom EC8A' },
  'reports.retakePhoto': { en: 'Retake Photo', ha: 'Sake Ɗaukar Hoto' },
  'reports.photoHint': { en: 'Photograph the result sheet for verification', ha: 'Ɗauki hoton takardar sakamako don tabbatarwa' },
  'reports.submit': { en: 'Submit Report', ha: 'Aika Rahoto' },
  'reports.submitting': { en: 'Submitting...', ha: 'Ana aikawa...' },
  'reports.resolvingElection': { en: 'Resolving election...', ha: 'Ana neman zaɓe...' },
  'reports.required': { en: 'Required', ha: 'Wajibi ne' },
  'reports.enterPuCode': { en: 'Enter the Polling Unit code', ha: 'Shigar da lambar sansanin ƙuri\'a' },
  'reports.photoRequired': {
    en: 'Take a photo of the EC8A form — the backend requires photo evidence',
    ha: 'Ɗauki hoton fom EC8A — ana buƙatar shaidar hoto',
  },
  'reports.electionUnavailable': { en: 'Election Unavailable', ha: 'Babu Zaɓe' },
  'reports.pendingUpload': { en: 'Pending Upload', ha: 'Ana Jiran Aikawa' },
  'reports.submittedReports': { en: 'Submitted Reports', ha: 'Rahotannin da aka Aika' },
  'reports.noReports': { en: 'No reports yet', ha: 'Babu rahoto tukuna' },
  'reports.noReportsDesc': {
    en: 'Submit your first observer report to help verify election results',
    ha: 'Aika rahoton farko na lura tare don taimakawa tabbatar da sakamakon zaɓe',
  },
  'reports.cameraAccess': { en: 'Camera Access Needed', ha: 'Ana Buƙatar Izinin Kyamara' },
  'reports.cameraAccessDesc': {
    en: 'Take photos of EC8A result sheets for verification and evidence',
    ha: 'Ɗauki hotunan takardun sakamako EC8A don tabbatarwa da shaida',
  },
  'reports.grantCamera': { en: 'Grant Camera Access', ha: 'Ba da Izinin Kyamara' },
  'reports.cancel': { en: 'Cancel', ha: 'Soke' },
  'reports.alignFrame': { en: 'Align EC8A form within frame', ha: 'Daidaita fom EC8A a cikin firam' },

  // ── Result capture ──
  'capture.title': { en: 'Capture EC8A Result', ha: 'Rubuta Sakamakon EC8A' },
  'capture.partyScores': { en: 'Party Scores', ha: 'Ƙuri\'un Jam\'iyyu' },
  'capture.addParty': { en: 'Add party', ha: 'Ƙara jam\'iyya' },
  'capture.accredited': { en: 'Accredited Voters', ha: 'Masu Ƙuri\'a da aka Tantance' },
  'capture.rejected': { en: 'Rejected Votes', ha: 'Ƙuri\'un da aka Ƙi' },
  'capture.submit': { en: 'Capture Result', ha: 'Rubuta Sakamako' },
  'capture.saving': { en: 'Saving...', ha: 'Ana adanawa...' },
  'capture.submitted': { en: 'Submitted', ha: 'An Aika' },
  'capture.submittedDesc': { en: 'The result was received by the server.', ha: 'Umarci ya karɓi sakamakon.' },
  'capture.queued': { en: 'Queued Offline', ha: 'An Ajiye don Aikawa' },
  'capture.queuedDesc': {
    en: 'The result is saved on this device and will sync automatically when connectivity returns.',
    ha: 'An adana sakamakon a wannan na\'ura; za a aika shi da kansa idan hanya ya dawo.',
  },
  'capture.pendingSync': { en: 'Pending Sync', ha: 'Ana Jiran Aikawa' },
  'capture.syncNow': { en: 'Sync now', ha: 'Aika yanzu' },
  'capture.syncing': { en: 'Syncing...', ha: 'Ana aikawa...' },
};

let currentLanguage: Language = 'en';
const listeners = new Set<(lang: Language) => void>();

export function getLanguage(): Language {
  return currentLanguage;
}

export async function initI18n(): Promise<void> {
  try {
    const stored = await AsyncStorage.getItem(LANG_KEY);
    if (stored === 'en' || stored === 'ha') currentLanguage = stored;
  } catch { /* keep default */ }
}

export async function setLanguage(lang: Language): Promise<void> {
  currentLanguage = lang;
  try { await AsyncStorage.setItem(LANG_KEY, lang); } catch { /* best-effort */ }
  listeners.forEach((l) => l(lang));
}

/** Translate a key in the active language, falling back to English. */
export function t(key: keyof typeof STRINGS | string): string {
  const entry = STRINGS[key];
  if (!entry) return key; // missing key is loud (renders the key), never blank
  return entry[currentLanguage] ?? entry.en;
}

/** React hook: re-renders when the language changes. */
export function useI18n(): { t: typeof t; language: Language; setLanguage: typeof setLanguage } {
  const [language, setLangState] = useState<Language>(currentLanguage);
  useEffect(() => {
    const listener = (lang: Language) => setLangState(lang);
    listeners.add(listener);
    if (currentLanguage === 'en') initI18n().then(() => setLangState(currentLanguage));
    return () => { listeners.delete(listener); };
  }, []);
  const translate = useCallback((key: string) => t(key), []);
  return { t: translate as typeof t, language, setLanguage };
}
