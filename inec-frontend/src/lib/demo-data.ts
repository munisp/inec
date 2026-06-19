/**
 * Demo/fallback data for when the backend API is unavailable.
 * Pages use these as defaults so the UI renders with realistic Nigerian election data
 * even without a running backend. In production, live API data overrides these.
 */

const NIGERIAN_PARTIES = [
  { code: 'APC', name: 'All Progressives Congress', abbreviation: 'APC', color: '#2563eb', total_votes: 8794726 },
  { code: 'PDP', name: "People's Democratic Party", abbreviation: 'PDP', color: '#dc2626', total_votes: 6984520 },
  { code: 'LP', name: 'Labour Party', abbreviation: 'LP', color: '#16a34a', total_votes: 6101533 },
  { code: 'NNPP', name: 'New Nigeria Peoples Party', abbreviation: 'NNPP', color: '#d97706', total_votes: 1496687 },
  { code: 'APGA', name: 'All Progressives Grand Alliance', abbreviation: 'APGA', color: '#7c3aed', total_votes: 422215 },
  { code: 'SDP', name: 'Social Democratic Party', abbreviation: 'SDP', color: '#0891b2', total_votes: 189043 },
  { code: 'ADC', name: 'African Democratic Congress', abbreviation: 'ADC', color: '#db2777', total_votes: 105061 },
  { code: 'YPP', name: 'Young Progressives Party', abbreviation: 'YPP', color: '#65a30d', total_votes: 52347 },
];

