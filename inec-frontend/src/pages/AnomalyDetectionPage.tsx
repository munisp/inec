import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { useI18n } from '@/lib/i18n';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Progress } from '@/components/ui/progress';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, PieChart, Pie, Cell, Legend } from 'recharts';
import { AlertTriangle, ShieldCheck, Activity, Brain, RefreshCw } from 'lucide-react';

interface Anomaly {
  polling_unit_code: string;
  anomaly_type: string;
  severity: string;
  detail: string;
  score: number;
  pu_name?: string;
}

interface IntegrityData {
  integrity_score: number;
  grade: string;
  breakdown: Record<string, unknown>;
  methods_used: string[];
}

interface BenfordData {
  test: string;
  chi_square: number;
  status: string;
  sample_size: number;
  distribution: { digit: number; observed_pct: number; expected_pct: number; deviation: number }[];
}

const SEVERITY_COLORS: Record<string, string> = {
  critical: 'bg-red-100 text-red-800 border-red-200',
  high: 'bg-orange-100 text-orange-800 border-orange-200',
  medium: 'bg-yellow-100 text-yellow-800 border-yellow-200',
  low: 'bg-blue-100 text-blue-800 border-blue-200',
};

const PIE_COLORS = ['#ef4444', '#f97316', '#eab308', '#3b82f6'];

