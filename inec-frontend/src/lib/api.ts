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

function handleAuthFailure() {
  localStorage.removeItem('token');
  localStorage.removeItem('user');
  localStorage.removeItem('inec_token');
  window.location.reload();
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
        if (res.status === 401 && path !== '/auth/login' && path !== '/auth/refresh') {
          handleAuthFailure();
          throw new ApiError(401, 'Session expired');
        }
        if (attempt < retries && res.status >= 500) {
          await new Promise(r => setTimeout(r, 1000 * (attempt + 1)));
          continue;
        }
        throw new ApiError(res.status, err.detail || err.error || 'Request failed');
      }
      return res.json();
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 401) throw err;
        throw err;
      }
      // Fallback to synchronous XHR when fetch() fails (e.g. in automation environments)
      if (err instanceof TypeError && (err.message === 'Failed to fetch' || err.message === 'NetworkError when attempting to fetch resource.')) {
        try {
          return xhrFallback(path, options.method || 'GET', headers, options.body as string | undefined);
        } catch (xhrErr) {
          if (xhrErr instanceof ApiError) {
            if (xhrErr.status === 401 && path !== '/auth/login' && path !== '/auth/refresh') {
              handleAuthFailure();
            }
            throw xhrErr;
          }
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
  bvasAccreditation: (data: Record<string, unknown>) =>
    request('/bvas/accreditation', { method: 'POST', body: JSON.stringify(data) }),
  resolveDispute: (id: number, resolution: string) =>
    request(`/disputes/${id}/resolve`, { method: 'POST', body: JSON.stringify({ resolution }) }),
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

  // Auth / Session Management
  getAuthSessions: () => request('/auth/sessions'),
  revokeSession: (sessionId: string) =>
    request('/auth/sessions/revoke', { method: 'POST', body: JSON.stringify({ session_id: sessionId }) }),
  revokeAllSessions: () =>
    request('/auth/sessions/revoke-all', { method: 'POST' }),
  rotateAPIKey: () =>
    request('/auth/api-keys/rotate', { method: 'POST', body: JSON.stringify({ name: 'default-api-key' }) }),

  // Geofencing
  geofenceCheck: (lat: number, lng: number, puCode: string, bvasSerial?: string) =>
    request('/geofence/check', { method: 'POST', body: JSON.stringify({ latitude: lat, longitude: lng, polling_unit_code: puCode, bvas_serial: bvasSerial || 'BVAS-DEFAULT' }) }),
  getGeofenceStats: (electionId: number) => request(`/geofence/stats/${electionId}`),

  // GPS Spoofing Detection
  gpsSpoofCheck: (lat: number, lng: number, deviceId: string, accuracy?: number) =>
    request('/geo/spoof-check', { method: 'POST', body: JSON.stringify({ lat, lng, device_id: deviceId, accuracy }) }),

  // Webhooks
  getWebhooks: () => request('/api/v1/webhooks'),
  createWebhook: (url: string, events: string[], secret?: string) =>
    request('/api/v1/webhooks', { method: 'POST', body: JSON.stringify({ url, events, secret }) }),
  deleteWebhook: (id: number) => request(`/api/v1/webhooks/${id}`, { method: 'DELETE' }),

  // Exports
  exportResults: (electionId: number, format?: string) =>
    request(`/export/results?election_id=${electionId}${format ? `&format=${format}` : ''}`),
  exportVoters: (stateCode?: string) =>
    request(`/export/voters${stateCode ? `?state_code=${stateCode}` : ''}`),
  exportCollation: (electionId: number, level?: string) =>
    request(`/export/collation?election_id=${electionId}${level ? `&level=${level}` : ''}`),
  exportAudit: (startDate?: string, endDate?: string) =>
    request(`/export/audit${startDate ? `?start=${startDate}` : ''}${endDate ? `&end=${endDate}` : ''}`),

  // Duplicate Voter Detection
  scanDuplicateVoters: (stateCode?: string, modality?: string) =>
    request('/voters/duplicates/scan', { method: 'POST', body: JSON.stringify({ state_code: stateCode, modality: modality || 'fingerprint' }) }),
  resolveDuplicateVoter: (sourceVin: string, candidateVin: string, decision: string) =>
    request('/voters/duplicates/resolve', { method: 'POST', body: JSON.stringify({ voter_a_vin: sourceVin, voter_b_vin: candidateVin, decision }) }),

  // Admin User Management
  promoteUser: (userId: number, role: string) =>
    request('/admin/users/promote', { method: 'POST', body: JSON.stringify({ user_id: userId, role }) }),

  // GNN
  getGNNScore: (electionId: number) => request(`/ai/gnn/score?election_id=${electionId}`),

  // Lakehouse Pipeline
  getLakehouseStatus: () => request('/ai/lakehouse/status'),
  triggerLakehouseIngest: (electionId: number) =>
    request(`/ai/lakehouse/ingest?election_id=${electionId}`, { method: 'POST' }),
  triggerLakehousePipeline: (electionId: number) =>
    request(`/ai/lakehouse/pipeline?election_id=${electionId}`, { method: 'POST' }),

  // Ray Distributed Compute
  rayBatchPredict: (electionId: number) =>
    request(`/ai/ray/batch-predict?election_id=${electionId}`, { method: 'POST' }),
  rayTrain: () =>
    request('/ai/ray/train', { method: 'POST' }),

  // Continuous Training
  getTrainingStatus: () => request('/ai/training/status'),
  checkDrift: () => request('/ai/training/check-drift'),
  triggerRetrain: (useRay: boolean) =>
    request(`/ai/training/retrain?use_ray=${useRay}`, { method: 'POST' }),
  getModelRegistry: () => request('/ai/registry/models'),

  // Document AI
  analyzeDocument: (reportId: number) =>
    request(`/document-ai/analyze?report_id=${reportId}`, { method: 'POST' }),
  getDocumentAnalysisStatus: (reportId?: number) =>
    request(`/document-ai/status${reportId ? `?report_id=${reportId}` : ''}`),

  // KYC (already exists in pages but not in api object)
  kycVerify: (form: FormData) =>
    request('/kyc/verify', { method: 'POST', body: form as unknown as string }),
  kycLiveness: (form: FormData) =>
    request('/kyc/liveness', { method: 'POST', body: form as unknown as string }),
  kycStatus: (userId?: number) =>
    request(`/kyc/status${userId ? `?user_id=${userId}` : ''}`),

  // Mojaloop
  getMojaStatus: () => request('/middleware/mojaloop/status'),
  getMojaTransactions: () => request('/middleware/mojaloop/transactions'),
  mojaPartyLookup: (partyId: string) => request(`/middleware/mojaloop/parties?party_id=${partyId}`),
  mojaCreateQuote: (payerFsp: string, payeeFsp: string, amount: number, currency?: string) =>
    request('/middleware/mojaloop/quotes', { method: 'POST', body: JSON.stringify({ payer_fsp: payerFsp, payee_fsp: payeeFsp, amount, currency }) }),

  // OpenSearch
  getOpenSearchStatus: () => request('/middleware/opensearch/status'),
  getOpenSearchIndices: () => request('/middleware/opensearch/indices'),
  getOpenSearchStats: () => request('/middleware/opensearch/stats'),
  openSearchQuery: (query: string, index?: string) =>
    request(`/middleware/opensearch/search?q=${encodeURIComponent(query)}${index ? `&index=${index}` : ''}`),

  // WAF / OpenAppSec
  getWAFStatus: () => request('/middleware/waf/status'),
  getWAFStats: () => request('/middleware/waf/stats'),
  getWAFThreats: () => request('/middleware/waf/threats'),
  getWAFBlocklist: () => request('/middleware/waf/blocklist'),
  addWAFBlock: (ip: string, reason?: string) =>
    request('/middleware/waf/blocklist', { method: 'POST', body: JSON.stringify({ ip, reason }) }),

  // Pgpool Config
  getPgpoolConfig: () => request('/pgpool/config'),
  getPgpoolQueryCache: () => request('/pgpool/cache'),

  // OIDC
  getOIDCConfig: () => request('/.well-known/openid-configuration'),

  // Command Center (#1)
  getCommandCenterLive: () => request('/command-center/live'),
  getCommandCenterAlerts: () => request('/command-center/alerts'),
  getEscalationConfig: () => request('/escalation/config'),
  updateEscalationConfig: (rules: unknown[]) =>
    request('/escalation/config', { method: 'POST', body: JSON.stringify(rules) }),
  getLoadShedding: () => request('/load-shedding'),
  setLoadShedding: (level: number) =>
    request('/load-shedding', { method: 'POST', body: JSON.stringify({ level }) }),

  // MFA (#3)
  setupTOTP: () => request('/auth/mfa/totp/setup', { method: 'POST' }),
  verifyTOTP: (code: string) =>
    request('/auth/mfa/totp/verify', { method: 'POST', body: JSON.stringify({ code }) }),
  mfaChallenge: (user_id: number, code: string, method: string) =>
    request('/auth/mfa/challenge', { method: 'POST', body: JSON.stringify({ user_id, code, method }) }),
  sendMFASMS: (user_id: number) =>
    request('/auth/mfa/sms/send', { method: 'POST', body: JSON.stringify({ user_id }) }),
  getMFAStatus: () => request('/auth/mfa/status'),
  registerWebAuthn: (credential_id: string, public_key: string, device_name: string) =>
    request('/auth/mfa/webauthn/register', { method: 'POST', body: JSON.stringify({ credential_id, public_key, device_name }) }),

  // Citizen Portal (#6)
  citizenVerify: (params: { pu_code?: string; state?: string; lga?: string }) => {
    const qs = new URLSearchParams(params as Record<string, string>).toString();
    return request(`/citizen/verify?${qs}`);
  },
  citizenVerifySignature: (result_id: number) =>
    request(`/citizen/verify/signature?result_id=${result_id}`),
  signResult: (result_id: number, officer_pubkey: string) =>
    request('/results/sign', { method: 'POST', body: JSON.stringify({ result_id, officer_pubkey }) }),
  getResultQR: (result_id: number) => request(`/results/qr?result_id=${result_id}`),

  // Media API (#23) + PDF (#8) + OpenAPI (#11)
  getMediaWidget: (type?: string) => request(`/media/widget${type ? `?type=${type}` : ''}`),
  exportPDFReport: (type: string, state?: string) =>
    request(`/export/report/pdf?type=${type}${state ? `&state=${state}` : ''}`),
  getOpenAPIDocs: () => request('/openapi.json'),

  // Geo-fenced Submissions (#12)
  geoSubmissionCheck: (data: { result_id: number; officer_lat: number; officer_lng: number; pu_code: string }) =>
    request('/geo/submission/check', { method: 'POST', body: JSON.stringify(data) }),
  geoSubmissionOverride: (submission_id: number, reason: string) =>
    request('/geo/submission/override', { method: 'POST', body: JSON.stringify({ submission_id, reason }) }),

  // Anomaly Escalation (#7)
  getAnomalyEscalations: () => request('/anomaly/escalation'),
  createAnomalyEscalation: (data: { anomaly_id: string; severity: string; state_code: string; pu_code: string }) =>
    request('/anomaly/escalation', { method: 'POST', body: JSON.stringify(data) }),

  // Biometric Quality (#9)
  biometricQualityCheck: (data: { capture_id: string; modality: string; blur_score: number; exposure: number; angle: number }) =>
    request('/biometric/quality-check', { method: 'POST', body: JSON.stringify(data) }),

  // Predictive Analytics (#16)
  getPredictiveAnalytics: (election_id?: string) =>
    request(`/predictive/analytics${election_id ? `?election_id=${election_id}` : ''}`),

  // Multi-Election (#17)
  getElectionTemplates: () => request('/election/templates'),
  createElectionTemplate: (data: { election_type: string; template_name: string; party_count: number }) =>
    request('/election/templates', { method: 'POST', body: JSON.stringify(data) }),
  getElectionArchives: () => request('/election/archive'),
  archiveElection: (election_id: number) =>
    request('/election/archive', { method: 'POST', body: JSON.stringify({ election_id }) }),

  // Data Sovereignty (#20)
  getDataClassifications: () => request('/data/classification'),
  setDataClassification: (data: { table_name: string; column_name: string; classification: string }) =>
    request('/data/classification', { method: 'POST', body: JSON.stringify(data) }),
  requestDataErasure: (vin: string, reason: string) =>
    request('/data/erasure', { method: 'POST', body: JSON.stringify({ vin, reason }) }),

  // Observer Photo Verification (#18)
  observerPhotoVerify: (data: { observer_id: number; pu_code: string; photo_hash: string; gps_lat: number; gps_lng: number }) =>
    request('/observer/photo-verify', { method: 'POST', body: JSON.stringify(data) }),

  // Offline Conflict Resolution (#2)
  resolveOfflineConflict: (data: { result_id: number; local_data: unknown; server_data: unknown; strategy: string }) =>
    request('/offline/conflict/resolve', { method: 'POST', body: JSON.stringify(data) }),

  // Voice IVR (#14)
  ivrVerify: (phone_number: string, pu_code: string, language?: string) =>
    request('/ivr/verify', { method: 'POST', body: JSON.stringify({ phone_number, pu_code, language }) }),

  // User Management CRUD
  getUsers: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return request(`/users${qs}`);
  },
  getUser: (id: number) => request(`/users/${id}`),
  createUser: (data: { username: string; full_name: string; password: string; role: string; staff_id?: string; state_code?: string }) =>
    request('/users', { method: 'POST', body: JSON.stringify(data) }),
  updateUser: (id: number, data: { full_name?: string; role?: string; staff_id?: string; state_code?: string }) =>
    request(`/users/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  deleteUser: (id: number) => request(`/users/${id}`, { method: 'DELETE' }),

  // Election Delete
  deleteElection: (id: number) => request(`/elections/${id}`, { method: 'DELETE' }),

  // Incident Update
  updateIncident: (id: number, status: string) =>
    request(`/incidents/${id}`, { method: 'PATCH', body: JSON.stringify({ status }) }),

  // Stakeholder Create
  createStakeholder: (data: { org_name: string; type: string; contact_person?: string; email?: string; phone?: string; state_code?: string }) =>
    request('/stakeholders', { method: 'POST', body: JSON.stringify(data) }),

  // Grievance Create
  createGrievance: (data: { category: string; description: string; election_id?: number; polling_unit_code?: string }) =>
    request('/stakeholders/grievances', { method: 'POST', body: JSON.stringify(data) }),

  // Webhook Update
  updateWebhook: (id: number, data: { url?: string; events?: string[]; active?: boolean }) =>
    request(`/api/v1/webhooks/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),

  // Training Course Create
  createCourse: (data: { title: string; course_type: string; target_role?: string; duration_hours?: number; is_mandatory?: boolean }) =>
    request('/training/courses', { method: 'POST', body: JSON.stringify(data) }),

  // Platform Improvements v2
  getOpenAPISpec: () => request('/api/openapi.json'),
  getGeoIPCheck: (data: { gps_lat: number; gps_lng: number; device_id: string }) =>
    request('/security/geo-ip-check', { method: 'POST', body: JSON.stringify(data) }),
  getDLPExport: (type: string, format: string) =>
    request(`/security/dlp-export?type=${type}&format=${format}`, { method: 'POST' }),
  presenceHeartbeat: (page: string) =>
    request('/presence/heartbeat', { method: 'POST', body: JSON.stringify({ page }) }),
  getPresenceList: (page: string) => request(`/presence/list?page=${page}`),
  batchImportUsers: (users: Array<{ username: string; email: string; role: string; state_code?: string }>) =>
    request('/admin/batch/users', { method: 'POST', body: JSON.stringify({ users }) }),
  batchStatusUpdate: (entity: string, ids: number[], status: string) =>
    request('/admin/batch/status', { method: 'POST', body: JSON.stringify({ entity, ids, status }) }),
  getIntegrityScore: (puCode: string, electionId?: number) =>
    request(`/ai/integrity-score?pu_code=${puCode}&election_id=${electionId || 1}`),
  getIntegrityHeatmap: (electionId?: number, stateCode?: string) => {
    const params = new URLSearchParams({ election_id: String(electionId || 1) });
    if (stateCode) params.set('state_code', stateCode);
    return request(`/ai/integrity-heatmap?${params}`);
  },
  getResultCertificate: (puCode: string, electionId?: number) =>
    request(`/public/result-certificate?pu_code=${puCode}&election_id=${electionId || 1}`),
  getTVDashboard: (electionId?: number) =>
    request(`/public/tv-dashboard?election_id=${electionId || 1}`),
  getComplianceReport: (standard: string, electionId?: number) =>
    request(`/reports/compliance?standard=${standard}&election_id=${electionId || 1}`),
  getAuditTimeline: (params?: { user_id?: string; pu_code?: string; limit?: number }) => {
    const p = new URLSearchParams();
    if (params?.user_id) p.set('user_id', params.user_id);
    if (params?.pu_code) p.set('pu_code', params.pu_code);
    if (params?.limit) p.set('limit', String(params.limit));
    return request(`/audit/timeline?${p}`);
  },
  transcribeVoice: (audioBlob: Blob) =>
    request('/voice/transcribe', { method: 'POST', body: audioBlob, headers: { 'Content-Type': 'audio/wav' } }),

  // Enhanced Geospatial
  getNearbyPUs: (lat: number, lng: number, radius?: number, limit?: number) => {
    const p = new URLSearchParams({ lat: String(lat), lng: String(lng) });
    if (radius) p.set('radius', String(radius));
    if (limit) p.set('limit', String(limit));
    return request(`/geo/nearby-pus?${p}`);
  },
  getLandmarks: (params?: { lat?: number; lng?: number; radius?: number; category?: string; state_code?: string }) => {
    const p = new URLSearchParams();
    if (params?.lat) p.set('lat', String(params.lat));
    if (params?.lng) p.set('lng', String(params.lng));
    if (params?.radius) p.set('radius', String(params.radius));
    if (params?.category) p.set('category', params.category);
    if (params?.state_code) p.set('state_code', params.state_code);
    return request(`/geo/landmarks?${p}`);
  },
  createLandmark: (data: { name: string; category: string; latitude: number; longitude: number; state_code?: string; address?: string }) =>
    request('/geo/landmarks', { method: 'POST', body: JSON.stringify(data) }),
  seedLandmarks: () => request('/geo/landmarks/seed', { method: 'POST' }),
  getGeoHeatmap: (electionId: number, metric?: string) => {
    const p = new URLSearchParams({ election_id: String(electionId) });
    if (metric) p.set('metric', metric);
    return request(`/geo/heatmap?${p}`);
  },
  getGeoClusters: (electionId: number, zoom: number, stateCode?: string) => {
    const p = new URLSearchParams({ election_id: String(electionId), zoom: String(zoom) });
    if (stateCode) p.set('state_code', stateCode);
    return request(`/geo/clusters?${p}`);
  },
  getStreetView: (lat: number, lng: number) =>
    request(`/geo/street-view?lat=${lat}&lng=${lng}`),
  getGeoBoundary: (stateCode?: string, lgaCode?: string) => {
    const p = new URLSearchParams();
    if (stateCode) p.set('state_code', stateCode);
    if (lgaCode) p.set('lga_code', lgaCode);
    return request(`/geo/boundary?${p}`);
  },
  getGeoSpatialStats: (electionId?: number, stateCode?: string) => {
    const p = new URLSearchParams({ election_id: String(electionId || 1) });
    if (stateCode) p.set('state_code', stateCode);
    return request(`/geo/spatial-stats?${p}`);
  },
  getSedonaAnalysis: (type?: string, electionId?: number) => {
    const p = new URLSearchParams();
    if (type) p.set('type', type);
    if (electionId) p.set('election_id', String(electionId));
    return request(`/geo/sedona/analysis?${p}`);
  },
  // Real-time tracking & crowd density
  getOfficialLocations: (params?: { state_code?: string; role?: string; active_minutes?: number }) => {
    const p = new URLSearchParams();
    if (params?.state_code) p.set('state_code', params.state_code);
    if (params?.role) p.set('role', params.role);
    if (params?.active_minutes) p.set('active_minutes', String(params.active_minutes));
    return request(`/geo/tracking/officials?${p}`);
  },
  updateOfficialLocation: (data: { lat: number; lng: number; staff_id: string; role?: string; pu_code?: string; activity?: string; battery_pct?: number }) =>
    request('/geo/tracking/update', { method: 'POST', body: JSON.stringify(data) }),
  getCrowdDensity: (params?: { state_code?: string; recent_minutes?: number }) => {
    const p = new URLSearchParams();
    if (params?.state_code) p.set('state_code', params.state_code);
    if (params?.recent_minutes) p.set('recent_minutes', String(params.recent_minutes));
    return request(`/geo/crowd/density?${p}`);
  },
  reportCrowdDensity: (data: { pu_code: string; lat: number; lng: number; head_count: number; density_level?: string; queue_length?: number; wait_time_min?: number }) =>
    request('/geo/crowd/report', { method: 'POST', body: JSON.stringify(data) }),
  seedTrackingData: () => request('/geo/tracking/seed', { method: 'POST' }),
  // Advanced Geo (#2-#30)
  getTrackingReplay: (staffId?: string, hours?: number) => {
    const p = new URLSearchParams();
    if (staffId) p.set('staff_id', staffId);
    if (hours) p.set('hours', String(hours));
    return request(`/geo/tracking/replay?${p}`);
  },
  getGeofenceZones: (stateCode?: string) =>
    request(`/geo/geofence/zones${stateCode ? `?state_code=${stateCode}` : ''}`),
  getGeofenceViolations: () => request('/geo/geofence/violations'),
  seedGeofenceZones: () => request('/geo/geofence/zones/seed', { method: 'POST' }),
  getSpatialClusters: (electionId?: number, epsKm?: number) => {
    const p = new URLSearchParams({ election_id: String(electionId || 1) });
    if (epsKm) p.set('eps_km', String(epsKm));
    return request(`/geo/spatial/clusters?${p}`);
  },
  getVoronoiDiagram: (stateCode?: string) =>
    request(`/geo/spatial/voronoi${stateCode ? `?state_code=${stateCode}` : ''}`),
  getCrowdAlerts: (severity?: string) =>
    request(`/geo/crowd/alerts${severity ? `?severity=${severity}` : ''}`),
  acknowledgeCrowdAlert: (alertId: number, userId: string) =>
    request('/geo/crowd/alerts/ack', { method: 'POST', body: JSON.stringify({ alert_id: alertId, user_id: userId }) }),
  getRouteOptimize: (originLat: number, originLng: number, destLat: number, destLng: number, profile?: string) =>
    request('/geo/route', { method: 'POST', body: JSON.stringify({ origin_lat: originLat, origin_lng: originLng, dest_lat: destLat, dest_lng: destLng, profile: profile || 'driving' }) }),
  getNearestOfficial: (lat: number, lng: number, role?: string) =>
    request(`/geo/nearest-official?lat=${lat}&lng=${lng}${role ? `&role=${role}` : ''}`),
  getWeatherOverlay: (lat?: number, lng?: number) =>
    request(`/geo/weather${lat ? `?lat=${lat}&lng=${lng}` : ''}`),
  uploadPUPhoto: (formData: FormData) =>
    request('/geo/photos/upload', { method: 'POST', body: formData, headers: {} }),
  getPUPhotos: (puCode?: string) =>
    request(`/geo/photos${puCode ? `?pu_code=${puCode}` : ''}`),
  getIncidentHotspots: (hours?: number, severity?: string) => {
    const p = new URLSearchParams();
    if (hours) p.set('hours', String(hours));
    if (severity) p.set('severity', severity);
    return request(`/geo/incidents/hotspots?${p}`);
  },
  getPredictiveCrowdFlow: () => request('/geo/crowd/predictions'),
  getDronePositions: () => request('/geo/drones'),
  getSimulation: (scenario?: string) =>
    request(`/geo/simulation${scenario ? `?scenario=${scenario}` : ''}`),
  getGeofenceAttestation: (data: { staff_id: string; pu_code: string; lat: number; lng: number }) =>
    request('/geo/geofence/attest', { method: 'POST', body: JSON.stringify(data) }),
  getMeshNetworkStatus: () => request('/geo/mesh/status'),
  getH3HexGrid: (resolution?: number, electionId?: number) => {
    const p = new URLSearchParams();
    if (resolution) p.set('resolution', String(resolution));
    if (electionId) p.set('election_id', String(electionId));
    return request(`/geo/h3/grid?${p}`);
  },
};