const NIGERIA_STATES = [
  { code: 'AB', name: 'Abia', geo_zone: 'South East', capital: 'Umuahia', lat: 5.4527, lng: 7.5248, total_pus: 2980, reported_pus: 2891, total_votes: 238012, total_cast: 231456 },
  { code: 'AD', name: 'Adamawa', geo_zone: 'North East', capital: 'Yola', lat: 9.3265, lng: 12.3984, total_pus: 2968, reported_pus: 2750, total_votes: 197234, total_cast: 189456 },
  { code: 'AK', name: 'Akwa Ibom', geo_zone: 'South South', capital: 'Uyo', lat: 5.0080, lng: 7.8494, total_pus: 2980, reported_pus: 2910, total_votes: 285023, total_cast: 278034 },
  { code: 'AN', name: 'Anambra', geo_zone: 'South East', capital: 'Awka', lat: 6.2100, lng: 7.0700, total_pus: 5720, reported_pus: 5612, total_votes: 321098, total_cast: 312456 },
  { code: 'BA', name: 'Bauchi', geo_zone: 'North East', capital: 'Bauchi', lat: 10.3158, lng: 9.8442, total_pus: 4014, reported_pus: 3892, total_votes: 456123, total_cast: 445678 },
  { code: 'BY', name: 'Bayelsa', geo_zone: 'South South', capital: 'Yenagoa', lat: 4.7719, lng: 6.0699, total_pus: 1804, reported_pus: 1723, total_votes: 138234, total_cast: 132456 },
  { code: 'BE', name: 'Benue', geo_zone: 'North Central', capital: 'Makurdi', lat: 7.7322, lng: 8.5391, total_pus: 4911, reported_pus: 4782, total_votes: 367890, total_cast: 356123 },
  { code: 'BO', name: 'Borno', geo_zone: 'North East', capital: 'Maiduguri', lat: 11.8333, lng: 13.1500, total_pus: 3939, reported_pus: 3456, total_votes: 302145, total_cast: 289567 },
  { code: 'CR', name: 'Cross River', geo_zone: 'South South', capital: 'Calabar', lat: 4.9500, lng: 8.3500, total_pus: 3281, reported_pus: 3190, total_votes: 198456, total_cast: 191234 },
  { code: 'DE', name: 'Delta', geo_zone: 'South South', capital: 'Asaba', lat: 6.2000, lng: 6.7333, total_pus: 4619, reported_pus: 4498, total_votes: 342567, total_cast: 334890 },
  { code: 'EB', name: 'Ebonyi', geo_zone: 'South East', capital: 'Abakaliki', lat: 6.3249, lng: 8.1137, total_pus: 2558, reported_pus: 2489, total_votes: 187654, total_cast: 181234 },
  { code: 'ED', name: 'Edo', geo_zone: 'South South', capital: 'Benin City', lat: 6.3350, lng: 5.6037, total_pus: 4519, reported_pus: 4401, total_votes: 298765, total_cast: 289456 },
  { code: 'EK', name: 'Ekiti', geo_zone: 'South West', capital: 'Ado-Ekiti', lat: 7.6300, lng: 5.2200, total_pus: 2445, reported_pus: 2398, total_votes: 176543, total_cast: 171234 },
  { code: 'EN', name: 'Enugu', geo_zone: 'South East', capital: 'Enugu', lat: 6.4584, lng: 7.5464, total_pus: 4145, reported_pus: 4012, total_votes: 267890, total_cast: 261234 },
  { code: 'FC', name: 'FCT Abuja', geo_zone: 'North Central', capital: 'Abuja', lat: 9.0579, lng: 7.4951, total_pus: 2822, reported_pus: 2756, total_votes: 378456, total_cast: 371234 },
  { code: 'GO', name: 'Gombe', geo_zone: 'North East', capital: 'Gombe', lat: 10.2897, lng: 11.1711, total_pus: 2352, reported_pus: 2278, total_votes: 198765, total_cast: 191234 },
  { code: 'IM', name: 'Imo', geo_zone: 'South East', capital: 'Owerri', lat: 5.4927, lng: 7.0262, total_pus: 3860, reported_pus: 3754, total_votes: 276543, total_cast: 268901 },
  { code: 'JI', name: 'Jigawa', geo_zone: 'North West', capital: 'Dutse', lat: 11.7000, lng: 9.3500, total_pus: 2817, reported_pus: 2734, total_votes: 334567, total_cast: 327890 },
  { code: 'KD', name: 'Kaduna', geo_zone: 'North West', capital: 'Kaduna', lat: 10.5105, lng: 7.4165, total_pus: 7018, reported_pus: 6891, total_votes: 567890, total_cast: 556123 },
  { code: 'KN', name: 'Kano', geo_zone: 'North West', capital: 'Kano', lat: 12.0022, lng: 8.5920, total_pus: 8012, reported_pus: 7823, total_votes: 876543, total_cast: 862345 },
  { code: 'KT', name: 'Katsina', geo_zone: 'North West', capital: 'Katsina', lat: 13.0059, lng: 7.6000, total_pus: 4869, reported_pus: 4723, total_votes: 456789, total_cast: 447890 },
  { code: 'KB', name: 'Kebbi', geo_zone: 'North West', capital: 'Birnin Kebbi', lat: 12.4533, lng: 4.1975, total_pus: 2605, reported_pus: 2534, total_votes: 234567, total_cast: 228901 },
  { code: 'KO', name: 'Kogi', geo_zone: 'North Central', capital: 'Lokoja', lat: 7.8000, lng: 6.7333, total_pus: 3196, reported_pus: 3089, total_votes: 234567, total_cast: 227890 },
  { code: 'KW', name: 'Kwara', geo_zone: 'North Central', capital: 'Ilorin', lat: 8.4966, lng: 4.5426, total_pus: 2371, reported_pus: 2312, total_votes: 198765, total_cast: 192345 },
  { code: 'LA', name: 'Lagos', geo_zone: 'South West', capital: 'Ikeja', lat: 6.5244, lng: 3.3792, total_pus: 13325, reported_pus: 13012, total_votes: 1234567, total_cast: 1218901 },
  { code: 'NA', name: 'Nasarawa', geo_zone: 'North Central', capital: 'Lafia', lat: 8.4966, lng: 8.5147, total_pus: 2515, reported_pus: 2445, total_votes: 187654, total_cast: 182345 },
  { code: 'NI', name: 'Niger', geo_zone: 'North Central', capital: 'Minna', lat: 9.6139, lng: 6.5569, total_pus: 3743, reported_pus: 3634, total_votes: 289456, total_cast: 281234 },
  { code: 'OG', name: 'Ogun', geo_zone: 'South West', capital: 'Abeokuta', lat: 7.1608, lng: 3.3509, total_pus: 4959, reported_pus: 4856, total_votes: 345678, total_cast: 338901 },
  { code: 'ON', name: 'Ondo', geo_zone: 'South West', capital: 'Akure', lat: 7.2500, lng: 5.2100, total_pus: 3009, reported_pus: 2934, total_votes: 218765, total_cast: 212345 },
  { code: 'OS', name: 'Osun', geo_zone: 'South West', capital: 'Osogbo', lat: 7.7827, lng: 4.5418, total_pus: 3010, reported_pus: 2945, total_votes: 228765, total_cast: 221234 },
  { code: 'OY', name: 'Oyo', geo_zone: 'South West', capital: 'Ibadan', lat: 7.3775, lng: 3.9470, total_pus: 5619, reported_pus: 5498, total_votes: 398765, total_cast: 391234 },
  { code: 'PL', name: 'Plateau', geo_zone: 'North Central', capital: 'Jos', lat: 9.8965, lng: 8.8583, total_pus: 4038, reported_pus: 3923, total_votes: 312456, total_cast: 304567 },
  { code: 'RI', name: 'Rivers', geo_zone: 'South South', capital: 'Port Harcourt', lat: 4.8156, lng: 7.0498, total_pus: 6866, reported_pus: 6712, total_votes: 478234, total_cast: 468901 },
  { code: 'SO', name: 'Sokoto', geo_zone: 'North West', capital: 'Sokoto', lat: 13.0629, lng: 5.2411, total_pus: 3048, reported_pus: 2934, total_votes: 267890, total_cast: 261234 },
  { code: 'TA', name: 'Taraba', geo_zone: 'North East', capital: 'Jalingo', lat: 8.8930, lng: 11.3592, total_pus: 2830, reported_pus: 2712, total_votes: 198765, total_cast: 191234 },
  { code: 'YO', name: 'Yobe', geo_zone: 'North East', capital: 'Damaturu', lat: 11.7490, lng: 11.9659, total_pus: 2341, reported_pus: 2198, total_votes: 187654, total_cast: 181234 },
  { code: 'ZA', name: 'Zamfara', geo_zone: 'North West', capital: 'Gusau', lat: 12.1628, lng: 6.6635, total_pus: 2815, reported_pus: 2698, total_votes: 223456, total_cast: 218901 },
];

