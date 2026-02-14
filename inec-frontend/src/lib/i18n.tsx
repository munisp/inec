import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';

export type Lang = 'en' | 'ha' | 'yo' | 'ig';

type Dict = Record<string, string>;

const DICTS: Record<Lang, Dict> = {
  en: {
    street: 'Street',
    satellite: 'Satellite',
    compare: 'Compare',
    leading_party: 'Leading Party',
    completion: 'Completion %',
    zone: 'Geo-Political Zone',
    pu_markers: 'PU Markers',
    box_select: 'Box Select',
    export_csv: 'Export CSV',
    export_geojson: 'Export GeoJSON',
    selection: 'Selection',
    search_places: 'Search places...'
  },
  ha: {
    street: 'Titin',
    satellite: 'Satilaid',
    compare: 'Kwatanta',
    leading_party: 'Jam’iyya Mai Nasara',
    completion: 'Kashi na kammalawa',
    zone: 'Yankin Siyasa',
    pu_markers: 'Alamomin PU',
    box_select: 'Zaɓen Akwati',
    export_csv: 'Fitar da CSV',
    export_geojson: 'Fitar da GeoJSON',
    selection: 'Zaɓi',
    search_places: 'Nema wurare...'
  },
  yo: {
    street: 'Ọna',
    satellite: 'Satẹlaiti',
    compare: 'Fiwera',
    leading_party: 'Ẹgbẹ to n ṣaju',
    completion: 'Ipẹyà %',
    zone: 'Agbegbe Oṣelu',
    pu_markers: 'Awọn ami PU',
    box_select: 'Aṣayan Apoti',
    export_csv: 'Jade CSV',
    export_geojson: 'Jade GeoJSON',
    selection: 'Yiyan',
    search_places: 'Wa awọn ibi...'
  },
  ig: {
    street: 'Ụzọ',
    satellite: 'Satẹlaịtị',
    compare: 'Tụnyere',
    leading_party: 'Ụlọ ọrụ ndọrọ ndọrọ ọchịchị nke na-edu',
    completion: 'Pasent nke mmejuputa',
    zone: 'Mpaghara ndọrọ ndọrọ ọchịchị',
    pu_markers: 'Ihe ngosi PU',
    box_select: 'Nhọrọ igbe',
    export_csv: 'Zipụta CSV',
    export_geojson: 'Zipụta GeoJSON',
    selection: 'Nhọrọ',
    search_places: 'Chọọ ebe...'
  }
};

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
    if (saved) setLangState(saved);
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