export default function AnomalyDetectionPage() {
  const { t } = useI18n();
  const [anomalies, setAnomalies] = useState<Anomaly[]>([]);
  const [integrity, setIntegrity] = useState<IntegrityData | null>(null);
  const [benford, setBenford] = useState<BenfordData | null>(null);
  const [methods, setMethods] = useState<{name: string; description: string}[]>([]);
  const [loading, setLoading] = useState(true);
  const [severityFilter, setSeverityFilter] = useState<string>('');
  const [summary, setSummary] = useState<Record<string, number>>({});
  const [gnnScore, setGnnScore] = useState<Record<string, unknown> | null>(null);

  const loadData = async () => {
    setLoading(true);
    try {
      const [anomalyRes, integrityRes, benfordRes, methodsRes, gnnRes] = await Promise.all([
        api.getAIAnomalies(1, severityFilter || undefined).catch(() => ({ anomalies: [], summary: {} })),
        api.getAIIntegrity(1).catch(() => null),
        api.getAIBenford(1).catch(() => null),
        api.getAIMethods().catch(() => ({ methods: [] })),
        api.getGNNScore(1).catch(() => null),
      ]);
      setAnomalies(anomalyRes?.anomalies || []);
      setSummary(anomalyRes?.summary || {});
      setIntegrity(integrityRes);
      setBenford(benfordRes);
      setMethods(methodsRes?.methods || []);
      setGnnScore(gnnRes);
    } catch {
      // fallback
    }
    setLoading(false);
  };

  useEffect(() => { loadData(); }, [severityFilter]);

  const gradeColor = (grade: string) => {
    if (grade === 'A' || grade === 'B') return 'text-green-700';
    if (grade === 'C') return 'text-yellow-600';
    return 'text-red-600';
  };

  const severityPieData = Object.entries(summary).map(([name, value]) => ({ name, value }));

  return (
    <div className="space-y-6" role="main" aria-label={t('anomaly_detection')}>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-zinc-900">{t('anomaly_detection')}</h1>
          <p className="text-sm text-zinc-500 mt-1">{t('anomaly_desc')}</p>
        </div>
        <Button onClick={loadData} disabled={loading} variant="outline" size="sm" aria-label={t('refresh')}>
          <RefreshCw className={`w-4 h-4 mr-2 ${loading ? 'animate-spin' : ''}`} />
          {t('refresh')}
        </Button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-green-100">
                <ShieldCheck className="w-5 h-5 text-green-700" />
              </div>
              <div>
                <p className="text-sm text-zinc-500">{t('integrity_score')}</p>
                <div className="flex items-baseline gap-2">
                  <span className="text-2xl font-bold">{integrity?.integrity_score ?? '—'}</span>
                  {integrity?.grade && (
                    <span className={`text-lg font-bold ${gradeColor(integrity.grade)}`}>
                      {integrity.grade}
                    </span>
                  )}
                </div>
                {integrity && <Progress value={integrity.integrity_score} className="mt-2 h-2" />}
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-red-100">
                <AlertTriangle className="w-5 h-5 text-red-700" />
              </div>
              <div>
                <p className="text-sm text-zinc-500">{t('total_anomalies')}</p>
                <span className="text-2xl font-bold">{anomalies.length}</span>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-purple-100">
                <Brain className="w-5 h-5 text-purple-700" />
              </div>
              <div>
                <p className="text-sm text-zinc-500">{t('ai_methods')}</p>
                <span className="text-2xl font-bold">{methods.length}</span>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-blue-100">
                <Activity className="w-5 h-5 text-blue-700" />
              </div>
              <div>
                <p className="text-sm text-zinc-500">{t('benford_status')}</p>
                <span className="text-2xl font-bold capitalize">{benford?.status ?? '—'}</span>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="overview" className="space-y-4">
        <TabsList>
          <TabsTrigger value="overview">{t('overview')}</TabsTrigger>
          <TabsTrigger value="benford">{t('benford_analysis')}</TabsTrigger>
          <TabsTrigger value="anomalies">{t('anomaly_list')}</TabsTrigger>
          <TabsTrigger value="methods">{t('ai_methods')}</TabsTrigger>
          <TabsTrigger value="gnn">GNN Graph</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-4">
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <Card>
              <CardHeader><CardTitle className="text-base">{t('severity_distribution')}</CardTitle></CardHeader>
              <CardContent>
                {severityPieData.length > 0 ? (
                  <ResponsiveContainer width="100%" height={250}>
                    <PieChart>
                      <Pie data={severityPieData} cx="50%" cy="50%" outerRadius={80} dataKey="value" label={({ name, value }) => `${name}: ${value}`}>
                        {severityPieData.map((_, i) => (
                          <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />
                        ))}
                      </Pie>
                      <Tooltip />
                      <Legend />
                    </PieChart>
                  </ResponsiveContainer>
                ) : (
                  <p className="text-sm text-zinc-400 text-center py-8">{t('no_data')}</p>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader><CardTitle className="text-base">{t('integrity_breakdown')}</CardTitle></CardHeader>
              <CardContent>
                {integrity?.breakdown ? (
                  <div className="space-y-3">
                    {Object.entries(integrity.breakdown).map(([key, val]) => (
                      <div key={key} className="flex items-center justify-between">
                        <span className="text-sm text-zinc-600 capitalize">{key.replace(/_/g, ' ')}</span>
                        <Badge variant="outline">{String(val)}</Badge>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-sm text-zinc-400 text-center py-8">{t('no_data')}</p>
                )}
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="benford" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">{t('benford_first_digit')}</CardTitle>
            </CardHeader>
            <CardContent>
              {benford?.distribution ? (
                <ResponsiveContainer width="100%" height={300}>
                  <BarChart data={benford.distribution}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis dataKey="digit" label={{ value: t('digit'), position: 'insideBottom', offset: -5 }} />
                    <YAxis label={{ value: '%', angle: -90, position: 'insideLeft' }} />
                    <Tooltip />
                    <Legend />
                    <Bar dataKey="observed_pct" fill="#3b82f6" name={t('observed')} />
                    <Bar dataKey="expected_pct" fill="#10b981" name={t('expected_benford')} />
                  </BarChart>
                </ResponsiveContainer>
              ) : (
                <p className="text-sm text-zinc-400 text-center py-8">{t('no_data')}</p>
              )}
              {benford && (
                <div className="mt-4 flex gap-4 text-sm">
                  <span>Chi-Square: <strong>{benford.chi_square}</strong></span>
                  <span>{t('sample_size')}: <strong>{benford.sample_size}</strong></span>
                  <Badge className={benford.status === 'pass' ? 'bg-green-100 text-green-800' : benford.status === 'suspicious' ? 'bg-yellow-100 text-yellow-800' : 'bg-red-100 text-red-800'}>
                    {(benford.status || 'N/A').toUpperCase()}
                  </Badge>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="anomalies" className="space-y-4">
          <div className="flex gap-2 mb-4" role="group" aria-label={t('filter_severity')}>
            {['', 'critical', 'high', 'medium', 'low'].map((s) => (
              <Button key={s} variant={severityFilter === s ? 'default' : 'outline'} size="sm" onClick={() => setSeverityFilter(s)}>
                {s || t('all')}
              </Button>
            ))}
          </div>
          <Card>
            <CardContent className="pt-6">
              <div className="overflow-x-auto">
                <table className="w-full text-sm" role="table" aria-label={t('anomaly_list')}>
                  <thead>
                    <tr className="border-b text-left">
                      <th className="py-2 px-3 font-medium">{t('polling_unit')}</th>
                      <th className="py-2 px-3 font-medium">{t('type')}</th>
                      <th className="py-2 px-3 font-medium">{t('severity')}</th>
                      <th className="py-2 px-3 font-medium">{t('description')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {anomalies.length === 0 ? (
                      <tr><td colSpan={4} className="py-8 text-center text-zinc-400">{t('no_anomalies')}</td></tr>
                    ) : anomalies.map((a, i) => (
                      <tr key={i} className="border-b hover:bg-zinc-50">
                        <td className="py-2 px-3 font-mono text-xs">{a.polling_unit_code}</td>
                        <td className="py-2 px-3 capitalize">{(a.anomaly_type || '').replace(/_/g, ' ')}</td>
                        <td className="py-2 px-3">
                          <Badge className={SEVERITY_COLORS[a.severity] || ''}>{a.severity}</Badge>
                        </td>
                        <td className="py-2 px-3 text-zinc-600 max-w-xs truncate">{a.detail}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="gnn" className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-base">Graph Neural Network — Cross-PU Anomaly Detection</CardTitle></CardHeader>
            <CardContent>
              {gnnScore ? (
                <div className="space-y-4">
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                    {Object.entries(gnnScore).filter(([k]) => typeof gnnScore[k] !== 'object' || gnnScore[k] === null).map(([k, v]) => (
                      <div key={k}>
                        <p className="text-xs text-zinc-500 capitalize">{k.replace(/_/g, ' ')}</p>
                        <p className="text-lg font-bold">{typeof v === 'number' ? (v as number).toFixed(3) : String(v)}</p>
                      </div>
                    ))}
                  </div>
                  {gnnScore.node_scores && Array.isArray(gnnScore.node_scores) && (
                    <div>
                      <h4 className="text-sm font-medium mb-2">Node Anomaly Scores</h4>
                      <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                        {(gnnScore.node_scores as Array<{node: string; score: number}>).slice(0, 20).map((n, i) => (
                          <div key={i} className={`p-2 rounded text-xs ${n.score > 0.7 ? 'bg-red-50 text-red-700' : n.score > 0.4 ? 'bg-yellow-50 text-yellow-700' : 'bg-green-50 text-green-700'}`}>
                            <span className="font-mono">{n.node}</span>: {(n.score * 100).toFixed(0)}%
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                  {gnnScore.edges && Array.isArray(gnnScore.edges) && (gnnScore.edges as Array<{from: string; to: string; weight: number}>).length > 0 && (
                    <div>
                      <h4 className="text-sm font-medium mb-2">Suspicious Edges ({(gnnScore.edges as unknown[]).length})</h4>
                      <div className="space-y-1 max-h-40 overflow-y-auto">
                        {(gnnScore.edges as Array<{from: string; to: string; weight: number}>).slice(0, 15).map((e, i) => (
                          <div key={i} className="text-xs font-mono text-zinc-600">{e.from} → {e.to} (weight: {e.weight?.toFixed(2)})</div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              ) : (
                <p className="text-sm text-zinc-400 text-center py-8">GNN data not available — model may need training first</p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="methods" className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {[
              { name: "Benford's Law", desc: t('benford_method_desc'), icon: '📊' },
              { name: 'Z-Score Outliers', desc: t('zscore_method_desc'), icon: '📈' },
              { name: 'IQR Outliers', desc: t('iqr_method_desc'), icon: '📉' },
              { name: 'Party Dominance', desc: t('dominance_method_desc'), icon: '🏛️' },
              { name: 'Round Number Bias', desc: t('round_number_method_desc'), icon: '🔢' },
              { name: 'Sequential Patterns', desc: t('sequential_method_desc'), icon: '🔗' },
            ].map((m) => (
              <Card key={m.name}>
                <CardContent className="pt-6">
                  <div className="flex items-start gap-3">
                    <span className="text-2xl" aria-hidden="true">{m.icon}</span>
                    <div>
                      <h3 className="font-medium text-zinc-900">{m.name}</h3>
                      <p className="text-sm text-zinc-500 mt-1">{m.desc}</p>
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