// Compute totals
const TOTAL_PUS = NIGERIA_STATES.reduce((s, st) => s + st.total_pus, 0);
const REPORTED_PUS = NIGERIA_STATES.reduce((s, st) => s + st.reported_pus, 0);

export const DEMO_DASHBOARD = {
  election: { title: '2027 Presidential Election', status: 'voting', election_date: '2027-02-25' },
  total_polling_units: TOTAL_PUS,
  results_received: REPORTED_PUS,
  completion_percentage: Math.round((REPORTED_PUS / TOTAL_PUS) * 1000) / 10,
  status_breakdown: { finalized: Math.round(REPORTED_PUS * 0.72), validated: Math.round(REPORTED_PUS * 0.18), pending: Math.round(REPORTED_PUS * 0.06), disputed: Math.round(REPORTED_PUS * 0.04) },
  vote_totals: { valid: 24146132, rejected: 346789, cast: 24492921, accredited: 25187456 },
  party_scores: NIGERIAN_PARTIES,
  state_results: NIGERIA_STATES.map(s => ({ code: s.code, name: s.name, geo_zone: s.geo_zone, results_count: s.reported_pus, total_votes: s.total_votes })),
  zone_results: [
    { geo_zone: 'North Central', total_votes: 1658788, results_count: 19019 },
    { geo_zone: 'North East', total_votes: 1540686, results_count: 17286 },
    { geo_zone: 'North West', total_votes: 2961302, results_count: 32337 },
    { geo_zone: 'South East', total_votes: 1291197, results_count: 18758 },
    { geo_zone: 'South South', total_votes: 1741279, results_count: 24534 },
    { geo_zone: 'South West', total_votes: 2603083, results_count: 31643 },
  ],
  dual_ledger: { tigerbeetle_posted: REPORTED_PUS, hyperledger_confirmed: Math.round(REPORTED_PUS * 0.98), total_results: REPORTED_PUS, reconciliation_variance: 0.02 },
};

export const DEMO_LIVE_FEED = [
  { id: 1, polling_unit_code: 'LA/001/001/001', status: 'finalized', total_votes_cast: 342, tigerbeetle_status: 'posted', hyperledger_status: 'confirmed', submitted_at: new Date(Date.now() - 120000).toISOString(), pu_name: 'Ikeja Ward 1 PU 001', ward_name: 'Ikeja Ward 1', lga_name: 'Ikeja', state_name: 'Lagos', state_code: 'LA' },
  { id: 2, polling_unit_code: 'KN/003/002/015', status: 'finalized', total_votes_cast: 567, tigerbeetle_status: 'posted', hyperledger_status: 'confirmed', submitted_at: new Date(Date.now() - 180000).toISOString(), pu_name: 'Nassarawa Ward 2 PU 015', ward_name: 'Nassarawa Ward 2', lga_name: 'Nassarawa', state_name: 'Kano', state_code: 'KN' },
  { id: 3, polling_unit_code: 'FC/001/003/007', status: 'validated', total_votes_cast: 289, tigerbeetle_status: 'posted', hyperledger_status: 'pending', submitted_at: new Date(Date.now() - 240000).toISOString(), pu_name: 'AMAC Ward 3 PU 007', ward_name: 'AMAC Ward 3', lga_name: 'AMAC', state_name: 'FCT Abuja', state_code: 'FC' },
  { id: 4, polling_unit_code: 'RI/005/001/003', status: 'finalized', total_votes_cast: 412, tigerbeetle_status: 'posted', hyperledger_status: 'confirmed', submitted_at: new Date(Date.now() - 300000).toISOString(), pu_name: 'Obio-Akpor Ward 1 PU 003', ward_name: 'Obio-Akpor Ward 1', lga_name: 'Obio-Akpor', state_name: 'Rivers', state_code: 'RI' },
  { id: 5, polling_unit_code: 'OY/002/004/012', status: 'pending', total_votes_cast: 198, tigerbeetle_status: 'pending', hyperledger_status: 'pending', submitted_at: new Date(Date.now() - 360000).toISOString(), pu_name: 'Ibadan North Ward 4 PU 012', ward_name: 'Ibadan North Ward 4', lga_name: 'Ibadan North', state_name: 'Oyo', state_code: 'OY' },
];

