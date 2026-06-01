import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';

export type Lang = 'en' | 'ha' | 'yo' | 'ig';

type Dict = Record<string, string>;

const DICTS: Record<Lang, Dict> = {
  en: {
    street: 'Street', satellite: 'Satellite', compare: 'Compare',
    leading_party: 'Leading Party', completion: 'Completion %', zone: 'Geo-Political Zone',
    pu_markers: 'PU Markers', box_select: 'Box Select',
    export_csv: 'Export CSV', export_geojson: 'Export GeoJSON',
    selection: 'Selection', search_places: 'Search places...',
    anomaly_detection: 'AI Anomaly Detection', anomaly_desc: 'AI-powered result validation detecting statistical anomalies',
    integrity_score: 'Integrity Score', total_anomalies: 'Total Anomalies',
    ai_methods: 'AI Methods', benford_status: 'Benford Test',
    overview: 'Overview', benford_analysis: "Benford's Analysis", anomaly_list: 'Anomaly List',
    severity_distribution: 'Severity Distribution', integrity_breakdown: 'Integrity Breakdown',
    benford_first_digit: "Benford's First Digit Distribution", digit: 'Digit',
    observed: 'Observed %', expected_benford: 'Expected (Benford)', sample_size: 'Sample Size',
    filter_severity: 'Filter by Severity', all: 'All',
    polling_unit: 'Polling Unit', type: 'Type', severity: 'Severity', description: 'Description',
    no_anomalies: 'No anomalies detected', no_data: 'No data available', refresh: 'Refresh',
    benford_method_desc: 'Chi-square test of first-digit distribution against Benford\'s Law',
    zscore_method_desc: 'Z-score outlier detection for turnout anomalies',
    iqr_method_desc: 'IQR-based outlier detection for vote counts',
    dominance_method_desc: 'Detects single-party dominance (>90% vote share)',
    round_number_method_desc: 'Identifies suspicious round-number vote totals',
    sequential_method_desc: 'Detects identical/sequential patterns across adjacent polling units',
    sms_verification: 'SMS/USSD Verification', sms_desc: 'Verify election results via SMS or USSD — no internet required',
    sms_channel: 'SMS Channel', ussd_channel: 'USSD Channel',
    text_verify: 'Text to Verify', no_internet: 'No Internet Required', works_offline: 'Works Offline',
    sms_verify: 'SMS Verify', ussd_simulator: 'USSD Simulator', statistics: 'Statistics', user_guide: 'User Guide',
    sms_result_verify: 'SMS Result Verification', phone_number: 'Phone Number',
    phone_hint: 'Nigerian phone number with country code', polling_unit_code: 'Polling Unit Code',
    pu_code_hint: 'Enter the polling unit code to verify', sending: 'Sending...',
    verify_result: 'Verify Result', error: 'Error', result_found: 'Result Found',
    ussd_start_hint: 'Press Send to start a USSD session', enter_option: 'Enter option...',
    ussd_input: 'USSD input', ussd_instructions: 'Simulates a USSD session. Send empty to start, then enter menu options.',
    sms_ussd_stats: 'SMS/USSD Statistics', click_tab_load: 'Click this tab to load data',
    how_to_use: 'How to Use', sms_guide_title: 'Via SMS',
    sms_step_1: 'Send an SMS to the INEC shortcode with your polling unit code',
    sms_step_2: 'You will receive a reply with the verified result for that polling unit',
    sms_step_3: 'Results include vote counts for all parties and verification status',
    ussd_guide_title: 'Via USSD', ussd_step_1: 'Dial *347*123# from any mobile phone',
    ussd_step_2: 'Select option 1 to check results', ussd_step_3: 'Enter your state and polling unit code',
    ussd_step_4: 'View verified results on your screen',
    public_api: 'Public API', public_api_desc: 'Versioned API with key auth, rate limiting, and OpenAPI docs for third-party verification',
    api_version: 'API Version', rate_limit: 'Rate Limit', req_per_min: 'req/min',
    active_keys: 'Active Keys', api_docs: 'API Docs', api_keys: 'API Keys', usage: 'Usage', examples: 'Examples',
    api_endpoints: 'API Endpoints', auth_required: 'Auth', copied: 'Copied!',
    authentication: 'Authentication', auth_desc: 'Pass your API key via the X-API-Key header or api_key query parameter.',
    auth_query_param: 'Or as a query parameter:',
    generate_key: 'Generate API Key', key_name: 'Key Name', owner: 'Owner',
    generating: 'Generating...', key_generated: 'API Key Generated Successfully',
    key_warning: 'Save this key — it will not be shown again.',
    existing_keys: 'Existing API Keys', name: 'Name', permissions: 'Permissions', status: 'Status',
    active: 'Active', inactive: 'Inactive', no_keys: 'No API keys generated yet',
    api_usage: 'API Usage Statistics', code_examples: 'Code Examples',
  },
  ha: {
    street: 'Titin', satellite: 'Satilaid', compare: 'Kwatanta',
    leading_party: "Jam'iyya Mai Nasara", completion: 'Kashi na kammalawa', zone: 'Yankin Siyasa',
    pu_markers: 'Alamomin PU', box_select: 'Zaɓen Akwati',
    export_csv: 'Fitar da CSV', export_geojson: 'Fitar da GeoJSON',
    selection: 'Zaɓi', search_places: 'Nema wurare...',
    anomaly_detection: 'Gano Matsalolin AI', anomaly_desc: 'Tabbatar da sakamakon zaɓe ta hanyar AI',
    integrity_score: 'Maki Gaskiya', total_anomalies: 'Jimillar Matsaloli',
    ai_methods: 'Hanyoyin AI', benford_status: 'Gwajin Benford',
    overview: 'Bayani', benford_analysis: 'Nazarin Benford', anomaly_list: 'Jerin Matsaloli',
    severity_distribution: 'Rarraba Tsanani', integrity_breakdown: 'Rarraba Gaskiya',
    benford_first_digit: 'Rarraba Lambar Farko ta Benford', digit: 'Lamba',
    observed: 'Abin da aka gani %', expected_benford: 'Abin da ake tsammani', sample_size: 'Girman samfuri',
    filter_severity: 'Tace ta tsanani', all: 'Duka',
    polling_unit: 'Rumfar zaɓe', type: 'Iri', severity: 'Tsanani', description: 'Bayani',
    no_anomalies: 'Ba a gano matsala ba', no_data: 'Babu bayanai', refresh: 'Sabunta',
    sms_verification: 'Tabbatarwa ta SMS/USSD', sms_desc: 'Tabbatar da sakamakon zaɓe ta SMS ko USSD',
    sms_channel: 'Hanyar SMS', ussd_channel: 'Hanyar USSD',
    text_verify: 'Aika don tabbatarwa', no_internet: 'Ba a buƙatar Intanet', works_offline: 'Yana aiki ba tare da Intanet ba',
    sms_verify: 'Tabbatar ta SMS', ussd_simulator: 'Mai koyi da USSD', statistics: 'Ƙididdiga', user_guide: 'Jagorar mai amfani',
    phone_number: 'Lambar waya', polling_unit_code: 'Lambar rumfar zaɓe',
    verify_result: 'Tabbatar da sakamako', error: 'Kuskure', result_found: 'An sami sakamako',
    public_api: 'API na Jama\'a', public_api_desc: 'API mai sigar da makulli da iyaka don tabbatarwa',
    api_version: 'Sigar API', rate_limit: 'Iyakar buƙatu', active_keys: 'Makullan da ke aiki',
    api_docs: 'Takardun API', api_keys: 'Makullan API', usage: 'Amfani', examples: 'Misalai',
    generate_key: 'Ƙirƙiri makulli', owner: 'Mai shi', name: 'Suna',
    active: 'Yana aiki', inactive: 'Ba ya aiki',
  },
  yo: {
    street: 'Ọna', satellite: 'Satẹlaiti', compare: 'Fiwera',
    leading_party: 'Ẹgbẹ to n ṣaju', completion: 'Ipẹyà %', zone: 'Agbegbe Oṣelu',
    pu_markers: 'Awọn ami PU', box_select: 'Aṣayan Apoti',
    export_csv: 'Jade CSV', export_geojson: 'Jade GeoJSON',
    selection: 'Yiyan', search_places: 'Wa awọn ibi...',
    anomaly_detection: 'Iwadii Aiṣedeede AI', anomaly_desc: 'Ṣayẹwo abajade idibo pẹlu AI',
    integrity_score: 'Iwọn Otitọ', total_anomalies: 'Apapọ Aiṣedeede',
    ai_methods: 'Awọn ọna AI', benford_status: 'Idanwo Benford',
    overview: 'Akopọ', benford_analysis: 'Itupalẹ Benford', anomaly_list: 'Atokọ Aiṣedeede',
    severity_distribution: 'Pinpin Bi o ti buru', integrity_breakdown: 'Alaye Otitọ',
    benford_first_digit: 'Pinpin Nọmba Akọkọ Benford', digit: 'Nọmba',
    observed: 'Ti a ri %', expected_benford: 'Ti a nireti', sample_size: 'Iwọn ayẹwo',
    filter_severity: 'Ṣe àyọkà', all: 'Gbogbo',
    polling_unit: 'Ibudo idibo', type: 'Iru', severity: 'Bi o ṣe buru', description: 'Apejuwe',
    no_anomalies: 'Ko si aiṣedeede', no_data: 'Ko si data', refresh: 'Tun ṣe',
    sms_verification: 'Ìjẹ́rìísí SMS/USSD', sms_desc: 'Jẹrisi abajade idibo nipasẹ SMS tabi USSD',
    sms_channel: 'Ọna SMS', ussd_channel: 'Ọna USSD',
    text_verify: 'Fi ọrọ ranṣẹ lati jẹrisi', no_internet: 'Ko nilo Intanẹẹti', works_offline: 'N ṣiṣẹ laisi Intanẹẹti',
    sms_verify: 'Jẹrisi nipasẹ SMS', ussd_simulator: 'Ẹrọ USSD', statistics: 'Awọn iṣiro', user_guide: 'Itọsọna',
    phone_number: 'Nọmba foonu', polling_unit_code: 'Koodu ibudo idibo',
    verify_result: 'Jẹrisi abajade', error: 'Aṣiṣe', result_found: 'A ri abajade',
    public_api: 'API Gbogbogbo', public_api_desc: 'API pẹlu bọtini ati opin fun ìjẹ́rìísí',
    api_version: 'Ẹya API', rate_limit: 'Opin ibeere', active_keys: 'Awọn bọtini ti n ṣiṣẹ',
    api_docs: 'Iwe API', api_keys: 'Awọn bọtini API', usage: 'Lilo', examples: 'Awọn apẹẹrẹ',
    generate_key: 'Ṣẹda bọtini', owner: 'Oniwun', name: 'Orukọ',
    active: 'Nṣiṣẹ', inactive: 'Ko ṣiṣẹ',
  },
  ig: {
    street: 'Ụzọ', satellite: 'Satẹlaịtị', compare: 'Tụnyere',
    leading_party: 'Ụlọ ọrụ ndọrọ ndọrọ ọchịchị nke na-edu',
    completion: 'Pasent nke mmejuputa', zone: 'Mpaghara ndọrọ ndọrọ ọchịchị',
    pu_markers: 'Ihe ngosi PU', box_select: 'Nhọrọ igbe',
    export_csv: 'Zipụta CSV', export_geojson: 'Zipụta GeoJSON',
    selection: 'Nhọrọ', search_places: 'Chọọ ebe...',
    anomaly_detection: 'Nchọpụta Nsogbu AI', anomaly_desc: 'Nyocha nsonaazụ ntuli aka site na AI',
    integrity_score: 'Akara Eziokwu', total_anomalies: 'Mkpokọta Nsogbu',
    ai_methods: 'Ụzọ AI', benford_status: 'Ule Benford',
    overview: 'Nchịkọta', benford_analysis: 'Nyocha Benford', anomaly_list: 'Ndepụta Nsogbu',
    severity_distribution: 'Nkesa Njọ', integrity_breakdown: 'Nkọwa Eziokwu',
    benford_first_digit: 'Nkesa Nọmba Mbụ Benford', digit: 'Nọmba',
    observed: 'Ahụrụ %', expected_benford: 'A na-atụ anya', sample_size: 'Ọnụ ọgụgụ sample',
    filter_severity: 'Hazie site na njọ', all: 'Niile',
    polling_unit: 'Ebe ntuli aka', type: 'Udi', severity: 'Njọ', description: 'Nkọwa',
    no_anomalies: 'Enweghị nsogbu achọpụtara', no_data: 'Enweghị data', refresh: 'Mee ọhụrụ',
    sms_verification: 'Nyocha SMS/USSD', sms_desc: 'Nyochaa nsonaazụ ntuli aka site na SMS ma ọ bụ USSD',
    sms_channel: 'Ụzọ SMS', ussd_channel: 'Ụzọ USSD',
    text_verify: 'Dee ka ịnyochaa', no_internet: 'Enweghị Internet dị mkpa', works_offline: 'Na-arụ ọrụ na-enweghị Internet',
    sms_verify: 'Nyochaa site na SMS', ussd_simulator: 'Ngwa USSD', statistics: 'Ọnụ ọgụgụ', user_guide: 'Ntuziaka',
    phone_number: 'Nọmba ekwentị', polling_unit_code: 'Koodu ebe ntuli aka',
    verify_result: 'Nyochaa nsonaazụ', error: 'Njehie', result_found: 'Achọpụtara nsonaazụ',
    public_api: 'API Ọha', public_api_desc: 'API nwere igodo na oke maka nyocha',
    api_version: 'Ụdị API', rate_limit: 'Oke arịrịọ', active_keys: 'Igodo na-arụ ọrụ',
    api_docs: 'Akwụkwọ API', api_keys: 'Igodo API', usage: 'Ojiji', examples: 'Ihe atụ',
    generate_key: 'Mepụta igodo', owner: 'Onye nwe ya', name: 'Aha',
    active: 'Na-arụ ọrụ', inactive: 'Anaghị arụ ọrụ',
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
