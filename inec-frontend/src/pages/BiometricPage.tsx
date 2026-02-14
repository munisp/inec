import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Fingerprint, Eye, ScanFace, AlertTriangle, ShieldCheck, Activity } from 'lucide-react';

export default function BiometricPage() {
  const [stats, setStats] = useState<any>(null);
  const [profiles, setProfiles] = useState<any>(null);
  const [duplicates, setDuplicates] = useState<any>(null);
  const [tab, setTab] = useState('overview');

  useEffect(() => {
    api.getBiometricStats().then(setStats).catch(() => {});
    api.getBiometricProfiles(50, 0).then(setProfiles).catch(() => {});
    api.getABISDuplicates().then(setDuplicates).catch(() => {});
  }, []);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-bold">Enhanced Biometric Verification</h2>
        <p className="text-zinc-500 text-sm">Multi-modal biometrics with ABIS duplicate detection</p>
      </div>

      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-6 gap-4">
          {[
            { label: 'Total Profiles', value: stats.total_profiles?.toLocaleString(), icon: Fingerprint, color: 'blue' },
            { label: 'Active Enrolled', value: stats.enrolled_active?.toLocaleString(), icon: ShieldCheck, color: 'green' },
            { label: 'Multi-Modal', value: stats.multi_modal?.toLocaleString(), icon: ScanFace, color: 'purple' },
            { label: 'Duplicates Flagged', value: stats.duplicates_flagged, icon: AlertTriangle, color: 'red' },
            { label: 'Match Rate', value: `${stats.match_rate?.toFixed(1)}%`, icon: Activity, color: 'emerald' },
            { label: 'Avg Latency', value: `${stats.avg_latency_ms?.toFixed(0)}ms`, icon: Eye, color: 'amber' },
          ].map((s, i) => (
            <Card key={i}>
              <CardContent className="pt-4 pb-3">
                <div className="flex items-center gap-2 mb-1">
                  <s.icon className={`w-4 h-4 text-${s.color}-600`} />
                  <span className="text-xs text-zinc-500">{s.label}</span>
                </div>
                <p className="text-xl font-bold">{s.value}</p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="overview">Verification Stats</TabsTrigger>
          <TabsTrigger value="profiles">Biometric Profiles</TabsTrigger>
          <TabsTrigger value="duplicates">ABIS Duplicates</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          {stats && (
            <div className="grid md:grid-cols-2 gap-4">
              <Card>
                <CardHeader><CardTitle className="text-sm">Verification Summary</CardTitle></CardHeader>
                <CardContent>
                  <div className="space-y-3">
                    <div className="flex justify-between"><span className="text-sm text-zinc-500">Total Verifications</span><span className="font-semibold">{stats.total_verifications?.toLocaleString()}</span></div>
                    <div className="flex justify-between"><span className="text-sm text-zinc-500">Matches</span><span className="font-semibold text-green-600">{stats.matches?.toLocaleString()}</span></div>
                    <div className="flex justify-between"><span className="text-sm text-zinc-500">No Matches</span><span className="font-semibold text-red-600">{stats.no_matches?.toLocaleString()}</span></div>
                    <div className="flex justify-between"><span className="text-sm text-zinc-500">Spoof Detections</span><span className="font-semibold text-amber-600">{stats.spoof_detections}</span></div>
                    <div className="flex justify-between"><span className="text-sm text-zinc-500">Avg Quality Score</span><span className="font-semibold">{stats.avg_quality?.toFixed(2)}</span></div>
                  </div>
                </CardContent>
              </Card>
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
            </div>
          )}
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

        <TabsContent value="duplicates">
          <Card>
            <CardHeader>
              <CardTitle className="text-sm flex items-center justify-between">
                ABIS Duplicate Detection
                <div className="flex gap-2 text-xs font-normal">
                  <Badge variant="outline">Pending: {duplicates?.pending || 0}</Badge>
                  <Badge variant="destructive">Confirmed: {duplicates?.confirmed || 0}</Badge>
                  <Badge className="bg-green-100 text-green-700">False Positives: {duplicates?.false_positives || 0}</Badge>
                </div>
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-4">ID</th><th className="pb-2 pr-4">Source VIN</th><th className="pb-2 pr-4">Candidate VIN</th><th className="pb-2 pr-4">Similarity</th><th className="pb-2 pr-4">Modality</th><th className="pb-2">Status</th>
                  </tr></thead>
                  <tbody>
                    {duplicates?.checks?.map((d: any) => (
                      <tr key={d.id} className="border-b border-zinc-100">
                        <td className="py-2 pr-4">{d.id}</td>
                        <td className="py-2 pr-4 font-mono text-xs">{d.source_vin}</td>
                        <td className="py-2 pr-4 font-mono text-xs">{d.candidate_vin}</td>
                        <td className="py-2 pr-4">
                          <div className="flex items-center gap-2">
                            <div className="w-16 h-2 bg-zinc-100 rounded-full overflow-hidden">
                              <div className="h-full rounded-full" style={{ width: `${d.similarity_score * 100}%`, backgroundColor: d.similarity_score > 0.9 ? '#ef4444' : d.similarity_score > 0.8 ? '#f59e0b' : '#22c55e' }} />
                            </div>
                            <span className="text-xs">{(d.similarity_score * 100).toFixed(1)}%</span>
                          </div>
                        </td>
                        <td className="py-2 pr-4 capitalize">{d.modality}</td>
                        <td className="py-2">
                          <Badge variant={d.status === 'confirmed_duplicate' ? 'destructive' : d.status === 'false_positive' ? 'default' : 'outline'} className="text-xs">
                            {d.status.replace('_', ' ')}
                          </Badge>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