export const DEMO_MAP_STATES = NIGERIA_STATES.map(s => ({
  code: s.code, name: s.name, geo_zone: s.geo_zone, capital: s.capital,
  total_pus: s.total_pus, reported_pus: s.reported_pus,
  total_votes: s.total_votes, total_cast: s.total_cast,
  avg_lat: s.lat, avg_lng: s.lng,
  party_scores: NIGERIAN_PARTIES.slice(0, 4).map((p, i) => ({
    party_code: p.code, abbreviation: p.abbreviation, color: p.color,
    total_votes: Math.round(s.total_votes * [0.36, 0.29, 0.25, 0.10][i]),
  })),
  leading_party: { abbreviation: 'APC', color: '#2563eb', total_votes: Math.round(s.total_votes * 0.36) },
}));

export const DEMO_TV_DATA = {
  election_id: 1,
  total_pus: TOTAL_PUS,
  reported_pus: REPORTED_PUS,
  completion_pct: Math.round((REPORTED_PUS / TOTAL_PUS) * 1000) / 10,
  total_votes: 24146132,
  party_totals: NIGERIAN_PARTIES.map(p => ({ party: p.abbreviation, votes: p.total_votes })),
  state_results: Object.fromEntries(
    NIGERIA_STATES.slice(0, 12).map(s => [
      s.name,
      NIGERIAN_PARTIES.slice(0, 6).map((p, i) => ({
        party: p.abbreviation,
        votes: Math.round(s.total_votes * [0.34, 0.28, 0.22, 0.09, 0.04, 0.03][i]),
      })),
    ])
  ),
  last_updated: new Date().toISOString(),
};

export const DEMO_COLLATION = NIGERIA_STATES.map(s => ({
  code: s.code, name: s.name, geo_zone: s.geo_zone,
  total_pus: s.total_pus, reported_pus: s.reported_pus,
  total_valid_votes: s.total_votes,
  rejected_votes: Math.round(s.total_votes * 0.014),
  total_votes_cast: s.total_cast,
  party_scores: NIGERIAN_PARTIES.slice(0, 4).map((p, i) => ({
    party_code: p.code, abbreviation: p.abbreviation, color: p.color,
    total_votes: Math.round(s.total_votes * [0.36, 0.29, 0.25, 0.10][i]),
  })),
  registered_voters: Math.round(s.total_pus * 500),
}));

export const DEMO_RESULTS = {
  results: [
    { id: 1, polling_unit_code: 'LA/001/001/001', status: 'finalized', total_valid_votes: 334, rejected_votes: 8, total_votes_cast: 342, accredited_voters: 378, tigerbeetle_status: 'posted', hyperledger_status: 'confirmed', submitted_at: new Date(Date.now() - 3600000).toISOString(), pu_name: 'Ikeja Ward 1 PU 001', ward_name: 'Ikeja Ward 1', lga_name: 'Ikeja', state_name: 'Lagos', party_scores: [{ party_code: 'APC', party_name: 'APC', color: '#2563eb', votes: 145 }, { party_code: 'PDP', party_name: 'PDP', color: '#dc2626', votes: 98 }, { party_code: 'LP', party_name: 'LP', color: '#16a34a', votes: 91 }] },
    { id: 2, polling_unit_code: 'KN/003/002/015', status: 'finalized', total_valid_votes: 556, rejected_votes: 11, total_votes_cast: 567, accredited_voters: 612, tigerbeetle_status: 'posted', hyperledger_status: 'confirmed', submitted_at: new Date(Date.now() - 7200000).toISOString(), pu_name: 'Nassarawa Ward 2 PU 015', ward_name: 'Nassarawa Ward 2', lga_name: 'Nassarawa', state_name: 'Kano', party_scores: [{ party_code: 'APC', party_name: 'APC', color: '#2563eb', votes: 312 }, { party_code: 'NNPP', party_name: 'NNPP', color: '#d97706', votes: 156 }, { party_code: 'PDP', party_name: 'PDP', color: '#dc2626', votes: 88 }] },
    { id: 3, polling_unit_code: 'AN/002/001/009', status: 'validated', total_valid_votes: 287, rejected_votes: 5, total_votes_cast: 292, accredited_voters: 315, tigerbeetle_status: 'posted', hyperledger_status: 'pending', submitted_at: new Date(Date.now() - 5400000).toISOString(), pu_name: 'Awka South Ward 1 PU 009', ward_name: 'Awka South Ward 1', lga_name: 'Awka South', state_name: 'Anambra', party_scores: [{ party_code: 'LP', party_name: 'LP', color: '#16a34a', votes: 156 }, { party_code: 'APGA', party_name: 'APGA', color: '#7c3aed', votes: 78 }, { party_code: 'PDP', party_name: 'PDP', color: '#dc2626', votes: 53 }] },
    { id: 4, polling_unit_code: 'FC/001/003/007', status: 'pending', total_valid_votes: 278, rejected_votes: 11, total_votes_cast: 289, accredited_voters: 320, tigerbeetle_status: 'pending', hyperledger_status: 'pending', submitted_at: new Date(Date.now() - 4800000).toISOString(), pu_name: 'AMAC Ward 3 PU 007', ward_name: 'AMAC Ward 3', lga_name: 'AMAC', state_name: 'FCT Abuja', party_scores: [{ party_code: 'APC', party_name: 'APC', color: '#2563eb', votes: 121 }, { party_code: 'LP', party_name: 'LP', color: '#16a34a', votes: 89 }, { party_code: 'PDP', party_name: 'PDP', color: '#dc2626', votes: 68 }] },
    { id: 5, polling_unit_code: 'RI/005/001/003', status: 'disputed', total_valid_votes: 398, rejected_votes: 14, total_votes_cast: 412, accredited_voters: 445, tigerbeetle_status: 'posted', hyperledger_status: 'confirmed', submitted_at: new Date(Date.now() - 9000000).toISOString(), pu_name: 'Obio-Akpor Ward 1 PU 003', ward_name: 'Obio-Akpor Ward 1', lga_name: 'Obio-Akpor', state_name: 'Rivers', party_scores: [{ party_code: 'PDP', party_name: 'PDP', color: '#dc2626', votes: 198 }, { party_code: 'APC', party_name: 'APC', color: '#2563eb', votes: 112 }, { party_code: 'LP', party_name: 'LP', color: '#16a34a', votes: 88 }] },
  ],
  total: 5,
};

