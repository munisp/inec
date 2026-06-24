/**
 * Comprehensive i18n translations for all 46 INEC Election Platform pages.
 * Languages: English (en), Hausa (ha), Yoruba (yo), Igbo (ig), Pidgin (pcm)
 */

export type Lang = 'en' | 'ha' | 'yo' | 'ig' | 'pcm';

type Dict = Record<string, string>;

// ── Common / Shared keys ────────────────────────────────────────
const COMMON_EN: Dict = {
  // Navigation & Layout
  dashboard: 'Dashboard', results: 'Results', map: 'Map', settings: 'Settings',
  logout: 'Logout', login: 'Login', profile: 'Profile', notifications: 'Notifications',
  home: 'Home', back: 'Back', next: 'Next', previous: 'Previous', save: 'Save',
  cancel: 'Cancel', delete: 'Delete', edit: 'Edit', create: 'Create', update: 'Update',
  submit: 'Submit', confirm: 'Confirm', close: 'Close', open: 'Open', search: 'Search',
  filter: 'Filter', sort: 'Sort', refresh: 'Refresh', loading: 'Loading...',
  error: 'Error', success: 'Success', warning: 'Warning', info: 'Information',
  yes: 'Yes', no: 'No', ok: 'OK', retry: 'Retry', export: 'Export', import: 'Import',
  download: 'Download', upload: 'Upload', view: 'View', details: 'Details',
  actions: 'Actions', status: 'Status', date: 'Date', time: 'Time', name: 'Name',
  description: 'Description', type: 'Type', id: 'ID', total: 'Total', count: 'Count',
  percentage: 'Percentage', active: 'Active', inactive: 'Inactive', pending: 'Pending',
  completed: 'Completed', failed: 'Failed', processing: 'Processing',
  no_data: 'No data available', all: 'All', none: 'None',
  // Election-specific
  election: 'Election', polling_unit: 'Polling Unit', ward: 'Ward', lga: 'LGA',
  state: 'State', zone: 'Geo-Political Zone', party: 'Party', candidate: 'Candidate',
  voter: 'Voter', votes: 'Votes', result: 'Result', turnout: 'Turnout',
  accreditation: 'Accreditation', collation: 'Collation', declaration: 'Declaration',
  // Roles
  admin: 'Admin', officer: 'Officer', observer: 'Observer', presiding_officer: 'Presiding Officer',
  returning_officer: 'Returning Officer', collation_officer: 'Collation Officer',
};

const COMMON_HA: Dict = {
  dashboard: 'Babban Shafi', results: 'Sakamako', map: 'Taswirar', settings: 'Saitunan',
  logout: 'Fita', login: 'Shiga', profile: 'Bayanan Kai', notifications: 'Sanarwa',
  home: 'Gida', back: 'Baya', next: 'Gaba', previous: 'Na baya', save: 'Ajiye',
  cancel: 'Soke', delete: 'Share', edit: 'Gyara', create: 'Ƙirƙira', update: 'Sabunta',
  submit: 'Tura', confirm: 'Tabbatar', close: 'Rufe', open: 'Buɗe', search: 'Bincike',
  filter: 'Tace', sort: 'Tsara', refresh: 'Sabunta', loading: 'Ana lodi...',
  error: 'Kuskure', success: 'Nasara', warning: 'Gargaɗi', info: 'Bayanai',
  yes: 'Eh', no: "A'a", ok: 'To', retry: 'Sake gwadawa', export: 'Fitar', import: 'Shigo',
  download: 'Sauke', upload: 'Ɗora', view: 'Duba', details: 'Cikakkun Bayanai',
  actions: 'Ayyuka', status: 'Matsayi', date: 'Kwanan wata', time: 'Lokaci', name: 'Suna',
  description: 'Bayani', type: 'Iri', id: 'Lamba', total: 'Jimla', count: 'Adadi',
  percentage: 'Kashi', active: 'Yana aiki', inactive: 'Ba ya aiki', pending: 'Jira',
  completed: 'An kammala', failed: 'Ya gaza', processing: 'Ana sarrafa',
  no_data: 'Babu bayanai', all: 'Duka', none: 'Babu',
  election: 'Zaɓe', polling_unit: 'Rumfar zaɓe', ward: 'Unguwa', lga: 'Ƙaramar hukuma',
  state: 'Jiha', zone: 'Yankin siyasa', party: "Jam'iyya", candidate: 'Ɗan takara',
  voter: 'Mai jefa ƙuri\'a', votes: "Ƙuri'u", result: 'Sakamako', turnout: 'Halartar zaɓe',
  accreditation: 'Tantancewa', collation: 'Haɗa sakamako', declaration: 'Sanarwar sakamako',
  admin: 'Mai gudanarwa', officer: 'Jami\'i', observer: 'Mai sa ido',
  presiding_officer: 'Shugaban rumfa', returning_officer: 'Jami\'in sakamako',
  collation_officer: 'Jami\'in haɗa sakamako',
};

