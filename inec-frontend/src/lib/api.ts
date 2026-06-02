const API_URL = import.meta.env.VITE_API_URL ?? '';

export class ApiError extends Error {
  constructor(public status: number, public detail: string) {
    super(detail);
    this.name = 'ApiError';
  }
}

function getToken(): string | null {
  return localStorage.getItem('token') || localStorage.getItem('inec_token');
}

function xhrFallback(path: string, method: string, headers: Record<string, string>, body?: string): unknown {
  const xhr = new XMLHttpRequest();
  xhr.open(method, `${API_URL}${path}`, false);
  Object.entries(headers).forEach(([k, v]) => xhr.setRequestHeader(k, v));
  xhr.send(body || null);
  if (xhr.status === 0) throw new Error('XHR failed: network error');
  if (xhr.status >= 400) {
    const err = (() => { try { return JSON.parse(xhr.responseText); } catch { return { detail: xhr.statusText }; } })();
    throw new ApiError(xhr.status, err.detail || err.error || 'Request failed');
  }
  try { return JSON.parse(xhr.responseText); } catch { return xhr.responseText; }
}

async function request(path: string, options: RequestInit = {}, retries = 2) {
  const token = getToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> || {}),
  };
  if (token) headers['Authorization'] = `Bearer ${token}`;

  for (let attempt = 0; attempt <= retries; attempt++) {
    try {
      const res = await fetch(`${API_URL}${path}`, { ...options, headers });
      if (!res.ok) {
        const err = await res.json().catch(() => ({ detail: res.statusText }));
        if (attempt < retries && res.status >= 500) {
          await new Promise(r => setTimeout(r, 1000 * (attempt + 1)));
          continue;
        }
        throw new ApiError(res.status, err.detail || err.error || 'Request failed');
      }
      return res.json();
    } catch (err) {
      if (err instanceof ApiError) throw err;
      // Fallback to synchronous XHR when fetch() fails (e.g. in automation environments)
      if (err instanceof TypeError && (err.message === 'Failed to fetch' || err.message === 'NetworkError when attempting to fetch resource.')) {
        try {
          return xhrFallback(path, options.method || 'GET', headers, options.body as string | undefined);
        } catch (xhrErr) {
          if (xhrErr instanceof ApiError) throw xhrErr;
          if (attempt === retries) throw xhrErr;
        }
      }
      if (attempt === retries) throw err;
      await new Promise(r => setTimeout(r, 1000 * (attempt + 1)));
    }
  }
  throw new Error('request failed after retries');
}