export const DEMO_AUDIT = [
  { id: 1, action: 'result_submitted', entity_type: 'result', entity_id: 'LA/001/001/001', user_id: 1, username: 'po_lagos_001', ip_address: '41.58.12.34', details: 'Result submitted for Ikeja Ward 1 PU 001', created_at: new Date(Date.now() - 600000).toISOString() },
  { id: 2, action: 'result_validated', entity_type: 'result', entity_id: 'KN/003/002/015', user_id: 2, username: 'co_kano_001', ip_address: '41.190.23.45', details: 'Result validated by collation officer', created_at: new Date(Date.now() - 900000).toISOString() },
  { id: 3, action: 'bvas_sync', entity_type: 'bvas_device', entity_id: 'BVAS-FC-00312', user_id: 3, username: 'tech_fct_001', ip_address: '41.204.34.56', details: 'BVAS device synced 45 accreditations', created_at: new Date(Date.now() - 1200000).toISOString() },
  { id: 4, action: 'user_login', entity_type: 'auth', entity_id: 'admin', user_id: 1, username: 'admin', ip_address: '41.58.78.90', details: 'Admin login from Abuja', created_at: new Date(Date.now() - 1800000).toISOString() },
  { id: 5, action: 'incident_reported', entity_type: 'incident', entity_id: 'INC-2027-0042', user_id: 4, username: 'observer_anambra', ip_address: '41.138.45.67', details: 'Card reader malfunction reported at PU AN/002/003/012', created_at: new Date(Date.now() - 2400000).toISOString() },
  { id: 6, action: 'blockchain_commit', entity_type: 'hyperledger', entity_id: 'block-8847', user_id: 0, username: 'system', ip_address: '10.0.0.1', details: 'Block 8847 committed with 128 transactions', created_at: new Date(Date.now() - 3000000).toISOString() },
  { id: 7, action: 'dispute_filed', entity_type: 'dispute', entity_id: 'DSP-2027-0018', user_id: 5, username: 'party_agent_pdp', ip_address: '41.73.56.78', details: 'Dispute filed for RI/005/001/003 — vote count discrepancy', created_at: new Date(Date.now() - 3600000).toISOString() },
  { id: 8, action: 'collation_finalized', entity_type: 'collation', entity_id: 'LA', user_id: 6, username: 'rec_lagos', ip_address: '41.58.90.12', details: 'Lagos state collation finalized', created_at: new Date(Date.now() - 4200000).toISOString() },
];

export const DEMO_INCIDENTS = [
  { id: 1, type: 'equipment_failure', severity: 'high', state_code: 'AN', state_name: 'Anambra', lga_name: 'Awka South', polling_unit_code: 'AN/002/003/012', description: 'BVAS card reader malfunction — unable to verify fingerprints', status: 'investigating', reported_by: 'observer_anambra', reported_at: new Date(Date.now() - 3600000).toISOString(), resolved_at: null },
  { id: 2, type: 'violence', severity: 'critical', state_code: 'RI', state_name: 'Rivers', lga_name: 'Obio-Akpor', polling_unit_code: 'RI/005/001/003', description: 'Disruption of voting process by armed thugs — ballot box snatched', status: 'escalated', reported_by: 'po_rivers_005', reported_at: new Date(Date.now() - 7200000).toISOString(), resolved_at: null },
  { id: 3, type: 'irregularity', severity: 'medium', state_code: 'KN', state_name: 'Kano', lga_name: 'Nassarawa', polling_unit_code: 'KN/003/002/015', description: 'Voter intimidation reported near polling unit entrance', status: 'resolved', reported_by: 'observer_kano_002', reported_at: new Date(Date.now() - 10800000).toISOString(), resolved_at: new Date(Date.now() - 7200000).toISOString() },
  { id: 4, type: 'logistics', severity: 'low', state_code: 'BA', state_name: 'Bauchi', lga_name: 'Bauchi', polling_unit_code: 'BA/001/005/008', description: 'Late arrival of election materials — voting started 2 hours late', status: 'resolved', reported_by: 'po_bauchi_001', reported_at: new Date(Date.now() - 14400000).toISOString(), resolved_at: new Date(Date.now() - 10800000).toISOString() },
  { id: 5, type: 'network', severity: 'medium', state_code: 'BO', state_name: 'Borno', lga_name: 'Maiduguri', polling_unit_code: 'BO/001/002/004', description: 'Network connectivity lost — results queued for offline sync', status: 'monitoring', reported_by: 'tech_borno_001', reported_at: new Date(Date.now() - 5400000).toISOString(), resolved_at: null },
];