const COMMON_YO: Dict = {
  dashboard: 'Ojú Ìwé', results: 'Awọn Abajade', map: 'Àwòrán ilẹ̀', settings: 'Ètò',
  logout: 'Jáde', login: 'Wọlé', profile: 'Àkọlé mi', notifications: 'Ìfitónilétí',
  home: 'Ilé', back: 'Padà', next: 'Tókàn', previous: 'Ti tẹ́lẹ̀', save: 'Fi pamọ́',
  cancel: 'Fagilee', delete: 'Pa rẹ́', edit: 'Ṣàtúnṣe', create: 'Ṣẹ̀dá', update: 'Ṣe ìmúdájú',
  submit: 'Fi sílẹ̀', confirm: 'Jẹ́rìísí', close: 'Pa', open: 'Ṣí', search: 'Wá',
  filter: 'Ṣe àyọkà', sort: 'Tòṣe', refresh: 'Tun ṣe', loading: 'Ó ń yọ...',
  error: 'Àṣìṣe', success: 'Àṣeyọrí', warning: 'Ìkìlọ̀', info: 'Ìfitónilétí',
  yes: 'Bẹ́ẹ̀ni', no: 'Rárá', ok: 'O dáa', retry: 'Tun gbìyànjú', export: 'Gbé jáde',
  import: 'Gbé wọlé', download: 'Gbà sílẹ̀', upload: 'Gbé sókè', view: 'Wo',
  details: 'Àlàyé', actions: 'Ìṣe', status: 'Ipò', date: 'Ọjọ́', time: 'Àkókò', name: 'Orúkọ',
  description: 'Àpèjúwe', type: 'Irú', id: 'Nọ́mbà', total: 'Àpapọ̀', count: 'Iye',
  percentage: 'Ìpín nínú ọgọ́rùn', active: 'Ṣíṣẹ́', inactive: 'Kò ṣíṣẹ́',
  pending: 'Ndúró', completed: 'Parí', failed: 'Kùnà', processing: 'Ṣíṣe',
  no_data: 'Kò sí data', all: 'Gbogbo', none: 'Kò sí',
  election: 'Ìdìbò', polling_unit: 'Ibùdó ìdìbò', ward: 'Ẹ̀ka', lga: 'Ìjọba ìbílẹ̀',
  state: 'Ìpínlẹ̀', zone: 'Agbègbè oṣèlú', party: 'Ẹgbẹ́ oṣèlú',
  candidate: 'Olùdíje', voter: 'Olùdìbò', votes: 'Ìbò', result: 'Àbájáde',
  turnout: 'Iye olùwá', accreditation: 'Ìfọwọ́sí', collation: 'Ìkójọpọ̀ àbájáde',
  declaration: 'Ìkéde àbájáde',
  admin: 'Alákòóso', officer: 'Oṣiṣẹ́', observer: 'Olùṣàkíyèsí',
  presiding_officer: 'Olórí ibùdó', returning_officer: 'Oṣiṣẹ́ àbájáde',
  collation_officer: 'Oṣiṣẹ́ ìkójọpọ̀',
};

const COMMON_IG: Dict = {
  dashboard: 'Pánélụ', results: 'Nsonaazụ', map: 'Maapụ', settings: 'Ntọala',
  logout: 'Pụọ', login: 'Banye', profile: 'Profaịlụ', notifications: 'Ọkwa',
  home: 'Ụlọ', back: 'Laghachi', next: 'Ọzọ', previous: 'Nke gara aga', save: 'Chekwaa',
  cancel: 'Kagbuo', delete: 'Hichapụ', edit: 'Dezie', create: 'Mepụta', update: 'Melite',
  submit: 'Nyefee', confirm: 'Kwenye', close: 'Mechie', open: 'Mepee', search: 'Chọọ',
  filter: 'Nyocha', sort: 'Hazie', refresh: 'Mee ọhụrụ', loading: 'Na-ebugo...',
  error: 'Njehie', success: 'Ọganihu', warning: 'Ịdọ aka ná ntị', info: 'Ozi',
  yes: 'Ee', no: 'Mba', ok: 'Ọ dị mma', retry: 'Nwaa ọzọ', export: 'Bupụ', import: 'Bubata',
  download: 'Budata', upload: 'Bulite', view: 'Lee', details: 'Nkọwa',
  actions: 'Omume', status: 'Ọnọdụ', date: 'Ụbọchị', time: 'Oge', name: 'Aha',
  description: 'Nkọwa', type: 'Ụdị', id: 'Nọmbà', total: 'Niile', count: 'Ọnụ ọgụgụ',
  percentage: 'Pasent', active: 'Na-arụ ọrụ', inactive: 'Anaghị arụ ọrụ',
  pending: 'Na-eche', completed: 'Emechara', failed: 'Adaghị', processing: 'Na-arụ',
  no_data: 'Enweghị data', all: 'Niile', none: 'Ọ dịghị',
  election: 'Ntuli aka', polling_unit: 'Ebe ntuli aka', ward: 'Wọọdụ', lga: 'Ọchịchị obodo',
  state: 'Steeti', zone: 'Mpaghara ndọrọ ndọrọ ọchịchị', party: 'Otu ndọrọ ndọrọ ọchịchị',
  candidate: 'Onye na-asọ mpi', voter: 'Onye ntuli aka', votes: 'Votu',
  result: 'Nsonaazụ', turnout: 'Ọnụ ọgụgụ', accreditation: 'Nkwenye',
  collation: 'Nchịkọta', declaration: 'Nkwupụta',
  admin: 'Onye nlekọta', officer: 'Onye ọrụ', observer: 'Onye nleba anya',
  presiding_officer: 'Onye isi', returning_officer: 'Onye nsonaazụ',
  collation_officer: 'Onye nchịkọta',
};

