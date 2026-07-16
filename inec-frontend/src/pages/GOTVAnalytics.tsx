import { useEffect, useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { BarChart3, Brain, DollarSign, TrendingUp, AlertTriangle } from 'lucide-react';
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, PieChart, Pie, Cell } from 'recharts';

interface ChannelROI {
  channel: string;
  total_sent: number;
  total_delivered: number;
  total_responded: number;
  total_pledged: number;
  total_cost_kobo: number;
  cost_per_send: number;
  cost_per_deliver: number;
  cost_per_respond: number;
  cost_per_pledge: number;
  delivery_rate: number;
  response_rate: number;
  recommendation: string;
}

interface AIVariant {
  variant_id: string;
  base_message: string;
  variant_text: string;
  target_state: string;
  channel: string;
  variant_index: number;
}

interface TurnoutPrediction {
  ward_code: string;
  predicted_turnout: number;
  confidence: number;
  risk_level: string;
  recommended_actions: string[];
}

const PIE_COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899'];

const REC_COLORS: Record<string, string> = {
  SCALE_UP: 'bg-green-100 text-green-800',
  OPTIMIZE: 'bg-yellow-100 text-yellow-800',
  REDUCE: 'bg-red-100 text-red-800',
  INSUFFICIENT_DATA: 'bg-gray-100 text-gray-800',
};

