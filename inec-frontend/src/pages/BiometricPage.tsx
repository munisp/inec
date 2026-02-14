import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Fingerprint, Eye, ScanFace, AlertTriangle, ShieldCheck, Activity, Lock, Database, Cpu, Scan, KeyRound, HardDrive } from 'lucide-react';

export default function BiometricPage() {
  const [stats, setStats] = useState<any>(null);
  const [engineStats, setEngineStats] = useState<any>(null);
  const [profiles, setProfiles] = useState<any>(null);
  const [, setDuplicates] = useState<any>(null);
  const [padHistory, setPadHistory] = useState<any>(null);
  const [dedupJobs, setDedupJobs] = useState<any>(null);
  const [vaultStats, setVaultStats] = useState<any>(null);
  const [deviceCaps, setDeviceCaps] = useState<any>(null);
  const [pipeline, setPipeline] = useState<any>(null);
  const [abisConfig, setAbisConfig] = useState<any>(null);
  const [captureSessions, setCaptureSessions] = useState<any>(null);
  const [tab, setTab] = useState('engine');

  useEffect(() => {
    api.getBiometricStats().then(setStats).catch(() => {});
    api.getBiometricEngineStats().then(setEngineStats).catch(() => {});
    api.getBiometricProfiles(50, 0).then(setProfiles).catch(() => {});
    api.getABISDuplicates().then(setDuplicates).catch(() => {});
    api.getPADHistory().then(setPadHistory).catch(() => {});
    api.getDedupJobs().then(setDedupJobs).catch(() => {});
    api.getVaultStats().then(setVaultStats).catch(() => {});
    api.getBVASDeviceCapabilities().then(setDeviceCaps).catch(() => {});
    api.getABISPipeline().then(setPipeline).catch(() => {});
    api.getABISConfig().then(setAbisConfig).catch(() => {});
    api.getBVASCaptureSessions().then(setCaptureSessions).catch(() => {});
  }, []);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-bold">Enhanced Biometric Verification System</h2>
        <p className="text-zinc-500 text-sm">Production-grade ABIS with real template matching, PAD, encrypted vault, 1:N dedup, and device SDK</p>
      </div>

      {engineStats && (
        <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-6 gap-3">
          {[
            { label: 'Encrypted Templates', value: engineStats.templates?.total?.toLocaleString(), icon: Lock, color: 'text-blue-600' },
            { label: 'Fingerprint', value: engineStats.templates?.fingerprint, icon: Fingerprint, color: 'text-indigo-600' },
            { label: 'Facial', value: engineStats.templates?.facial, icon: ScanFace, color: 'text-purple-600' },
            { label: 'Iris', value: engineStats.templates?.iris, icon: Eye, color: 'text-teal-600' },
            { label: 'Vault Keys', value: engineStats.vault?.active_keys, icon: KeyRound, color: 'text-amber-600' },
            { label: 'PAD Checks', value: engineStats.pad?.total_checks, icon: ShieldCheck, color: 'text-green-600' },
            { label: 'Spoofs Caught', value: engineStats.pad?.spoof_detected, icon: AlertTriangle, color: 'text-red-600' },
            { label: 'Dedup Jobs', value: engineStats.deduplication?.completed, icon: Database, color: 'text-cyan-600' },
            { label: 'Devices', value: engineStats.devices?.registered, icon: HardDrive, color: 'text-orange-600' },
            { label: 'Capture Sessions', value: engineStats.devices?.processed, icon: Scan, color: 'text-pink-600' },
            { label: 'Vault Ops', value: engineStats.vault?.total_operations, icon: Activity, color: 'text-violet-600' },
            { label: 'Avg Quality', value: engineStats.templates?.avg_quality?.toFixed(2), icon: Cpu, color: 'text-emerald-600' },
          ].map((s, i) => (
            <Card key={i}>
              <CardContent className="pt-3 pb-2">
                <div className="flex items-center gap-1.5 mb-1">
                  <s.icon className={`w-3.5 h-3.5 ${s.color}`} />
                  <span className="text-[10px] text-zinc-500">{s.label}</span>
                </div>
                <p className="text-lg font-bold">{s.value ?? '-'}</p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className="flex-wrap h-auto gap-1">
          <TabsTrigger value="engine">ABIS Engine</TabsTrigger>
          <TabsTrigger value="pad">PAD / Liveness</TabsTrigger>
          <TabsTrigger value="dedup">Deduplication</TabsTrigger>
          <TabsTrigger value="vault">Encrypted Vault</TabsTrigger>
          <TabsTrigger value="devices">Device SDK</TabsTrigger>
          <TabsTrigger value="pipeline">Enrollment Pipeline</TabsTrigger>
          <TabsTrigger value="profiles">Profiles</TabsTrigger>
          <TabsTrigger value="config">ABIS Config</TabsTrigger>
        </TabsList>

        <TabsContent value="engine">
          <div className="grid md:grid-cols-2 gap-4">
            <Card>
              <CardHeader><CardTitle className="text-sm">Template Matching Algorithms</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {[
                    { mod: 'Fingerprint', algo: 'Minutiae Matching', format: 'ISO 19794-2', detail: 'Ridge endings, bifurcations, core/delta points' },
                    { mod: 'Facial', algo: 'Cosine Similarity 128D', format: 'ISO 19794-5', detail: '128-dim normalized embedding vectors' },
                    { mod: 'Iris', algo: 'Hamming Distance 2048-bit', format: 'ISO 19794-6', detail: 'IrisCode with mask-based XOR comparison' },
                  ].map((a, i) => (
                    <div key={i} className="p-3 bg-zinc-50 rounded-lg">
                      <div className="flex items-center justify-between mb-1">
                        <span className="font-semibold text-sm">{a.mod}</span>
                        <Badge variant="outline" className="text-[10px]">{a.format}</Badge>
                      </div>
                      <p className="text-xs text-zinc-600">{a.algo}</p>
                      <p className="text-[10px] text-zinc-400 mt-0.5">{a.detail}</p>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">Multi-Modal Fusion</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-3">
                  <div className="p-3 bg-zinc-50 rounded-lg">
                    <p className="text-sm font-semibold mb-2">Weighted Sum Fusion</p>
                    <div className="space-y-2">
                      {[
                        { mod: 'Fingerprint', weight: 0.45, color: 'bg-blue-500' },
                        { mod: 'Facial', weight: 0.30, color: 'bg-purple-500' },
                        { mod: 'Iris', weight: 0.25, color: 'bg-teal-500' },
                      ].map((w, i) => (
                        <div key={i}>
                          <div className="flex justify-between text-xs mb-0.5">
                            <span>{w.mod}</span><span className="font-mono">{(w.weight * 100).toFixed(0)}%</span>
                          </div>
                          <div className="w-full h-2 bg-zinc-200 rounded-full">
                            <div className={`h-full rounded-full ${w.color}`} style={{ width: `${w.weight * 100}%` }} />
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                  {engineStats?.abis && (
                    <div className="p-3 bg-zinc-50 rounded-lg">
                      <p className="text-sm font-semibold mb-2">FAR/FRR Thresholds</p>
                      <div className="space-y-1 text-xs">
                        <div className="flex justify-between"><span>Fingerprint FAR</span><span className="font-mono">{engineStats.abis.far_fingerprint}</span></div>
                        <div className="flex justify-between"><span>Fingerprint FRR</span><span className="font-mono">{engineStats.abis.frr_fingerprint}</span></div>
                        <div className="flex justify-between"><span>Facial FAR</span><span className="font-mono">{engineStats.abis.far_facial}</span></div>
                        <div className="flex justify-between"><span>Iris FAR</span><span className="font-mono">{engineStats.abis.far_iris}</span></div>
                        <div className="flex justify-between"><span>Fusion Threshold</span><span className="font-mono font-bold">{engineStats.abis.fusion_threshold}</span></div>
                      </div>
                    </div>
                  )}
                  {engineStats?.abis?.iso_compliance && (
                    <div className="flex flex-wrap gap-1">
                      {engineStats.abis.iso_compliance.map((iso: string) => (
                        <Badge key={iso} variant="outline" className="text-[10px]">{iso}</Badge>
                      ))}
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
            {stats && (
              <Card>
                <CardHeader><CardTitle className="text-sm">Verification Summary</CardTitle></CardHeader>
                <CardContent>
                  <div className="space-y-2 text-sm">
                    <div className="flex justify-between"><span className="text-zinc-500">Total Verifications</span><span className="font-semibold">{stats.total_verifications?.toLocaleString()}</span></div>
                    <div className="flex justify-between"><span className="text-zinc-500">Matches</span><span className="font-semibold text-green-600">{stats.matches?.toLocaleString()}</span></div>
                    <div className="flex justify-between"><span className="text-zinc-500">No Matches</span><span className="font-semibold text-red-600">{stats.no_matches?.toLocaleString()}</span></div>
                    <div className="flex justify-between"><span className="text-zinc-500">Match Rate</span><span className="font-semibold">{stats.match_rate?.toFixed(1)}%</span></div>
                    <div className="flex justify-between"><span className="text-zinc-500">Avg Latency</span><span className="font-semibold">{stats.avg_latency_ms?.toFixed(0)}ms</span></div>
                  </div>
                </CardContent>
              </Card>
            )}
            {stats && (
              <Card>
                <CardHeader><CardTitle className="text-sm">By Modality</CardTitle></CardHeader>
                <CardContent>
                  <div className="space-y-3">
                    {stats.by_modality?.map((m: any, i: number) => (
                      <div key={i} className="flex items-center justify-between">
                        <div className="flex items-center gap-2">
                          {m.modality === 'fingerprint' && <Fingerprint className="w-4 h-4 text-blue-600" />}
                          {m.modality === 'facial' && <ScanFace className="w-4 h-4 text-purple-600" />}
                          {m.modality === 'multi_modal' && <Eye className="w-4 h-4 text-green-600" />}
                          <span className="text-sm capitalize">{m.modality.replace('_', ' ')}</span>
                        </div>
                        <div className="text-right">
                          <span className="font-semibold">{m.count}</span>
                          <span className="text-xs text-zinc-500 ml-2">avg {m.avg_score?.toFixed(2)}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            )}
          </div>
        </TabsContent>

        <TabsContent value="pad">
          <div className="space-y-4">
            <Card>
              <CardHeader>
                <CardTitle className="text-sm flex items-center justify-between">
                  Presentation Attack Detection (PAD) - ISO 30107 Level 2
                  <div className="flex gap-2 text-xs font-normal">
                    <Badge className="bg-green-100 text-green-700">Live: {padHistory?.results?.filter((p: any) => p.decision === 'live').length || 0}</Badge>
                    <Badge variant="destructive">Spoof: {padHistory?.results?.filter((p: any) => p.decision === 'spoof').length || 0}</Badge>
                  </div>
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid md:grid-cols-4 gap-3 mb-4">
                  {['Texture (LBP)', 'Motion Analysis', 'Depth Estimation', 'Spectral Analysis'].map((algo, i) => (
                    <div key={i} className="p-2 bg-zinc-50 rounded text-center">
                      <p className="text-xs font-semibold">{algo}</p>
                      <p className="text-[10px] text-zinc-500 mt-0.5">{['Local Binary Patterns for fingerprint surface', 'Blink/head movement for facial', '3D depth map verification', 'NIR spectral response for iris'][i]}</p>
                    </div>
                  ))}
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-xs">
                    <thead><tr className="border-b text-left text-zinc-500">
                      <th className="pb-2 pr-3">VIN</th><th className="pb-2 pr-3">Modality</th><th className="pb-2 pr-3">Liveness</th>
                      <th className="pb-2 pr-3">Texture</th><th className="pb-2 pr-3">Motion</th><th className="pb-2 pr-3">Depth</th>
                      <th className="pb-2 pr-3">Spectral</th><th className="pb-2 pr-3">Decision</th><th className="pb-2">Attack</th>
                    </tr></thead>
                    <tbody>
                      {padHistory?.results?.slice(0, 20).map((p: any, i: number) => (
                        <tr key={i} className="border-b border-zinc-100">
                          <td className="py-1.5 pr-3 font-mono">{p.voter_vin?.slice(0, 12)}...</td>
                          <td className="py-1.5 pr-3 capitalize">{p.modality}</td>
                          <td className="py-1.5 pr-3">{p.liveness_score?.toFixed(3)}</td>
                          <td className="py-1.5 pr-3">{p.texture_score?.toFixed(3)}</td>
                          <td className="py-1.5 pr-3">{p.motion_score?.toFixed(3)}</td>
                          <td className="py-1.5 pr-3">{p.depth_score?.toFixed(3)}</td>
                          <td className="py-1.5 pr-3">{p.spectral_score?.toFixed(3)}</td>
                          <td className="py-1.5 pr-3">
                            <Badge variant={p.decision === 'live' ? 'default' : 'destructive'} className="text-[10px]">
                              {p.decision}
                            </Badge>
                          </td>
                          <td className="py-1.5">{p.attack_type || '-'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="dedup">
          <div className="space-y-4">
            <Card>
              <CardHeader>
                <CardTitle className="text-sm flex items-center justify-between">
                  1:N Deduplication Pipeline
                  <Badge variant="outline" className="text-[10px]">LSH Blocking Strategy</Badge>
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full text-xs">
                    <thead><tr className="border-b text-left text-zinc-500">
                      <th className="pb-2 pr-3">ID</th><th className="pb-2 pr-3">Type</th><th className="pb-2 pr-3">Status</th>
                      <th className="pb-2 pr-3">Comparisons</th><th className="pb-2 pr-3">Dups Found</th><th className="pb-2 pr-3">Progress</th>
                      <th className="pb-2 pr-3">Modalities</th><th className="pb-2 pr-3">Threshold</th><th className="pb-2">Blocking</th>
                    </tr></thead>
                    <tbody>
                      {dedupJobs?.jobs?.map((j: any) => (
                        <tr key={j.id} className="border-b border-zinc-100">
                          <td className="py-1.5 pr-3">{j.id}</td>
                          <td className="py-1.5 pr-3 capitalize">{j.type}</td>
                          <td className="py-1.5 pr-3">
                            <Badge variant={j.status === 'completed' ? 'default' : j.status === 'running' ? 'outline' : 'secondary'} className="text-[10px]">
                              {j.status}
                            </Badge>
                          </td>
                          <td className="py-1.5 pr-3">{j.total_comparisons}</td>
                          <td className="py-1.5 pr-3 font-semibold text-red-600">{j.duplicates_found}</td>
                          <td className="py-1.5 pr-3">
                            <div className="flex items-center gap-1">
                              <div className="w-16 h-1.5 bg-zinc-200 rounded-full">
                                <div className="h-full bg-blue-500 rounded-full" style={{ width: `${j.progress}%` }} />
                              </div>
                              <span>{j.progress?.toFixed(0)}%</span>
                            </div>
                          </td>
                          <td className="py-1.5 pr-3">{j.modalities}</td>
                          <td className="py-1.5 pr-3">{j.threshold}</td>
                          <td className="py-1.5">{j.blocking_strategy}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <div className="mt-3 p-3 bg-zinc-50 rounded-lg">
                  <p className="text-xs font-semibold mb-1">Multi-Modal Score Fusion</p>
                  <p className="text-[10px] text-zinc-500">Coarse-to-fine matching: LSH blocking reduces candidate set, then per-modality scoring with weighted sum fusion (FP: 0.45, Face: 0.30, Iris: 0.25) produces final decision.</p>
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="vault">
          <div className="grid md:grid-cols-2 gap-4">
            <Card>
              <CardHeader><CardTitle className="text-sm">Encrypted Biometric Vault</CardTitle></CardHeader>
              <CardContent>
                {vaultStats && (
                  <div className="space-y-3">
                    <div className="p-3 bg-zinc-50 rounded-lg">
                      <p className="text-xs font-semibold mb-2">Encryption</p>
                      <div className="space-y-1 text-xs">
                        <div className="flex justify-between"><span className="text-zinc-500">Algorithm</span><span className="font-mono">{vaultStats.encryption?.algorithm}</span></div>
                        <div className="flex justify-between"><span className="text-zinc-500">Key Wrapping</span><span className="font-mono">{vaultStats.encryption?.key_wrapping}</span></div>
                        <div className="flex justify-between"><span className="text-zinc-500">Master Key</span><span className="font-mono">{vaultStats.encryption?.master_key}</span></div>
                      </div>
                    </div>
                    <div className="p-3 bg-zinc-50 rounded-lg">
                      <p className="text-xs font-semibold mb-2">Keys</p>
                      <div className="space-y-1 text-xs">
                        <div className="flex justify-between"><span className="text-zinc-500">Active</span><span className="font-semibold text-green-600">{vaultStats.keys?.active}</span></div>
                        <div className="flex justify-between"><span className="text-zinc-500">Rotated</span><span>{vaultStats.keys?.rotated}</span></div>
                        <div className="flex justify-between"><span className="text-zinc-500">Revoked</span><span>{vaultStats.keys?.revoked}</span></div>
                      </div>
                    </div>
                    <div className="p-3 bg-zinc-50 rounded-lg">
                      <p className="text-xs font-semibold mb-2">Compliance</p>
                      <div className="flex flex-wrap gap-1">
                        {vaultStats.compliance && Object.entries(vaultStats.compliance).map(([k, v]) => (
                          <Badge key={k} variant={v ? 'default' : 'destructive'} className="text-[10px]">
                            {k.replace(/_/g, ' ')}
                          </Badge>
                        ))}
                      </div>
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">Vault Audit Log</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-1.5 max-h-[400px] overflow-y-auto">
                  {vaultStats?.recent_operations?.map((op: any, i: number) => (
                    <div key={i} className="flex items-center gap-2 text-xs p-1.5 bg-zinc-50 rounded">
                      <Badge variant={op.success ? 'default' : 'destructive'} className="text-[9px] min-w-[80px] justify-center">
                        {op.operation}
                      </Badge>
                      {op.voter_vin && <span className="font-mono text-[10px] text-zinc-500">{op.voter_vin.slice(0, 10)}...</span>}
                      {op.modality && <span className="text-zinc-400">{op.modality}</span>}
                      <span className="ml-auto text-[10px] text-zinc-400">{op.timestamp?.split('T')[0]}</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="devices">
          <div className="space-y-4">
            <Card>
              <CardHeader><CardTitle className="text-sm">BVAS Device Capabilities & SDK</CardTitle></CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full text-xs">
                    <thead><tr className="border-b text-left text-zinc-500">
                      <th className="pb-2 pr-3">Device ID</th><th className="pb-2 pr-3">Firmware</th><th className="pb-2 pr-3">Modalities</th>
                      <th className="pb-2 pr-3">FP Sensor</th><th className="pb-2 pr-3">FAP Level</th><th className="pb-2 pr-3">Camera</th>
                      <th className="pb-2 pr-3">NFC</th><th className="pb-2 pr-3">Secure Element</th><th className="pb-2">TLS</th>
                    </tr></thead>
                    <tbody>
                      {deviceCaps?.devices?.map((d: any, i: number) => (
                        <tr key={i} className="border-b border-zinc-100">
                          <td className="py-1.5 pr-3 font-mono font-semibold">{d.device_id}</td>
                          <td className="py-1.5 pr-3">{d.firmware}</td>
                          <td className="py-1.5 pr-3">
                            <div className="flex gap-0.5">
                              {d.modalities?.map((m: string) => (
                                <Badge key={m} variant="outline" className="text-[9px]">{m}</Badge>
                              ))}
                            </div>
                          </td>
                          <td className="py-1.5 pr-3">{d.fingerprint_sensor}</td>
                          <td className="py-1.5 pr-3"><Badge className="text-[9px] bg-blue-100 text-blue-700">{d.fap_level}</Badge></td>
                          <td className="py-1.5 pr-3">{d.camera_resolution}</td>
                          <td className="py-1.5 pr-3">{d.nfc_capable ? <Badge className="text-[9px] bg-green-100 text-green-700">Yes</Badge> : '-'}</td>
                          <td className="py-1.5 pr-3">{d.secure_element || '-'}</td>
                          <td className="py-1.5">{d.tls_version}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">Capture Sessions</CardTitle></CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full text-xs">
                    <thead><tr className="border-b text-left text-zinc-500">
                      <th className="pb-2 pr-3">Session</th><th className="pb-2 pr-3">Device</th><th className="pb-2 pr-3">VIN</th>
                      <th className="pb-2 pr-3">Modality</th><th className="pb-2 pr-3">Quality</th><th className="pb-2 pr-3">NFIQ2</th>
                      <th className="pb-2 pr-3">Image</th><th className="pb-2 pr-3">Status</th><th className="pb-2">Time (ms)</th>
                    </tr></thead>
                    <tbody>
                      {captureSessions?.sessions?.slice(0, 15).map((s: any, i: number) => (
                        <tr key={i} className="border-b border-zinc-100">
                          <td className="py-1.5 pr-3 font-mono text-[10px]">{s.session_id?.slice(0, 16)}...</td>
                          <td className="py-1.5 pr-3">{s.device_id}</td>
                          <td className="py-1.5 pr-3 font-mono text-[10px]">{s.voter_vin?.slice(0, 10)}...</td>
                          <td className="py-1.5 pr-3 capitalize">{s.modality}</td>
                          <td className="py-1.5 pr-3">{s.quality?.toFixed(2)}</td>
                          <td className="py-1.5 pr-3">{s.nfiq2_score}</td>
                          <td className="py-1.5 pr-3">{s.image?.width}x{s.image?.height} @{s.image?.dpi}dpi</td>
                          <td className="py-1.5 pr-3">
                            <Badge variant={s.status === 'processed' ? 'default' : 'outline'} className="text-[10px]">{s.status}</Badge>
                          </td>
                          <td className="py-1.5">{s.processing_time_ms}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="pipeline">
          <Card>
            <CardHeader>
              <CardTitle className="text-sm flex items-center justify-between">
                ABIS Enrollment Pipeline
                {pipeline?.summary && (
                  <div className="flex gap-2 text-xs font-normal">
                    <Badge className="bg-green-100 text-green-700">Complete: {pipeline.summary.completed}</Badge>
                    <Badge variant="destructive">Failed: {pipeline.summary.failed}</Badge>
                    <Badge variant="outline">Rate: {pipeline.summary.success_rate?.toFixed(1)}%</Badge>
                  </div>
                )}
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-6 gap-2 mb-4">
                {['Capture', 'Quality Check', 'Template Extract', 'Dedup Check', 'Vault Store', 'Complete'].map((stage, i) => (
                  <div key={i} className="text-center p-2 bg-zinc-50 rounded relative">
                    <div className="w-6 h-6 mx-auto mb-1 rounded-full bg-blue-100 flex items-center justify-center text-xs font-bold text-blue-700">{i + 1}</div>
                    <p className="text-[10px] font-semibold">{stage}</p>
                    {i < 5 && <div className="absolute top-5 -right-1 text-zinc-300">&rarr;</div>}
                  </div>
                ))}
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-3">VIN</th><th className="pb-2 pr-3">Stage</th><th className="pb-2 pr-3">Modality</th>
                    <th className="pb-2 pr-3">Quality</th><th className="pb-2 pr-3">Template</th><th className="pb-2 pr-3">Dedup</th>
                    <th className="pb-2 pr-3">Vault</th><th className="pb-2 pr-3">FAR</th><th className="pb-2">FRR</th>
                  </tr></thead>
                  <tbody>
                    {pipeline?.pipeline_entries?.slice(0, 20).map((e: any, i: number) => (
                      <tr key={i} className="border-b border-zinc-100">
                        <td className="py-1.5 pr-3 font-mono text-[10px]">{e.voter_vin?.slice(0, 12)}...</td>
                        <td className="py-1.5 pr-3">
                          <Badge variant={e.stage === 'complete' ? 'default' : e.stage === 'failed' ? 'destructive' : 'outline'} className="text-[10px]">
                            {e.stage}
                          </Badge>
                        </td>
                        <td className="py-1.5 pr-3 capitalize">{e.modality}</td>
                        <td className="py-1.5 pr-3">{e.quality_passed ? <span className="text-green-600">Pass</span> : <span className="text-red-600">Fail</span>}</td>
                        <td className="py-1.5 pr-3">{e.template_extracted ? <span className="text-green-600">Yes</span> : '-'}</td>
                        <td className="py-1.5 pr-3">{e.dedup_cleared ? <span className="text-green-600">Clear</span> : '-'}</td>
                        <td className="py-1.5 pr-3">{e.vault_stored ? <Lock className="w-3 h-3 text-green-600 inline" /> : '-'}</td>
                        <td className="py-1.5 pr-3 font-mono">{e.far_threshold}</td>
                        <td className="py-1.5 font-mono">{e.frr_threshold}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="profiles">
          <Card>
            <CardContent className="pt-4">
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-4">VIN</th><th className="pb-2 pr-4">Modalities</th><th className="pb-2 pr-4">Quality</th><th className="pb-2 pr-4">Matches</th><th className="pb-2 pr-4">Status</th><th className="pb-2">Duplicate</th>
                  </tr></thead>
                  <tbody>
                    {profiles?.profiles?.map((p: any, i: number) => (
                      <tr key={i} className="border-b border-zinc-100">
                        <td className="py-2 pr-4 font-mono text-xs">{p.voter_vin}</td>
                        <td className="py-2 pr-4">
                          <div className="flex gap-1">
                            {p.modalities?.split(',').map((m: string) => (
                              <Badge key={m} variant="outline" className="text-xs">{m}</Badge>
                            ))}
                          </div>
                        </td>
                        <td className="py-2 pr-4">{p.quality_score?.toFixed(2)}</td>
                        <td className="py-2 pr-4">{p.match_count}</td>
                        <td className="py-2 pr-4"><Badge variant={p.status === 'active' ? 'default' : 'destructive'} className="text-xs">{p.status}</Badge></td>
                        <td className="py-2">{p.duplicate_flag ? <Badge variant="destructive" className="text-xs">FLAGGED</Badge> : <span className="text-zinc-400">-</span>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="text-xs text-zinc-400 mt-2">{profiles?.total} total profiles</p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="config">
          <div className="grid md:grid-cols-3 gap-4">
            {abisConfig && ['fingerprint', 'facial', 'iris'].map((mod) => (
              <Card key={mod}>
                <CardHeader><CardTitle className="text-sm capitalize">{mod} Configuration</CardTitle></CardHeader>
                <CardContent>
                  {abisConfig[mod] && (
                    <div className="space-y-2 text-xs">
                      {Object.entries(abisConfig[mod]).map(([k, v]) => (
                        <div key={k} className="flex justify-between">
                          <span className="text-zinc-500">{k.replace(/_/g, ' ')}</span>
                          <span className="font-mono font-semibold">{String(v)}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </CardContent>
              </Card>
            ))}
            {abisConfig?.fusion && (
              <Card>
                <CardHeader><CardTitle className="text-sm">Fusion Configuration</CardTitle></CardHeader>
                <CardContent>
                  <div className="space-y-2 text-xs">
                    {Object.entries(abisConfig.fusion).map(([k, v]) => (
                      <div key={k} className="flex justify-between">
                        <span className="text-zinc-500">{k.replace(/_/g, ' ')}</span>
                        <span className="font-mono font-semibold">{typeof v === 'object' ? JSON.stringify(v) : String(v)}</span>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            )}
            {abisConfig?.pad && (
              <Card>
                <CardHeader><CardTitle className="text-sm">PAD Configuration</CardTitle></CardHeader>
                <CardContent>
                  <div className="space-y-2 text-xs">
                    <div className="flex justify-between"><span className="text-zinc-500">Required</span><span className="font-semibold">{abisConfig.pad.required ? 'Yes' : 'No'}</span></div>
                    <div className="flex justify-between"><span className="text-zinc-500">Level</span><span className="font-mono">{abisConfig.pad.level}</span></div>
                    <div className="mt-2">
                      <p className="text-zinc-500 mb-1">Algorithms</p>
                      <div className="flex flex-wrap gap-1">
                        {abisConfig.pad.algorithms?.map((a: string) => (
                          <Badge key={a} variant="outline" className="text-[10px]">{a}</Badge>
                        ))}
                      </div>
                    </div>
                  </div>
                </CardContent>
              </Card>
            )}
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