const COMMON_PCM: Dict = {
  dashboard: 'Dashboard', results: 'Results', map: 'Map', settings: 'Settings',
  logout: 'Comot', login: 'Enter', profile: 'My Profile', notifications: 'Notifications',
  home: 'Home', back: 'Go Back', next: 'Next One', previous: 'Before One', save: 'Keep Am',
  cancel: 'Cancel Am', delete: 'Remove Am', edit: 'Change Am', create: 'Make New',
  update: 'Update Am', submit: 'Send Am', confirm: 'Confirm Am', close: 'Close Am',
  open: 'Open Am', search: 'Find', filter: 'Filter Am', sort: 'Arrange Am',
  refresh: 'Load Again', loading: 'E dey load...',
  error: 'Problem', success: 'E Don Work', warning: 'Warning', info: 'Info',
  yes: 'Yes', no: 'No', ok: 'OK', retry: 'Try Again', export: 'Carry Out', import: 'Bring In',
  download: 'Download Am', upload: 'Upload Am', view: 'See Am', details: 'Full Details',
  actions: 'Actions', status: 'Status', date: 'Date', time: 'Time', name: 'Name',
  description: 'Details', type: 'Type', id: 'Number', total: 'Total', count: 'How Many',
  percentage: 'Percent', active: 'Active', inactive: 'No Active', pending: 'Waiting',
  completed: 'Done', failed: 'E Fail', processing: 'E Dey Process',
  no_data: 'No data dey', all: 'All', none: 'Nothing',
  election: 'Election', polling_unit: 'Polling Unit', ward: 'Ward', lga: 'Local Govment',
  state: 'State', zone: 'Zone', party: 'Party', candidate: 'Candidate',
  voter: 'Person wey dey vote', votes: 'Votes', result: 'Result', turnout: 'How Many Come',
  accreditation: 'Accreditation', collation: 'Counting', declaration: 'Final Result',
  admin: 'Admin', officer: 'Officer', observer: 'Observer',
  presiding_officer: 'Head Officer', returning_officer: 'Result Officer',
  collation_officer: 'Counting Officer',
};