export const api = {
  login: (username: string, password: string) =>
    request('/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
  register: (data: { username: string; password: string; full_name: string; role?: string }) =>
    request('/auth/register', { method: 'POST', body: JSON.stringify(data) }),
  getMe: () => request('/auth/me'),

  getElections: (status?: string) =>
    request(`/elections${status ? `?status=${status}` : ''}`),
  getElection: (id: number) => request(`/elections/${id}`),
  getElectionStats: (id: number) => request(`/elections/${id}/stats`),
  createElection: (data: Record<string, string>) =>
    request('/elections', { method: 'POST', body: JSON.stringify(data) }),
  updateElection: (id: number, data: Record<string, string>) =>
    request(`/elections/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),

  getDashboardStats: (electionId: number) =>
    request(`/dashboard/stats?election_id=${electionId}`),
  getLiveFeed: (electionId: number, limit = 20) =>
    request(`/dashboard/live-feed?election_id=${electionId}&limit=${limit}`),
  getCollation: (electionId: number, level: string, parentCode?: string) =>
    request(`/dashboard/collation?election_id=${electionId}&level=${level}${parentCode ? `&parent_code=${parentCode}` : ''}`),

  getResults: (electionId: number, params?: Record<string, string>) => {
    const q = new URLSearchParams({ election_id: String(electionId), ...params });
    return request(`/results?${q}`);
  },
  getResult: (id: number) => request(`/results/${id}`),
  submitResult: (data: Record<string, unknown>) =>
    request('/results/submit', { method: 'POST', body: JSON.stringify(data) }),
  validateResult: (id: number) =>
    request(`/results/${id}/validate`, { method: 'POST' }),
  finalizeResult: (id: number) =>
    request(`/results/${id}/finalize`, { method: 'POST' }),
  disputeResult: (id: number) =>
    request(`/results/${id}/dispute`, { method: 'POST' }),

  getStates: () => request('/geo/states'),
  getLgas: (stateCode?: string) =>
    request(`/geo/lgas${stateCode ? `?state_code=${stateCode}` : ''}`),
  getWards: (lgaCode?: string) =>
    request(`/geo/wards${lgaCode ? `?lga_code=${lgaCode}` : ''}`),
  getPollingUnits: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/geo/polling-units?${q}`);
  },

  getParties: () => request('/parties'),

  getAuditTrail: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/audit/trail?${q}`);
  },
  verifyResult: (id: number) => request(`/audit/verify/${id}`),
  getAuditStats: () => request('/audit/stats'),

  getIncidents: (electionId: number) =>
    request(`/incidents?election_id=${electionId}`),
  createIncident: (data: Record<string, unknown>) =>
    request('/incidents', { method: 'POST', body: JSON.stringify(data) }),

  getMapData: (electionId: number, stateCode?: string) =>
    request(`/geo/map-data?election_id=${electionId}${stateCode ? `&state_code=${stateCode}` : ''}`),

  getMiddlewareStatus: () => request('/middleware/status'),
  getMiddlewareHealth: () => request('/middleware/health'),
  getKafkaTopics: () => request('/middleware/kafka/topics'),
  getTemporalWorkflows: () => request('/middleware/temporal/workflows'),
  getTigerBeetleAccounts: () => request('/middleware/tigerbeetle/accounts'),
  getAPISIXRoutes: () => request('/middleware/apisix/routes'),
  getRedisStats: () => request('/middleware/redis/stats'),
  getFluvioTopics: () => request('/middleware/fluvio/topics'),
  getLakehouseTables: () => request('/middleware/lakehouse/tables'),
  getLakehouseAnalytics: (electionId: number, type: string) =>
    request(`/middleware/lakehouse/analytics/${electionId}/${type}`),

  getBVASSummary: (electionId: number) => request(`/bvas/summary?election_id=${electionId}`),
  getBVASDevices: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/bvas/devices?${q}`);
  },
  getBVASReconciliation: (electionId: number, flaggedOnly?: boolean) =>
    request(`/bvas/reconciliation?election_id=${electionId}${flaggedOnly ? '&flagged_only=true' : ''}`),
  getBVASAccreditationFeed: (electionId: number, limit?: number) =>
    request(`/bvas/accreditation/feed?election_id=${electionId}&limit=${limit || 50}`),
  getBVASAccreditationTimeline: (electionId: number, interval?: string) =>
    request(`/bvas/accreditation/timeline?election_id=${electionId}&interval=${interval || 'hour'}`),
  getIngestionStats: () => request('/ingestion/stats'),
  getIngestionJobs: (status?: string) =>
    request(`/ingestion/jobs${status ? `?status=${status}` : ''}`),
  getDeadLetterQueue: () => request('/ingestion/dead-letter'),

  smsVerify: (phone: string, pollingUnitCode: string) =>
    request('/sms/verify', { method: 'POST', body: JSON.stringify({ phone, polling_unit_code: pollingUnitCode }) }),
  ussdGateway: (sessionId: string, phoneNumber: string, text: string) =>
    request('/ussd/gateway', { method: 'POST', body: JSON.stringify({ sessionId, phoneNumber, text }) }),
  getSMSStats: () => request('/sms/stats'),

  getAIAnomalies: (electionId: number, severity?: string) =>
    request(`/ai/anomalies?election_id=${electionId}${severity ? `&severity=${severity}` : ''}`),
  getAIBenford: (electionId: number) =>
    request(`/ai/benford?election_id=${electionId}`),
  getAIIntegrity: (electionId: number) =>
    request(`/ai/integrity?election_id=${electionId}`),
  getAIMethods: () => request('/ai/methods'),

  getPublicAPIDocs: () => request('/api/v1/docs'),
  generateAPIKey: (name: string, owner: string) =>
    request('/api/v1/keys', { method: 'POST', body: JSON.stringify({ name, owner }) }),
  getAPIKeys: () => request('/api/v1/keys'),
  getAPIUsage: () => request('/api/v1/usage'),

  getEMSVoters: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/ems/voters?${q}`);
  },
  getEMSVoterStats: (stateCode?: string) =>
    request(`/ems/voters/stats${stateCode ? `?state_code=${stateCode}` : ''}`),
  getEMSRegistrationCenters: (stateCode?: string) =>
    request(`/ems/registration-centers${stateCode ? `?state_code=${stateCode}` : ''}`),

  getEMSWorkflows: (electionId?: number) =>
    request(`/ems/workflows${electionId ? `?election_id=${electionId}` : ''}`),
  getEMSWorkflow: (id: number) => request(`/ems/workflows/${id}`),
  advanceEMSWorkflow: (id: number) =>
    request(`/ems/workflows/${id}/advance`, { method: 'POST' }),

  getEMSSyncStats: (deviceId?: string) =>
    request(`/ems/sync/stats${deviceId ? `?device_id=${deviceId}` : ''}`),
  getEMSSyncQueue: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/ems/sync/queue?${q}`);
  },
  resolveEMSSyncConflict: (id: number, resolution: string) =>
    request(`/ems/sync/conflicts/${id}/resolve`, { method: 'POST', body: JSON.stringify({ resolution }) }),

  getEMSPortalStatus: () => request('/ems/portals/status'),
  getEMSPortal: (id: number) => request(`/ems/portals/${id}`),
  syncEMSPortal: (id: number) =>
    request(`/ems/portals/${id}/sync`, { method: 'POST' }),
  getEMSPortalSyncLog: () => request('/ems/portals/sync-log'),

  getEMSValidationRules: (entityType?: string) =>
    request(`/ems/validation/rules${entityType ? `?entity_type=${entityType}` : ''}`),
  getEMSValidationStats: () => request('/ems/validation/stats'),
  getEMSValidationHistory: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/ems/validation/history?${q}`);
  },

  getEMSLifecycle: (electionId: number) => request(`/ems/elections/${electionId}/lifecycle`),
  getEMSStaff: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/ems/staff?${q}`);
  },
  getEMSMaterials: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/ems/materials?${q}`);
  },
  getEMSMaterialStats: (electionId?: number) =>
    request(`/ems/materials/stats${electionId ? `?election_id=${electionId}` : ''}`),
  getEMSDashboard: (electionId: number) => request(`/ems/dashboard?election_id=${electionId}`),

  getBiometricStats: () => request('/biometric/stats'),
  getBiometricProfiles: (limit?: number, offset?: number) =>
    request(`/biometric/profiles?limit=${limit || 50}&offset=${offset || 0}`),
  biometricVerify: (vin: string, modality: string) =>
    request('/biometric/verify', { method: 'POST', body: JSON.stringify({ vin, modality }) }),
  getABISDuplicates: (status?: string) =>
    request(`/biometric/abis/duplicates${status ? `?status=${status}` : ''}`),
  resolveABISDuplicate: (id: number, status: string) =>
    request(`/biometric/abis/${id}/resolve`, { method: 'POST', body: JSON.stringify({ status }) }),

  getBiometricEngineStats: () => request('/biometric/engine/stats'),
  abisEnroll: (vin: string, modality: string, deviceId?: string) =>
    request('/biometric/engine/enroll', { method: 'POST', body: JSON.stringify({ vin, modality, device_id: deviceId || 'BVAS-001' }) }),
  abisVerify: (vin: string, modality: string, deviceId?: string) =>
    request('/biometric/engine/verify', { method: 'POST', body: JSON.stringify({ vin, modality, device_id: deviceId || 'BVAS-001' }) }),
  abisMultiModalVerify: (vin: string, deviceId?: string) =>
    request('/biometric/engine/verify-multimodal', { method: 'POST', body: JSON.stringify({ vin, device_id: deviceId || 'BVAS-001' }) }),
  abisIdentify: (vin: string, modality?: string, limit?: number) =>
    request(`/biometric/engine/identify?vin=${vin}&modality=${modality || 'fingerprint'}&limit=${limit || 10}`),
  padCheck: (vin: string, modality: string, deviceId?: string) =>
    request('/biometric/engine/pad-check', { method: 'POST', body: JSON.stringify({ vin, modality, device_id: deviceId || 'BVAS-001' }) }),
  getPADHistory: (vin?: string, limit?: number) =>
    request(`/biometric/engine/pad-history?${vin ? `vin=${vin}&` : ''}limit=${limit || 50}`),
  getDedupJobs: () => request('/biometric/engine/dedup/jobs'),
  startDedupJob: (type?: string, modalities?: string, threshold?: number) =>
    request('/biometric/engine/dedup/start', { method: 'POST', body: JSON.stringify({ type: type || 'incremental', modalities: modalities || 'fingerprint', threshold: threshold || 0.85 }) }),
  getDedupCandidates: (jobId: number, decision?: string) =>
    request(`/biometric/engine/dedup/${jobId}/candidates${decision ? `?decision=${decision}` : ''}`),
  resolveDedupCandidate: (id: number, decision: string, reviewer?: string) =>
    request(`/biometric/engine/dedup/resolve/${id}`, { method: 'POST', body: JSON.stringify({ decision, reviewer }) }),
  getVaultStats: () => request('/biometric/engine/vault/stats'),
  rotateVaultKey: (keyId: string) =>
    request('/biometric/engine/vault/rotate-key', { method: 'POST', body: JSON.stringify({ key_id: keyId }) }),
  getVaultAudit: (vin?: string, limit?: number) =>
    request(`/biometric/engine/vault/audit?${vin ? `vin=${vin}&` : ''}limit=${limit || 50}`),
  getBVASDeviceCapabilities: () => request('/biometric/engine/devices'),
  registerBVASDevice: (deviceId: string, firmware: string, modalities: string[], meta?: Record<string, unknown>) =>
    request('/biometric/engine/devices/register', { method: 'POST', body: JSON.stringify({ device_id: deviceId, firmware, modalities, meta }) }),
  getBVASCaptureSessions: (deviceId?: string, limit?: number) =>
    request(`/biometric/engine/capture-sessions?${deviceId ? `device_id=${deviceId}&` : ''}limit=${limit || 50}`),
  getABISPipeline: () => request('/biometric/engine/pipeline'),
  getABISConfig: () => request('/biometric/engine/config'),
  updateABISConfig: (config: Record<string, number>) =>
    request('/biometric/engine/config', { method: 'POST', body: JSON.stringify(config) }),
  getTemplateIntegrity: (vin: string) => request(`/biometric/engine/template-integrity?vin=${vin}`),

  getAdvancedBiometricStats: () => request('/biometric/advanced/stats'),
  getHSMStats: () => request('/biometric/advanced/hsm/stats'),
  generateHSMKey: (purpose: string, slot: number) =>
    request('/biometric/advanced/hsm/generate-key', { method: 'POST', body: JSON.stringify({ purpose, slot }) }),
  getSDKProviders: () => request('/biometric/advanced/sdk/providers'),
  getTemplateAging: (vin?: string, modality?: string) =>
    request(`/biometric/advanced/aging?${vin ? `vin=${vin}&` : ''}${modality ? `modality=${modality}` : ''}`),
  getCancelableStatus: (vin?: string) =>
    request(`/biometric/advanced/cancelable${vin ? `?vin=${vin}` : ''}`),
  revokeCancelable: (vin: string, modality: string, reason?: string) =>
    request('/biometric/advanced/cancelable/revoke', { method: 'POST', body: JSON.stringify({ vin, modality, reason }) }),
  getThresholdTuning: () => request('/biometric/advanced/threshold-tuning'),
  runThresholdTuning: (modality: string) =>
    request('/biometric/advanced/threshold-tuning', { method: 'POST', body: JSON.stringify({ modality }) }),
  runDistributedDedup: (modality?: string, workers?: number, threshold?: number) =>
    request('/biometric/advanced/distributed-dedup', { method: 'POST', body: JSON.stringify({ modality, workers, threshold }) }),
  getPADModels: () => request('/biometric/advanced/pad-models'),
  deployPADUpdate: (modelId: string, newVersion: string) =>
    request('/biometric/advanced/pad-models/update', { method: 'POST', body: JSON.stringify({ model_id: modelId, new_version: newVersion }) }),
  getQualityGateway: () => request('/biometric/advanced/quality-gateway'),
  evaluateQuality: (deviceId: string, vin: string, modality: string, quality: number, nfiq2: number) =>
    request('/biometric/advanced/quality-gateway', { method: 'POST', body: JSON.stringify({ device_id: deviceId, vin, modality, quality, nfiq2 }) }),
  getOfflineQueue: () => request('/biometric/advanced/offline-queue'),
  triggerOfflineSync: (deviceId: string) =>
    request('/biometric/advanced/offline-queue/sync', { method: 'POST', body: JSON.stringify({ device_id: deviceId }) }),
  normalizeScore: (score: number, modality: string, normType: string) =>
    request('/biometric/advanced/score-normalize', { method: 'POST', body: JSON.stringify({ score, modality, norm_type: normType }) }),
  getScoreCohorts: () => request('/biometric/advanced/score-cohorts'),
  getNISTBenchmarks: () => request('/biometric/advanced/nist-benchmark'),
  runNISTBenchmark: (type: string, modality: string) =>
    request('/biometric/advanced/nist-benchmark', { method: 'POST', body: JSON.stringify({ type, modality }) }),
  getBioAuditTimeline: (limit?: number, category?: string, severity?: string) =>
    request(`/biometric/advanced/audit/timeline?limit=${limit || 50}${category ? `&category=${category}` : ''}${severity ? `&severity=${severity}` : ''}`),
  getBioAuditSummary: () => request('/biometric/advanced/audit/summary'),
  startKioskSession: (deviceId: string, vin?: string) =>
    request('/biometric/advanced/kiosk/start', { method: 'POST', body: JSON.stringify({ device_id: deviceId, vin }) }),
  advanceKioskStep: (sessionId: string) =>
    request(`/biometric/advanced/kiosk/${sessionId}/advance`, { method: 'POST' }),
  getKioskSessions: (limit?: number) =>
    request(`/biometric/advanced/kiosk/sessions?limit=${limit || 20}`),
  enrollMultiFinger: (vin: string, fingers?: string[], primaryFinger?: string) =>
    request('/biometric/advanced/multi-finger/enroll', { method: 'POST', body: JSON.stringify({ vin, fingers, primary_finger: primaryFinger }) }),
  getMultiFingerStatus: (vin?: string) =>
    request(`/biometric/advanced/multi-finger${vin ? `?vin=${vin}` : ''}`),
  privacyMatch: (vin: string, modality?: string) =>
    request('/biometric/advanced/privacy-match', { method: 'POST', body: JSON.stringify({ vin, modality }) }),
  getPrivacyStats: () => request('/biometric/advanced/privacy-stats'),

  getBlockchainStats: () => request('/blockchain/stats'),
  getBlockchainChain: (limit?: number) =>
    request(`/blockchain/chain?limit=${limit || 50}`),
  getSmartContracts: () => request('/blockchain/contracts'),
  blockchainVerifyResult: (resultId: number) =>
    request(`/blockchain/verify/${resultId}`),
  getBlockchainAudit: (limit?: number) =>
    request(`/blockchain/audit?limit=${limit || 50}`),

  getBlockchainProductionStats: () => request('/blockchain/production/stats'),
  getFabricNetwork: () => request('/blockchain/fabric/network'),
  getFabricBlocks: (limit?: number) =>
    request(`/blockchain/fabric/blocks?limit=${limit || 20}`),
  getFabricTransactions: (limit?: number) =>
    request(`/blockchain/fabric/transactions?limit=${limit || 50}`),
  verifyFabricChain: (limit?: number) =>
    request(`/blockchain/fabric/verify-chain?limit=${limit || 100}`),
  submitFabricTx: (channel: string, chaincode: string, fn: string, args: string[]) =>
    request('/blockchain/fabric/submit', { method: 'POST', body: JSON.stringify({ channel, chaincode, function: fn, args }) }),
  chaincodeValidateResult: (resultId: number, puCode: string, electionId: number, totalVotes: number, accredited: number) =>
    request('/blockchain/chaincode/validate-result', { method: 'POST', body: JSON.stringify({ result_id: resultId, pu_code: puCode, election_id: electionId, total_votes: totalVotes, accredited }) }),
  chaincodeAggregate: (level: string, areaCode: string, electionId: number) =>
    request('/blockchain/chaincode/aggregate', { method: 'POST', body: JSON.stringify({ level, area_code: areaCode, election_id: electionId }) }),
  getIPFSStats: () => request('/blockchain/ipfs/stats'),
  storeIPFS: (data: string, contentType?: string) =>
    request('/blockchain/ipfs/store', { method: 'POST', body: JSON.stringify({ data, content_type: contentType }) }),
  verifyIPFS: (cid: string) => request(`/blockchain/ipfs/verify?cid=${cid}`),
  getIPFSObjects: (limit?: number, contentType?: string) =>
    request(`/blockchain/ipfs/objects?limit=${limit || 50}${contentType ? `&content_type=${contentType}` : ''}`),
  getLedgerStats: () => request('/blockchain/ledger/stats'),
  getLedgerAccounts: () => request('/blockchain/ledger/accounts'),
  getLedgerTransfers: (accountId?: string, limit?: number) =>
    request(`/blockchain/ledger/transfers?account_id=${accountId || 'inec-operational'}&limit=${limit || 50}`),
  createLedgerTransfer: (debitAccount: string, creditAccount: string, amount: number, userData?: string) =>
    request('/blockchain/ledger/transfer', { method: 'POST', body: JSON.stringify({ debit_account: debitAccount, credit_account: creditAccount, amount, user_data: userData }) }),
  postLedgerTransfer: (transferId: string) =>
    request('/blockchain/ledger/transfer/post', { method: 'POST', body: JSON.stringify({ transfer_id: transferId }) }),
  buildMerkleTree: (leaves: string[], treeType?: string) =>
    request('/blockchain/merkle/build', { method: 'POST', body: JSON.stringify({ leaves, tree_type: treeType }) }),
  getMerkleTrees: (limit?: number) =>
    request(`/blockchain/merkle/trees?limit=${limit || 20}`),

  getTrainingCourses: (role?: string) =>
    request(`/training/courses${role ? `?role=${role}` : ''}`),
  getTrainingStats: () => request('/training/stats'),
  getTrainingEnrollments: (courseId?: number) =>
    request(`/training/enrollments${courseId ? `?course_id=${courseId}` : ''}`),
  enrollInCourse: (courseId: number) =>
    request('/training/enrollments', { method: 'POST', body: JSON.stringify({ course_id: courseId }) }),
  completeTraining: (enrollmentId: number, score: number) =>
    request(`/training/enrollments/${enrollmentId}/complete`, { method: 'POST', body: JSON.stringify({ score }) }),
  getTrainingCertificates: () => request('/training/certificates'),
  getVRScenarios: () => request('/training/vr-scenarios'),

  // Stakeholder engagement
  resolveGrievance: (id: number, resolution: string) =>
    request(`/stakeholders/grievances/${id}`, { method: 'PATCH', body: JSON.stringify({ resolution, status: 'resolved' }) }),

  getStakeholderStats: () => request('/stakeholders/stats'),
  getStakeholders: (type?: string, status?: string) => {
    const p = new URLSearchParams();
    if (type) p.set('type', type);
    if (status) p.set('status', status);
    return request(`/stakeholders?${p}`);
  },
  getStakeholderIncidents: (severity?: string, status?: string) => {
    const p = new URLSearchParams();
    if (severity) p.set('severity', severity);
    if (status) p.set('status', status);
    return request(`/stakeholders/incidents?${p}`);
  },
  getGrievances: () => request('/stakeholders/grievances'),
  getPushNotifications: () => request('/stakeholders/notifications'),
  sendNotification: (data: { title: string; body: string; target_type?: string; target_value?: string; type?: string }) =>
    request('/stakeholders/notifications', { method: 'POST', body: JSON.stringify(data) }),

  getAIMonitoringDashboard: () => request('/ai-monitoring/dashboard'),
  getAIPredictions: (type?: string) =>
    request(`/ai-monitoring/predictions${type ? `?type=${type}` : ''}`),
  getSentimentAnalysis: () => request('/ai-monitoring/sentiment'),
  getMisinformationAlerts: (status?: string) =>
    request(`/ai-monitoring/misinformation${status ? `?status=${status}` : ''}`),
  getSecurityThreats: (status?: string) =>
    request(`/ai-monitoring/security-threats${status ? `?status=${status}` : ''}`),
  getCVMonitoring: () => request('/ai-monitoring/cv-monitoring'),

  getProductionStatus: () => request('/production/status'),
  getProductionHSMStats: () => request('/production/hsm/stats'),
  productionHSMGenerateKey: (purpose: string, algorithm?: string) =>
    request('/production/hsm/generate-key', { method: 'POST', body: JSON.stringify({ purpose, algorithm }) }),
  productionHSMSign: (keyId: string, data: string) =>
    request('/production/hsm/sign', { method: 'POST', body: JSON.stringify({ key_id: keyId, data }) }),
  productionHSMVerify: (keyId: string, data: string, signature: string) =>
    request('/production/hsm/verify', { method: 'POST', body: JSON.stringify({ key_id: keyId, data, signature }) }),
  productionHSMRotate: (keyId: string) =>
    request('/production/hsm/rotate', { method: 'POST', body: JSON.stringify({ key_id: keyId }) }),
  getProductionSMSStats: () => request('/production/sms/stats'),
  productionSMSSend: (phone: string, message: string) =>
    request('/production/sms/send', { method: 'POST', body: JSON.stringify({ phone, message }) }),
  getProductionSMSDeliveryLog: () => request('/production/sms/delivery-log'),
  getProductionPADStats: () => request('/production/pad/stats'),
  productionPADCheck: (voterId: string, modality: string) =>
    request('/production/pad/check', { method: 'POST', body: JSON.stringify({ voter_id: voterId, modality }) }),
  getProductionPADAttackLog: () => request('/production/pad/attack-log'),
  getProductionIPFSStats: () => request('/production/ipfs/stats'),
  productionIPFSStore: (data: string, codec?: string) =>
    request('/production/ipfs/store', { method: 'POST', body: JSON.stringify({ data, codec }) }),
  productionIPFSVerify: (cid: string) => request(`/production/ipfs/verify?cid=${cid}`),
  getProductionFabricStats: () => request('/production/fabric/stats'),
  productionFabricSubmit: (channel: string, chaincode: string, fn: string, args: string[]) =>
    request('/production/fabric/submit', { method: 'POST', body: JSON.stringify({ channel, chaincode, function: fn, args }) }),
  getProductionFabricEndorsements: (txId?: string) =>
    request(`/production/fabric/verify-endorsements${txId ? `?tx_id=${txId}` : ''}`),
  getProductionLedgerStats: () => request('/production/ledger/stats'),
  productionLedgerTransfer: (debitAccount: string, creditAccount: string, amount: number, idempotencyKey?: string) =>
    request('/production/ledger/transfer', { method: 'POST', body: JSON.stringify({ debit_account: debitAccount, credit_account: creditAccount, amount, idempotency_key: idempotencyKey }) }),
  getProductionLedgerJournal: (transferId?: string) =>
    request(`/production/ledger/journal${transferId ? `?transfer_id=${transferId}` : ''}`),
  getDBMetrics: () => request('/db/metrics'),
  getDBPool: () => request('/db/pool'),
  getPgpoolStatus: () => request('/pgpool/status'),
  getPgpoolNodes: () => request('/pgpool/nodes'),
  getPgpoolHealth: () => request('/pgpool/health'),
  getPgpoolMetrics: () => request('/pgpool/metrics'),
  getPgpoolReplication: () => request('/pgpool/replication'),
  getPgpoolDashboard: () => request('/pgpool/dashboard'),
};