export const DEMO_MIDDLEWARE = {
  services: [
    { name: 'Kafka', status: 'healthy', latency_ms: 12, uptime_pct: 99.98, topics: 8, messages_per_sec: 2340 },
    { name: 'Redis', status: 'healthy', latency_ms: 1, uptime_pct: 99.99, keys: 45230, memory_mb: 512 },
    { name: 'TigerBeetle', status: 'healthy', latency_ms: 3, uptime_pct: 99.99, transfers_per_sec: 890, accounts: 176543 },
    { name: 'PostgreSQL', status: 'healthy', latency_ms: 5, uptime_pct: 99.97, active_connections: 48, pool_size: 100 },
    { name: 'OpenSearch', status: 'healthy', latency_ms: 18, uptime_pct: 99.95, indices: 12, docs: 8934521 },
    { name: 'Keycloak', status: 'healthy', latency_ms: 45, uptime_pct: 99.94, active_sessions: 1245, realms: 2 },
    { name: 'Temporal', status: 'healthy', latency_ms: 8, uptime_pct: 99.96, running_workflows: 34, completed_today: 8923 },
    { name: 'Dapr', status: 'healthy', latency_ms: 6, uptime_pct: 99.97, sidecars: 14, pubsub_msgs: 12345 },
    { name: 'Fluvio', status: 'healthy', latency_ms: 4, uptime_pct: 99.98, topics: 5, throughput_mbps: 45 },
    { name: 'APISIX', status: 'healthy', latency_ms: 2, uptime_pct: 99.99, routes: 51, rate_limit_hits: 234 },
    { name: 'Permify', status: 'healthy', latency_ms: 7, uptime_pct: 99.96, policies: 28, checks_per_sec: 560 },
    { name: 'Mojaloop', status: 'healthy', latency_ms: 35, uptime_pct: 99.93, transactions: 4567, settlement_rate: 99.2 },
    { name: 'OpenAppSec', status: 'healthy', latency_ms: 3, uptime_pct: 99.98, threats_blocked: 187, rules_active: 342 },
    { name: 'Hyperledger Fabric', status: 'healthy', latency_ms: 120, uptime_pct: 99.91, blocks: 8847, peers: 4 },
  ],
  overall_health: 'healthy',
  total_services: 14,
  healthy_count: 14,
};

export const DEMO_BVAS = {
  devices: [
    { id: 'BVAS-LA-00001', state_code: 'LA', lga_name: 'Ikeja', polling_unit_code: 'LA/001/001/001', status: 'active', firmware: 'v3.2.1', battery_pct: 87, last_sync: new Date(Date.now() - 300000).toISOString(), accreditations: 342, fingerprint_matches: 338, facial_matches: 340 },
    { id: 'BVAS-KN-00234', state_code: 'KN', lga_name: 'Nassarawa', polling_unit_code: 'KN/003/002/015', status: 'active', firmware: 'v3.2.1', battery_pct: 62, last_sync: new Date(Date.now() - 600000).toISOString(), accreditations: 567, fingerprint_matches: 561, facial_matches: 563 },
    { id: 'BVAS-FC-00312', state_code: 'FC', lga_name: 'AMAC', polling_unit_code: 'FC/001/003/007', status: 'syncing', firmware: 'v3.2.1', battery_pct: 45, last_sync: new Date(Date.now() - 1200000).toISOString(), accreditations: 289, fingerprint_matches: 285, facial_matches: 287 },
    { id: 'BVAS-BO-00089', state_code: 'BO', lga_name: 'Maiduguri', polling_unit_code: 'BO/001/002/004', status: 'offline', firmware: 'v3.1.9', battery_pct: 23, last_sync: new Date(Date.now() - 7200000).toISOString(), accreditations: 156, fingerprint_matches: 152, facial_matches: 154 },
    { id: 'BVAS-AN-00167', state_code: 'AN', lga_name: 'Awka South', polling_unit_code: 'AN/002/003/012', status: 'error', firmware: 'v3.2.0', battery_pct: 71, last_sync: new Date(Date.now() - 3600000).toISOString(), accreditations: 0, fingerprint_matches: 0, facial_matches: 0 },
  ],
  total: 5,
  summary: { total_devices: 176543, active: 168234, syncing: 4523, offline: 2890, error: 896 },
};