// ── Page-specific keys ──────────────────────────────────────────
const PAGES_EN: Dict = {
  // DashboardPage
  results_received: 'Results Received', total_votes_cast: 'Total Votes Cast',
  completion: 'Completion', finalized: 'Finalized', rejected: 'Rejected',
  election_date: 'Election Date', real_time_stats: 'Real-Time Statistics',
  party_standings: 'Party Standings', status_breakdown: 'Status Breakdown',
  recent_submissions: 'Recent Submissions',

  // ResultsPage
  live_results: 'Live Election Results', vote_count: 'Vote Count',
  percentage_share: 'Percentage Share', leading_party: 'Leading Party',
  margin: 'Margin', results_by_state: 'Results by State',
  results_by_zone: 'Results by Zone',

  // MapPage
  street: 'Street', satellite: 'Satellite', compare: 'Compare',
  pu_markers: 'PU Markers', box_select: 'Box Select',
  export_csv: 'Export CSV', export_geojson: 'Export GeoJSON',
  selection: 'Selection', search_places: 'Search places...',

  // GeoLibreMapPage
  geolibre_gis: 'GeoLibre GIS', spatial_analysis: 'Spatial Analysis',
  live_tracking: 'Live Tracking', heatmap: 'Heatmap',
  buffer_zone: 'Buffer Zone', voronoi: 'Voronoi Diagram',
  isochrone: 'Isochrone Map', cluster_analysis: 'Cluster Analysis',

  // BVASPage
  bvas_management: 'BVAS Device Management', device_id: 'Device ID',
  battery_level: 'Battery Level', last_sync: 'Last Sync',
  firmware_version: 'Firmware Version', assigned_pu: 'Assigned PU',
  biometric_captures: 'Biometric Captures', device_status: 'Device Status',

  // BVASSyncPage
  bvas_sync: 'BVAS Sync Status', sync_progress: 'Sync Progress',
  pending_sync: 'Pending Sync', synced: 'Synced', sync_failed: 'Sync Failed',
  last_sync_time: 'Last Sync Time',

  // BiometricPage
  biometric_verification: 'Biometric Verification',
  fingerprint: 'Fingerprint', facial: 'Facial Recognition', iris: 'Iris Scan',
  match_score: 'Match Score', verification_status: 'Verification Status',
  enroll: 'Enroll', verify: 'Verify', template: 'Template',

  // BlockchainPage
  blockchain_ledger: 'Blockchain Ledger', block_height: 'Block Height',
  transactions: 'Transactions', chain_integrity: 'Chain Integrity',
  last_block: 'Last Block', hash: 'Hash', merkle_root: 'Merkle Root',
  consensus: 'Consensus', peer_count: 'Peer Count',

  // CollationPage
  collation_center: 'Collation Center', ward_collation: 'Ward Collation',
  lga_collation: 'LGA Collation', state_collation: 'State Collation',
  collation_status: 'Collation Status', approve: 'Approve', reject: 'Reject',

  // VoterRegistrationPage
  voter_registration: 'Voter Registration', vin: 'VIN',
  pvc_number: 'PVC Number', registration_center: 'Registration Center',
  biometric_enrolled: 'Biometric Enrolled', pvc_collected: 'PVC Collected',

  // AnomalyDetectionPage
  anomaly_detection: 'AI Anomaly Detection',
  anomaly_desc: 'AI-powered result validation detecting statistical anomalies',
  integrity_score: 'Integrity Score', total_anomalies: 'Total Anomalies',
  ai_methods: 'AI Methods', benford_status: 'Benford Test',
  overview: 'Overview', benford_analysis: "Benford's Analysis", anomaly_list: 'Anomaly List',
  severity_distribution: 'Severity Distribution', integrity_breakdown: 'Integrity Breakdown',
  benford_first_digit: "Benford's First Digit Distribution", digit: 'Digit',
  observed: 'Observed %', expected_benford: 'Expected (Benford)', sample_size: 'Sample Size',
  filter_severity: 'Filter by Severity', severity: 'Severity',
  no_anomalies: 'No anomalies detected',

  // SMSVerificationPage
  sms_verification: 'SMS/USSD Verification',
  sms_desc: 'Verify election results via SMS or USSD — no internet required',
  sms_channel: 'SMS Channel', ussd_channel: 'USSD Channel',
  text_verify: 'Text to Verify', no_internet: 'No Internet Required',
  works_offline: 'Works Offline', sms_verify: 'SMS Verify',
  ussd_simulator: 'USSD Simulator', statistics: 'Statistics', user_guide: 'User Guide',
  phone_number: 'Phone Number', phone_hint: 'Nigerian phone number with country code',
  polling_unit_code: 'Polling Unit Code', pu_code_hint: 'Enter the polling unit code to verify',
  sending: 'Sending...', verify_result: 'Verify Result', result_found: 'Result Found',

  // PublicAPIPage
  public_api: 'Public API',
  public_api_desc: 'Versioned API with key auth, rate limiting, and OpenAPI docs',
  api_version: 'API Version', rate_limit: 'Rate Limit', req_per_min: 'req/min',
  active_keys: 'Active Keys', api_docs: 'API Docs', api_keys: 'API Keys',
  usage: 'Usage', examples: 'Examples', api_endpoints: 'API Endpoints',
  auth_required: 'Auth', copied: 'Copied!', authentication: 'Authentication',
  generate_key: 'Generate API Key', key_name: 'Key Name', owner: 'Owner',
  key_generated: 'API Key Generated Successfully', existing_keys: 'Existing API Keys',
  permissions: 'Permissions',

  // KYCVerificationPage
  kyc_verification: 'KYC Verification', identity_verification: 'Identity Verification',
  nin_lookup: 'NIN Lookup', bvn_verification: 'BVN Verification',
  document_check: 'Document Check', liveness_check: 'Liveness Check',
  kyc_score: 'KYC Score', verified: 'Verified', unverified: 'Unverified',

  // ComplianceReportPage
  compliance_report: 'Compliance Report', compliance_score: 'Compliance Score',
  sanctions_check: 'Sanctions Check', pep_screening: 'PEP Screening',
  risk_level: 'Risk Level', high_risk: 'High Risk', low_risk: 'Low Risk',

  // AIMonitoringPage
  ai_monitoring: 'AI Monitoring', model_accuracy: 'Model Accuracy',
  predictions: 'Predictions', confidence_level: 'Confidence Level',
  real_time_inference: 'Real-Time Inference',

  // AdminConsolePage
  admin_console: 'Admin Console', system_health: 'System Health',
  service_status: 'Service Status', api_latency: 'API Latency',
  error_rate: 'Error Rate', uptime: 'Uptime',

  // AuditPage
  audit_trail: 'Audit Trail', event: 'Event', actor: 'Actor',
  ip_address: 'IP Address', timestamp: 'Timestamp', resource: 'Resource',

  // CitizenPortalPage
  citizen_portal: 'Citizen Portal', check_results: 'Check Results',
  verify_registration: 'Verify Registration', report_incident: 'Report Incident',

  // CommandCenterPage
  command_center: 'Command Center', live_feed: 'Live Feed',
  active_incidents: 'Active Incidents', deployed_teams: 'Deployed Teams',
  situation_report: 'Situation Report',

  // DataValidationPage
  data_validation: 'Data Validation', validation_rules: 'Validation Rules',
  passed: 'Passed', warnings: 'Warnings', errors: 'Errors',

  // DisputeResolutionPage
  dispute_resolution: 'Dispute Resolution', case_number: 'Case Number',
  filed_by: 'Filed By', resolution_status: 'Resolution Status',
  evidence: 'Evidence', tribunal: 'Tribunal',

  // DocumentAIPage
  document_ai: 'Document AI', ocr_extraction: 'OCR Extraction',
  document_type: 'Document Type', extracted_fields: 'Extracted Fields',
  confidence: 'Confidence',

  // DuplicateDetectionPage
  duplicate_detection: 'Duplicate Detection', duplicates_found: 'Duplicates Found',
  biometric_match: 'Biometric Match', resolution: 'Resolution',
  merge_records: 'Merge Records',

  // ElectionsPage
  elections: 'Elections', upcoming: 'Upcoming', ongoing: 'Ongoing',
  past: 'Past', election_type: 'Election Type', presidential: 'Presidential',
  gubernatorial: 'Gubernatorial', senatorial: 'Senatorial',

  // EnrollmentKioskPage
  enrollment_kiosk: 'Enrollment Kiosk', capture: 'Capture',
  quality_check: 'Quality Check', step: 'Step', start_enrollment: 'Start Enrollment',

  // ExportCenterPage
  export_center: 'Export Center', export_format: 'Export Format',
  csv: 'CSV', json: 'JSON', pdf: 'PDF', excel: 'Excel',
  date_range: 'Date Range',

  // GeofencingPage
  geofencing: 'Geofencing', boundary: 'Boundary', alert: 'Alert',
  geofence_violation: 'Geofence Violation', within_boundary: 'Within Boundary',

  // IncidentsPage
  incidents: 'Incidents', incident_type: 'Incident Type',
  priority: 'Priority', high: 'High', medium: 'Medium', low: 'Low',
  reported_by: 'Reported By', assigned_to: 'Assigned To',

  // IntegrityScorePage
  integrity: 'Integrity Score', overall_integrity: 'Overall Integrity',
  component_scores: 'Component Scores',

  // LoginPage
  sign_in: 'Sign In', email: 'Email', password: 'Password',
  forgot_password: 'Forgot Password?', remember_me: 'Remember Me',

  // MFAPage
  mfa: 'Multi-Factor Authentication', enter_code: 'Enter Code',
  verification_code: 'Verification Code', authenticator: 'Authenticator App',

  // MLDashboardPage
  ml_dashboard: 'ML Dashboard', model_performance: 'Model Performance',
  training_status: 'Training Status', inference_time: 'Inference Time',

  // MiddlewarePage
  middleware: 'Middleware Status', kafka: 'Kafka', redis: 'Redis',
  temporal: 'Temporal', opensearch: 'OpenSearch', connected: 'Connected',
  disconnected: 'Disconnected', messages_per_sec: 'Messages/sec',

  // ObserverMonitoringPage
  observer_monitoring: 'Observer Monitoring', live_reports: 'Live Reports',
  observer_count: 'Observer Count', coverage: 'Coverage',

  // PollingUnitsPage
  polling_units: 'Polling Units', total_pu: 'Total PUs',
  registered_voters: 'Registered Voters', accessibility: 'Accessibility',

  // PortalIntegrationPage
  portal_integration: 'Portal Integration', external_systems: 'External Systems',
  integration_status: 'Integration Status', api_health: 'API Health',

  // PredictiveAnalyticsPage
  predictive_analytics: 'Predictive Analytics', forecast: 'Forecast',
  trend: 'Trend', prediction: 'Prediction', accuracy: 'Accuracy',

  // ProductionPage
  production: 'Production Health', services: 'Services',
  cpu_usage: 'CPU Usage', memory_usage: 'Memory Usage', disk_usage: 'Disk Usage',

  // ScaleHealthPage
  scale_health: 'Scale & Health', throughput: 'Throughput',
  latency: 'Latency', tps: 'Transactions/sec', p99_latency: 'P99 Latency',

  // StakeholderPage
  stakeholder: 'Stakeholder Dashboard', political_parties: 'Political Parties',
  civil_society: 'Civil Society', media: 'Media',

  // TVDashboardPage
  tv_dashboard: 'TV Dashboard', live_broadcast: 'Live Broadcast',
  ticker: 'News Ticker', fullscreen: 'Fullscreen',

  // TrainingPage
  training: 'Training', modules: 'Modules', progress: 'Progress',
  certificate: 'Certificate', complete_module: 'Complete Module',

  // UserManagementPage
  user_management: 'User Management', users: 'Users', roles: 'Roles',
  add_user: 'Add User', role: 'Role', last_login: 'Last Login',

  // WebhookManagementPage
  webhook_management: 'Webhook Management', webhooks: 'Webhooks',
  endpoint: 'Endpoint', secret: 'Secret', events: 'Events',

  // WorkflowEnginePage
  workflow_engine: 'Workflow Engine', workflows: 'Workflows',
  triggers: 'Triggers', workflow_status: 'Workflow Status',
};