export default function GOTVAnalytics() {
  const [roi, setRoi] = useState<ChannelROI[]>([]);
  const [variants, setVariants] = useState<AIVariant[]>([]);
  const [predictions, setPredictions] = useState<TurnoutPrediction[]>([]);
  const [loading, setLoading] = useState(true);
  const [view, setView] = useState<'roi' | 'ai' | 'turnout'>('roi');
  const headers = { Authorization: `Bearer ${localStorage.getItem('auth_token')}`, 'X-Party-ID': localStorage.getItem('gotv_party_id') || '1' };

  useEffect(() => {
    Promise.all([
      fetch('/gotv/roi/channels', { headers }).then(r => r.json()).catch(() => ({ channels: [] })),
      fetch('/gotv/ai/variants', { headers }).then(r => r.json()).catch(() => ({ variants: [] })),
    ]).then(([roiData, aiData]) => {
      setRoi(roiData.channels || []);
      setVariants(aiData.variants || []);
    }).finally(() => setLoading(false));
  }, []);

  const loadTurnout = () => {
    fetch('/gotv/turnout/predict', {
      method: 'POST', headers: { ...headers, 'Content-Type': 'application/json' },
      body: JSON.stringify({ ward_codes: [], election_id: 1 }),
    })
      .then(r => r.json())
      .then(d => setPredictions(d.predictions || []))
      .catch(() => setPredictions([]));
  };

  if (loading) return <div className="text-center py-12 text-muted-foreground">Loading analytics...</div>;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold flex items-center gap-2">
          <BarChart3 className="h-5 w-5" /> GOTV Analytics
        </h2>
        <div className="flex gap-1">
          {([
            { key: 'roi', label: 'Channel ROI', icon: DollarSign },
            { key: 'ai', label: 'AI Variants', icon: Brain },
            { key: 'turnout', label: 'Turnout Prediction', icon: TrendingUp },
          ] as const).map(t => (
            <Button key={t.key} size="sm" variant={view === t.key ? 'default' : 'outline'}
              onClick={() => { setView(t.key); if (t.key === 'turnout' && predictions.length === 0) loadTurnout(); }}>
              <t.icon className="h-4 w-4 mr-1" /> {t.label}
            </Button>
          ))}
        </div>
      </div>

      {view === 'roi' && (
        <div className="space-y-4">
          {roi.length === 0 ? (
            <Card><CardContent className="py-8 text-center text-muted-foreground">No outreach data yet. Launch a campaign to see ROI metrics.</CardContent></Card>
          ) : (
            <>
              <div className="grid grid-cols-2 gap-4">
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm">Cost per Conversion by Channel</CardTitle></CardHeader>
                  <CardContent>
                    <ResponsiveContainer width="100%" height={200}>
                      <BarChart data={roi}>
                        <XAxis dataKey="channel" tick={{ fontSize: 10 }} />
                        <YAxis />
                        <Tooltip formatter={(v: number) => `₦${(v / 100).toFixed(2)}`} />
                        <Bar dataKey="cost_per_pledge" fill="#3b82f6" name="Cost/Pledge (kobo)" />
                      </BarChart>
                    </ResponsiveContainer>
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm">Send Volume by Channel</CardTitle></CardHeader>
                  <CardContent>
                    <ResponsiveContainer width="100%" height={200}>
                      <PieChart>
                        <Pie data={roi} dataKey="total_sent" nameKey="channel" cx="50%" cy="50%" outerRadius={80} label>
                          {roi.map((_, i) => <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />)}
                        </Pie>
                        <Tooltip />
                      </PieChart>
                    </ResponsiveContainer>
                  </CardContent>
                </Card>
              </div>
              <div className="space-y-2">
                {roi.map(ch => {
                  const recKey = ch.recommendation.split(':')[0];
                  return (
                    <Card key={ch.channel}>
                      <CardContent className="flex items-center gap-4 py-3">
                        <div className="font-medium w-24">{ch.channel}</div>
                        <div className="flex-1 grid grid-cols-5 gap-2 text-xs text-center">
                          <div><div className="font-bold">{ch.total_sent}</div>Sent</div>
                          <div><div className="font-bold">{ch.total_delivered}</div>Delivered</div>
                          <div><div className="font-bold">{ch.total_responded}</div>Responded</div>
                          <div><div className="font-bold">{ch.total_pledged}</div>Pledged</div>
                          <div><div className="font-bold">₦{(ch.total_cost_kobo / 100).toFixed(0)}</div>Total Cost</div>
                        </div>
                        <Badge className={`text-xs ${REC_COLORS[recKey] || 'bg-gray-100'}`}>
                          {ch.recommendation}
                        </Badge>
                      </CardContent>
                    </Card>
                  );
                })}
              </div>
            </>
          )}
        </div>
      )}

      {view === 'ai' && (
        <div className="space-y-2">
          {variants.length === 0 ? (
            <Card><CardContent className="py-8 text-center text-muted-foreground">No AI-generated variants yet. Use the AI optimization endpoint.</CardContent></Card>
          ) : (
            variants.map(v => (
              <Card key={v.variant_id}>
                <CardContent className="py-3">
                  <div className="flex items-center gap-2 mb-1">
                    <Badge variant="secondary" className="text-xs">{v.channel}</Badge>
                    <Badge variant="secondary" className="text-xs">{v.target_state}</Badge>
                    <Badge variant="secondary" className="text-xs">Variant #{v.variant_index + 1}</Badge>
                  </div>
                  <p className="text-sm">{v.variant_text}</p>
                  <p className="text-xs text-muted-foreground mt-1">Base: {v.base_message}</p>
                </CardContent>
              </Card>
            ))
          )}
        </div>
      )}

      {view === 'turnout' && (
        <div className="space-y-2">
          {predictions.length === 0 ? (
            <Card>
              <CardContent className="py-8 text-center">
                <p className="text-muted-foreground mb-3">Run turnout prediction for all wards</p>
                <Button onClick={loadTurnout}><TrendingUp className="h-4 w-4 mr-1" /> Predict Turnout</Button>
              </CardContent>
            </Card>
          ) : (
            predictions.map(p => (
              <Card key={p.ward_code} className={p.risk_level === 'high' ? 'border-red-200' : p.risk_level === 'medium' ? 'border-yellow-200' : ''}>
                <CardContent className="flex items-center gap-4 py-3">
                  <div className="font-medium w-32">{p.ward_code}</div>
                  <div className="flex-1">
                    <div className="flex items-center gap-2">
                      <div className="w-full bg-gray-200 rounded-full h-2">
                        <div className="bg-blue-500 h-2 rounded-full" style={{ width: `${p.predicted_turnout}%` }} />
                      </div>
                      <span className="text-sm font-bold w-12">{p.predicted_turnout.toFixed(0)}%</span>
                    </div>
                  </div>
                  <Badge className={`text-xs ${
                    p.risk_level === 'high' ? 'bg-red-100 text-red-800' :
                    p.risk_level === 'medium' ? 'bg-yellow-100 text-yellow-800' : 'bg-green-100 text-green-800'
                  }`}>
                    {p.risk_level === 'high' && <AlertTriangle className="h-3 w-3 mr-1" />}
                    {p.risk_level} risk
                  </Badge>
                  <div className="text-xs text-muted-foreground">
                    {(p.confidence * 100).toFixed(0)}% conf
                  </div>
                </CardContent>
              </Card>
            ))
          )}
        </div>
      )}
    </div>
  );
}