export const DEMO_COMMAND_CENTER = {
  states: NIGERIA_STATES.map(s => ({
    state_code: s.code,
    state_name: s.name,
    total_pus: s.total_pus,
    reported_pus: s.reported_pus,
    completion_pct: Math.round((s.reported_pus / s.total_pus) * 1000) / 10,
    stalled_pus: Math.round((s.total_pus - s.reported_pus) * 0.3),
    eta_complete: new Date(Date.now() + Math.random() * 7200000).toISOString(),
    status: s.reported_pus / s.total_pus > 0.95 ? 'on_track' : s.reported_pus / s.total_pus > 0.85 ? 'slow' : 'critical',
  })),
  alerts: [
    { id: 1, level: 'critical', state_code: 'RI', message: 'Rivers: 3 PUs report ballot box snatching incidents' },
    { id: 2, level: 'warning', state_code: 'BO', message: 'Borno: Network outage affecting 45 PUs in Maiduguri LGA' },
    { id: 3, level: 'info', state_code: 'LA', message: 'Lagos: Voting extended by 1 hour due to late start at 23 PUs' },
    { id: 4, level: 'warning', state_code: 'KN', message: 'Kano: BVAS synchronization delays in Nassarawa LGA' },
  ],
  overall_pus: TOTAL_PUS,
  reported_pus: REPORTED_PUS,
  stalled_pus: Math.round((TOTAL_PUS - REPORTED_PUS) * 0.3),
  completion_pct: Math.round((REPORTED_PUS / TOTAL_PUS) * 1000) / 10,
  load_shedding: 0,
  timestamp: new Date().toISOString(),
};

export const DEMO_PRODUCTION = {
  components: {
    'go-backend': { status: 'active', version: 'v2.4.1', uptime: '14d 6h 23m', cpu_pct: 34, memory_mb: 1024 },
    'rust-engine': { status: 'active', version: 'v1.2.0', uptime: '14d 6h 23m', cpu_pct: 12, memory_mb: 256 },
    'python-analytics': { status: 'active', version: 'v1.0.3', uptime: '14d 5h 45m', cpu_pct: 45, memory_mb: 2048 },
    'frontend': { status: 'active', version: 'v3.1.0', uptime: '14d 6h 23m', cpu_pct: 5, memory_mb: 128 },
    'postgres-primary': { status: 'active', version: '16.2', uptime: '30d 12h', cpu_pct: 28, memory_mb: 4096 },
    'redis-cluster': { status: 'active', version: '7.2', uptime: '30d 12h', cpu_pct: 8, memory_mb: 512 },
    'kafka-broker': { status: 'active', version: '3.7', uptime: '30d 12h', cpu_pct: 22, memory_mb: 2048 },
    'keycloak': { status: 'active', version: '24.0', uptime: '14d 6h', cpu_pct: 15, memory_mb: 1024 },
  },
};

export const DEMO_WEBHOOKS = [
  { id: 1, url: 'https://api.party-apc.ng/election/results', events: ['result.submitted', 'result.finalized'], status: 'active', last_delivery: new Date(Date.now() - 300000).toISOString(), success_rate: 98.5, created_at: new Date(Date.now() - 86400000 * 7).toISOString() },
  { id: 2, url: 'https://api.party-pdp.ng/election/results', events: ['result.submitted', 'result.finalized'], status: 'active', last_delivery: new Date(Date.now() - 600000).toISOString(), success_rate: 97.2, created_at: new Date(Date.now() - 86400000 * 7).toISOString() },
  { id: 3, url: 'https://irev.inecnigeria.org/webhook', events: ['result.finalized', 'collation.completed'], status: 'active', last_delivery: new Date(Date.now() - 120000).toISOString(), success_rate: 99.8, created_at: new Date(Date.now() - 86400000 * 14).toISOString() },
  { id: 4, url: 'https://observers.eu-eom.ng/feed', events: ['incident.reported', 'incident.resolved'], status: 'active', last_delivery: new Date(Date.now() - 1800000).toISOString(), success_rate: 96.1, created_at: new Date(Date.now() - 86400000 * 3).toISOString() },
  { id: 5, url: 'https://media.channels-tv.com/election-api', events: ['result.submitted'], status: 'paused', last_delivery: new Date(Date.now() - 86400000).toISOString(), success_rate: 94.3, created_at: new Date(Date.now() - 86400000 * 10).toISOString() },
];