const PAGES_HA: Dict = {
  results_received: 'Sakamakon da aka karɓa', total_votes_cast: "Jimlar ƙuri'u",
  completion: 'Kammala', finalized: 'An kammala', rejected: 'An ƙi',
  election_date: 'Ranar zaɓe', real_time_stats: 'Ƙididdiga na ainihi',
  party_standings: 'Matsayin Jam\'iyyoyi', status_breakdown: 'Rarraba matsayi',
  recent_submissions: 'Sabon sakamako',
  live_results: 'Sakamakon zaɓe na ainihi', vote_count: "Adadin ƙuri'u",
  percentage_share: 'Kashi cikin ɗari', leading_party: "Jam'iyya mai nasara",
  margin: 'Bambanci', results_by_state: 'Sakamako ta jiha',
  results_by_zone: 'Sakamako ta yanki',
  street: 'Titin', satellite: 'Satilaid', compare: 'Kwatanta',
  pu_markers: 'Alamomin PU', box_select: 'Zaɓen Akwati',
  export_csv: 'Fitar da CSV', export_geojson: 'Fitar da GeoJSON',
  selection: 'Zaɓi', search_places: 'Nema wurare...',
  geolibre_gis: 'GeoLibre GIS', spatial_analysis: 'Nazarin sarari',
  live_tracking: 'Bibiyar ainihi', heatmap: 'Taswirar zafi',
  bvas_management: 'Kula da na\'urar BVAS', device_id: 'Lambar na\'ura',
  battery_level: 'Ƙarfin batir', last_sync: 'Haɗin ƙarshe',
  biometric_verification: 'Tantancewar biometric',
  fingerprint: 'Sawun yatsa', facial: 'Fuskar fuska', iris: 'Binciken ido',
  match_score: 'Makin daidaituwa', verification_status: 'Matsayin tantancewa',
  enroll: 'Rajista', verify: 'Tabbatar', template: 'Tsari',
  blockchain_ledger: 'Littafin blockchain', block_height: 'Tsayin tubali',
  transactions: 'Ma\'amaloli', chain_integrity: 'Gaskiyar sarƙa',
  voter_registration: 'Rajista mai zaɓe', vin: 'Lambar VIN',
  pvc_number: 'Lambar PVC', registration_center: 'Cibiyar rajista',
  anomaly_detection: 'Gano matsalolin AI', integrity_score: 'Makin gaskiya',
  total_anomalies: 'Jimlar matsaloli', overview: 'Bayani',
  benford_analysis: 'Nazarin Benford', anomaly_list: 'Jerin matsaloli',
  severity: 'Tsanani', no_anomalies: 'Ba a gano matsala ba',
  sms_verification: 'Tabbatarwa ta SMS/USSD', phone_number: 'Lambar waya',
  verify_result: 'Tabbatar da sakamako', result_found: 'An sami sakamako',
  public_api: "API na jama'a", api_version: 'Sigar API',
  kyc_verification: 'Tantancewar KYC', nin_lookup: 'Binciken NIN',
  liveness_check: 'Gwajin rayuwa', verified: 'An tabbatar', unverified: 'Ba a tabbatar ba',
  compliance_report: 'Rahoton bin doka', sanctions_check: 'Binciken takunkumi',
  ai_monitoring: 'Sa ido kan AI', admin_console: 'Kundin gudanarwa',
  audit_trail: 'Sawun bincike', citizen_portal: 'Ƙofar jama\'a',
  command_center: 'Cibiyar umarni', data_validation: 'Tabbatar da bayanai',
  dispute_resolution: 'Warware rikici', document_ai: 'Takardun AI',
  duplicate_detection: 'Gano kwafi', elections: 'Zaɓukan',
  enrollment_kiosk: 'Rumfar rajista', export_center: 'Cibiyar fitarwa',
  geofencing: 'Iyakar wurare', incidents: 'Abubuwan da suka faru',
  integrity: 'Makin gaskiya', sign_in: 'Shiga', email: 'Imel', password: 'Kalmar sirri',
  mfa: 'Tantancewa mai yawa', ml_dashboard: 'Shafin ML',
  middleware: 'Matsayin middleware', observer_monitoring: 'Sa ido kan masu sa ido',
  polling_units: 'Rumfunan zaɓe', predictive_analytics: 'Nazarin hasashe',
  production: 'Lafiyar samarwa', scale_health: 'Girma da lafiya',
  stakeholder: 'Shafin masu ruwa da tsaki', tv_dashboard: 'Shafin TV',
  training: 'Horarwa', user_management: 'Kula da masu amfani',
  webhook_management: 'Kula da webhook', workflow_engine: 'Injin aiki',
  connected: 'An haɗa', disconnected: 'An yanke',
  high: 'Babba', medium: 'Matsakaici', low: 'Ƙanƙanta',
  priority: 'Muhimmanci', incident_type: 'Nau\'in lamarin',
};

const PAGES_YO: Dict = {
  results_received: 'Àbájáde tí a gbà', total_votes_cast: 'Àpapọ̀ ìbò',
  completion: 'Ìparí', finalized: 'Parí', rejected: 'A kọ̀',
  election_date: 'Ọjọ́ ìdìbò', real_time_stats: 'Ìṣirò àkókò gidi',
  party_standings: 'Ipò Ẹgbẹ́', status_breakdown: 'Àlàyé ipò',
  recent_submissions: 'Àbájáde tuntun',
  live_results: 'Àbájáde àkókò gidi', vote_count: 'Iye ìbò',
  percentage_share: 'Ìpín nínú ọgọ́rùn', leading_party: 'Ẹgbẹ́ tó ṣáájú',
  margin: 'Ààlà', results_by_state: 'Àbájáde ní ìpínlẹ̀',
  street: 'Ọ̀nà', satellite: 'Satẹlaiti', compare: 'Fiwéra',
  pu_markers: 'Àmì PU', box_select: 'Yan Àpótí',
  geolibre_gis: 'GeoLibre GIS', spatial_analysis: 'Ìtúpalẹ̀ agbègbè',
  biometric_verification: 'Ìjẹ́rìísí biometric',
  fingerprint: 'Ìka ọwọ́', facial: 'Ojú', iris: 'Oju inu',
  match_score: 'Àmì ìbámu', enroll: 'Forúkọ sílẹ̀', verify: 'Ṣe ìjẹ́rìísí',
  blockchain_ledger: 'Ìwé àkọsílẹ̀ blockchain',
  voter_registration: 'Ìforúkọsílẹ̀ olùdìbò',
  anomaly_detection: 'Ìwádìí aíṣedeede AI', integrity_score: 'Àmì ìdúróṣinṣin',
  sms_verification: 'Ìjẹ́rìísí SMS/USSD', phone_number: 'Nọ́mbà fóònù',
  kyc_verification: 'Ìjẹ́rìísí KYC', liveness_check: 'Ìdánwò ààyè',
  compliance_report: 'Ìjábọ̀ ìbámu', admin_console: 'Ojú ìṣàkóso',
  audit_trail: 'Ipa ìṣàyẹ̀wò', citizen_portal: 'Ẹnu-ọ̀nà àra ìlú',
  command_center: 'Ibùdó àṣẹ', data_validation: 'Ìjẹ́rìísí dátà',
  dispute_resolution: 'Yíyanjú àríyànjíyan', document_ai: 'Ìwé AI',
  duplicate_detection: 'Wíwá ẹ̀dà', elections: 'Àwọn ìdìbò',
  enrollment_kiosk: 'Ibùdó ìforúkọsílẹ̀', export_center: 'Ibùdó ìfijáde',
  geofencing: 'Àgbègbè ààlà', incidents: 'Àwọn ìṣẹ̀lẹ̀',
  sign_in: 'Wọlé', email: 'Ímeèlì', password: 'Ọ̀rọ̀ aṣínà',
  mfa: 'Ìjẹ́rìísí Ọ̀pọ̀lọpọ̀', ml_dashboard: 'Ojú ìwé ML',
  middleware: 'Ipò Middleware', observer_monitoring: 'Ìṣàkíyèsí olùṣàkíyèsí',
  polling_units: 'Àwọn ibùdó ìdìbò', predictive_analytics: 'Ìtúpalẹ̀ àsọtẹ́lẹ̀',
  production: 'Ìlera iṣelọ́pọ̀', stakeholder: 'Ojú ìwé olùkópa',
  tv_dashboard: 'Ojú ìwé TV', training: 'Ìdánilékọ̀ọ́',
  user_management: 'Ìṣàkóso olùlò', webhook_management: 'Ìṣàkóso webhook',
  workflow_engine: 'Ẹ̀rọ iṣẹ́',
  connected: 'A ti sopọ̀', disconnected: 'A ti yà kúrò',
  high: 'Gíga', medium: 'Àárín', low: 'Kékeré',
  verified: 'Jẹ́rìísí', unverified: 'Kò jẹ́rìísí',
};