export const DEMO_USERS = [
  { id: 1, username: 'admin', full_name: 'Chief Electoral Commissioner', role: 'admin', staff_id: 'INEC-HQ-001', state_code: 'FC', kyc_status: 'verified', created_at: new Date(Date.now() - 86400000 * 30).toISOString() },
  { id: 2, username: 'co_lagos_001', full_name: 'Adebayo Ogundimu', role: 'collation_officer', staff_id: 'INEC-LA-CO-001', state_code: 'LA', kyc_status: 'verified', created_at: new Date(Date.now() - 86400000 * 14).toISOString() },
  { id: 3, username: 'po_kano_005', full_name: 'Aminu Ibrahim', role: 'presiding_officer', staff_id: 'INEC-KN-PO-005', state_code: 'KN', kyc_status: 'verified', created_at: new Date(Date.now() - 86400000 * 10).toISOString() },
  { id: 4, username: 'observer_eu_001', full_name: 'Maria Schmidt', role: 'observer', staff_id: 'EU-EOM-042', state_code: 'FC', kyc_status: 'verified', created_at: new Date(Date.now() - 86400000 * 7).toISOString() },
  { id: 5, username: 'po_rivers_012', full_name: 'Chioma Nwosu', role: 'presiding_officer', staff_id: 'INEC-RI-PO-012', state_code: 'RI', kyc_status: 'pending', created_at: new Date(Date.now() - 86400000 * 3).toISOString() },
  { id: 6, username: 'tech_borno_001', full_name: 'Musa Shettima', role: 'presiding_officer', staff_id: 'INEC-BO-TECH-001', state_code: 'BO', kyc_status: 'verified', created_at: new Date(Date.now() - 86400000 * 5).toISOString() },
];

export const DEMO_PREDICTIVE = {
  predictions: [
    { election_id: 1, election_type: 'presidential', turnout_prediction: 0.672, margin_of_victory: 0.084, confidence: 0.89, risk_factors: ['Low turnout in South East', 'Network issues in North East', 'Security concerns in Rivers'], model_version: 'v2.1.0-ensemble' },
    { election_id: 2, election_type: 'gubernatorial', turnout_prediction: 0.584, margin_of_victory: 0.127, confidence: 0.82, risk_factors: ['Incumbency advantage bias', 'Limited polling data in rural areas'], model_version: 'v2.1.0-ensemble' },
    { election_id: 3, election_type: 'senatorial', turnout_prediction: 0.498, margin_of_victory: 0.065, confidence: 0.75, risk_factors: ['Multiple strong candidates', 'Historical voter apathy', 'Campaign finance irregularities'], model_version: 'v2.1.0-ensemble' },
  ],
  model: 'Gradient Boosted Ensemble (XGBoost + LightGBM + Neural Net)',
};

export const DEMO_COMPLIANCE = {
  frameworks: [
    {
      name: 'ECOWAS',
      score: 94,
      requirements: [
        { id: 'EC-1', name: 'Biometric voter verification', status: 'compliant', evidence: 'BVAS deployed to all 176,543 PUs' },
        { id: 'EC-2', name: 'Real-time result transmission', status: 'compliant', evidence: 'IReV integration active with 99.8% uptime' },
        { id: 'EC-3', name: 'Observer access', status: 'compliant', evidence: '2,345 accredited observers with digital portal access' },
        { id: 'EC-4', name: 'Dispute resolution mechanism', status: 'compliant', evidence: 'Digital dispute filing with 48-hour SLA' },
        { id: 'EC-5', name: 'Transparent collation', status: 'partial', evidence: 'Multi-tier collation with blockchain audit trail — ward-level public access pending' },
      ],
    },
    {
      name: 'AU',
      score: 91,
      requirements: [
        { id: 'AU-1', name: 'Universal suffrage verification', status: 'compliant', evidence: 'PVC + biometric dual verification' },
        { id: 'AU-2', name: 'Free and fair access', status: 'compliant', evidence: 'Multi-party agent presence at all PUs' },
        { id: 'AU-3', name: 'Electoral security', status: 'partial', evidence: '98% PU coverage by security forces — gaps in North East conflict zones' },
        { id: 'AU-4', name: 'Post-election audit capability', status: 'compliant', evidence: 'TigerBeetle + Hyperledger dual-ledger with Merkle tree verification' },
      ],
    },
    {
      name: 'EU',
      score: 88,
      requirements: [
        { id: 'EU-1', name: 'Data protection compliance', status: 'compliant', evidence: 'AES-256 encryption at rest, TLS 1.3 in transit, NDPR compliant' },
        { id: 'EU-2', name: 'E2E verifiability', status: 'partial', evidence: 'Blockchain verification available — full E2E verifiable voting in pilot for primaries' },
        { id: 'EU-3', name: 'Accessibility', status: 'partial', evidence: 'Web WCAG 2.1 AA — mobile accessibility audit pending' },
        { id: 'EU-4', name: 'Transparency of technology', status: 'compliant', evidence: 'Open-source platform, public API, published technical documentation' },
      ],
    },
  ],
  overall_score: 91,
  generated_at: new Date().toISOString(),
};

export const DEMO_PARTIES = NIGERIAN_PARTIES.map(p => ({ code: p.code, name: p.name, abbreviation: p.abbreviation, color: p.color }));

export const DEMO_STATES = NIGERIA_STATES.map(s => ({ code: s.code, name: s.name }));