const PAGES_IG: Dict = {
  results_received: 'Nsonaazụ e natara', total_votes_cast: 'Mkpokọta votu',
  completion: 'Mmezu', finalized: 'Emechara', rejected: 'Ajụrụ',
  election_date: 'Ụbọchị ntuli aka', real_time_stats: 'Ọnụ ọgụgụ oge a',
  party_standings: 'Ọnọdụ otu', status_breakdown: 'Nkọwa ọnọdụ',
  recent_submissions: 'Nsonaazụ ọhụrụ',
  live_results: 'Nsonaazụ ntuli aka oge a', vote_count: 'Ọnụ ọgụgụ votu',
  leading_party: 'Otu na-edu', margin: 'Oke',
  street: 'Ụzọ', satellite: 'Satẹlaịtị', compare: 'Tụnyere',
  geolibre_gis: 'GeoLibre GIS', spatial_analysis: 'Nyocha oghere',
  biometric_verification: 'Nyocha biometric',
  fingerprint: 'Mkpụrụ aka', facial: 'Ihu', iris: 'Anya',
  blockchain_ledger: 'Akwụkwọ blockchain',
  voter_registration: 'Ndebanye aha onye ntuli aka',
  anomaly_detection: 'Nchọpụta nsogbu AI', integrity_score: 'Akara eziokwu',
  sms_verification: 'Nyocha SMS/USSD', phone_number: 'Nọmbà ekwentị',
  kyc_verification: 'Nyocha KYC', liveness_check: 'Ule ndụ',
  compliance_report: 'Akụkọ mmekọrịta', admin_console: 'Pánélụ nchịkwa',
  audit_trail: 'Ụzọ nyocha', citizen_portal: 'Ọnụ ụzọ ndị mmadụ',
  command_center: 'Ebe iwu', data_validation: 'Nyocha data',
  dispute_resolution: 'Idozi nsogbu', document_ai: 'Akwụkwọ AI',
  duplicate_detection: 'Nchọpụta okopị', elections: 'Ntuli aka',
  sign_in: 'Banye', email: 'Email', password: 'Okwuntụghe',
  mfa: 'Nyocha ọtụtụ', middleware: 'Ọnọdụ middleware',
  observer_monitoring: 'Nleba anya ndị nleba anya',
  polling_units: 'Ebe ntuli aka', predictive_analytics: 'Nyocha amụma',
  production: 'Ahụ ike mmepụta', stakeholder: 'Pánélụ ndị metụtara ya',
  tv_dashboard: 'Pánélụ TV', training: 'Ọzụzụ',
  user_management: 'Njikwa ndị ọrụ', webhook_management: 'Njikwa webhook',
  workflow_engine: 'Injin ọrụ',
  connected: 'Jikọtara', disconnected: 'Ekwupụsịrị',
  high: 'Elu', medium: 'Etiti', low: 'Ala',
  verified: 'Enyochara', unverified: 'Anyochaghị',
};

const PAGES_PCM: Dict = {
  results_received: 'Results Wey Enter', total_votes_cast: 'All di Votes',
  completion: 'How Far', finalized: 'Don Finish', rejected: 'Dem Reject Am',
  election_date: 'Election Day', real_time_stats: 'Live Numbers',
  party_standings: 'Party Rankings', recent_submissions: 'New Results',
  live_results: 'Live Election Results', vote_count: 'How Many Votes',
  leading_party: 'Party Wey Dey Win', margin: 'How Much Lead',
  street: 'Street', satellite: 'Satellite', compare: 'Compare',
  pu_markers: 'PU Markers', search_places: 'Find places...',
  geolibre_gis: 'GeoLibre Map', spatial_analysis: 'Area Analysis',
  biometric_verification: 'Biometric Check',
  fingerprint: 'Finger Print', facial: 'Face Check', iris: 'Eye Scan',
  blockchain_ledger: 'Blockchain Record',
  voter_registration: 'Voter Registration',
  anomaly_detection: 'AI Wahala Detector', integrity_score: 'Trust Score',
  sms_verification: 'SMS/USSD Check', phone_number: 'Phone Number',
  kyc_verification: 'KYC Check', liveness_check: 'Liveness Test',
  compliance_report: 'Compliance Report', admin_console: 'Admin Panel',
  audit_trail: 'Audit Record', citizen_portal: 'Citizen Portal',
  command_center: 'Command Center', data_validation: 'Data Check',
  dispute_resolution: 'Settle Dispute', document_ai: 'Document AI',
  duplicate_detection: 'Find Duplicates', elections: 'Elections',
  sign_in: 'Enter', email: 'Email', password: 'Password',
  mfa: 'Extra Security Check', middleware: 'System Status',
  observer_monitoring: 'Observer Tracking',
  polling_units: 'Polling Units', predictive_analytics: 'Prediction',
  production: 'System Health', stakeholder: 'Stakeholder Page',
  tv_dashboard: 'TV Display', training: 'Training',
  user_management: 'Manage Users', webhook_management: 'Manage Webhooks',
  workflow_engine: 'Workflow System',
  connected: 'Connected', disconnected: 'No Connect',
  high: 'High', medium: 'Medium', low: 'Low',
  verified: 'Verified', unverified: 'Not Verified',
  enroll: 'Register', verify: 'Verify',
};

// ── Merge all dictionaries ──────────────────────────────────────
function merge(...dicts: Dict[]): Dict {
  const result: Dict = {};
  for (const d of dicts) {
    Object.assign(result, d);
  }
  return result;
}

export const DICTS: Record<Lang, Dict> = {
  en: merge(COMMON_EN, PAGES_EN),
  ha: merge(COMMON_HA, PAGES_HA),
  yo: merge(COMMON_YO, PAGES_YO),
  ig: merge(COMMON_IG, PAGES_IG),
  pcm: merge(COMMON_PCM, PAGES_PCM),
};
