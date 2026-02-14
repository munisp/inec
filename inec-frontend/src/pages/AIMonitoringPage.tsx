import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Brain, TrendingUp, Shield, Camera, MessageSquare, AlertTriangle } from 'lucide-react';

export default function AIMonitoringPage() {
  const [dashboard, setDashboard] = useState<any>(null);
  const [predictions, setPredictions] = useState<any>(null);
  const [sentiment, setSentiment] = useState<any>(null);
  const [misinfo, setMisinfo] = useState<any>(null);
  const [threats, setThreats] = useState<any>(null);
  const [cvEvents, setCvEvents] = useState<any>(null);
  const [tab, setTab] = useState('overview');

  useEffect(() => {
    api.getAIMonitoringDashboard().then(setDashboard).catch(() => {});
    api.getAIPredictions().then(setPredictions).catch(() => {});
    api.getSentimentAnalysis().then(setSentiment).catch(() => {});
    api.getMisinformationAlerts().then(setMisinfo).catch(() => {});
    api.getSecurityThreats().then(setThreats).catch(() => {});
    api.getCVMonitoring().then(setCvEvents).catch(() => {});
  }, []);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-bold">AI Election Monitoring & Analytics</h2>
        <p className="text-zinc-500 text-sm">Predictive analytics, sentiment analysis, NLP misinformation detection, and geospatial security</p>
      </div>

      {dashboard && (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
          {[
            { label: 'AI Predictions', value: dashboard.predictions?.total, icon: Brain, color: 'purple' },
            { label: 'Sentiment Posts', value: dashboard.sentiment?.total, icon: MessageSquare, color: 'blue' },
            { label: 'Positive Rate', value: `${dashboard.sentiment?.positive_rate?.toFixed(1)}%`, icon: TrendingUp, color: 'green' },
            { label: 'Misinfo Alerts', value: dashboard.misinformation?.total, icon: AlertTriangle, color: 'red' },
            { label: 'Active Threats', value: dashboard.security_threats?.active, icon: Shield, color: 'orange' },
            { label: 'CV Events', value: dashboard.cv_monitoring?.total_events, icon: Camera, color: 'cyan' },
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
        <TabsList className="flex-wrap">
          <TabsTrigger value="overview">Predictions</TabsTrigger>
          <TabsTrigger value="sentiment">Sentiment</TabsTrigger>
          <TabsTrigger value="misinfo">Misinformation</TabsTrigger>
          <TabsTrigger value="threats">Security Threats</TabsTrigger>
          <TabsTrigger value="cv">CV Monitoring</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <Card>
            <CardHeader><CardTitle className="text-sm">Predictive Analytics</CardTitle></CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-4">Type</th><th className="pb-2 pr-4">Area</th><th className="pb-2 pr-4">Level</th><th className="pb-2 pr-4">Predicted Value</th><th className="pb-2 pr-4">Confidence</th><th className="pb-2">Model</th>
                  </tr></thead>
                  <tbody>
                    {predictions?.predictions?.slice(0, 30).map((p: any) => (
                      <tr key={p.id} className="border-b border-zinc-100">
                        <td className="py-2 pr-4"><Badge variant="outline" className="text-xs capitalize">{p.type?.replace('_', ' ')}</Badge></td>
                        <td className="py-2 pr-4">{p.target_area}</td>
                        <td className="py-2 pr-4 capitalize">{p.target_level}</td>
                        <td className="py-2 pr-4 font-semibold">{p.predicted_value?.toFixed(1)}</td>
                        <td className="py-2 pr-4">
                          <div className="flex items-center gap-2">
                            <div className="w-16 h-2 bg-zinc-100 rounded-full overflow-hidden">
                              <div className="h-full bg-blue-500 rounded-full" style={{ width: `${p.confidence * 100}%` }} />
                            </div>
                            <span className="text-xs">{(p.confidence * 100).toFixed(0)}%</span>
                          </div>
                        </td>
                        <td className="py-2 font-mono text-xs">{p.model}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="sentiment">
          <div className="grid md:grid-cols-2 gap-4">
            <Card>
              <CardHeader><CardTitle className="text-sm">Sentiment Distribution</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {sentiment?.by_sentiment?.map((s: any, i: number) => {
                    const colors: Record<string, string> = { positive: 'bg-green-500', negative: 'bg-red-500', neutral: 'bg-zinc-400', mixed: 'bg-amber-500' };
                    return (
                      <div key={i}>
                        <div className="flex justify-between mb-1">
                          <span className="text-sm capitalize font-medium">{s.sentiment}</span>
                          <span className="text-sm">{s.count}</span>
                        </div>
                        <div className="w-full h-3 bg-zinc-100 rounded-full overflow-hidden">
                          <div className={`h-full rounded-full ${colors[s.sentiment] || 'bg-zinc-300'}`} style={{ width: `${(s.count / (dashboard?.sentiment?.total || 1)) * 100}%` }} />
                        </div>
                      </div>
                    );
                  })}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">By Source Platform</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {sentiment?.by_source?.map((s: any, i: number) => (
                    <div key={i} className="flex items-center justify-between py-2 border-b border-zinc-50">
                      <span className="text-sm capitalize">{s.source}</span>
                      <Badge variant="outline">{s.count}</Badge>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
            <Card className="md:col-span-2">
              <CardHeader><CardTitle className="text-sm">Trending Topics</CardTitle></CardHeader>
              <CardContent>
                <div className="flex flex-wrap gap-2">
                  {sentiment?.trending_topics?.map((t: any, i: number) => (
                    <div key={i} className="px-3 py-2 border rounded-lg">
                      <span className="text-sm font-medium">{t.topic}</span>
                      <Badge variant="outline" className="ml-2 text-xs">{t.mentions}</Badge>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="misinfo">
          <Card>
            <CardHeader>
              <CardTitle className="text-sm flex items-center justify-between">
                Misinformation Detection (NLP)
                <div className="flex gap-2 text-xs font-normal">
                  {misinfo?.by_classification?.map((c: any, i: number) => (
                    <Badge key={i} variant="outline">{c.classification?.replace('_', ' ')}: {c.count}</Badge>
                  ))}
                </div>
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                {misinfo?.alerts?.map((a: any) => (
                  <div key={a.id} className="p-3 border rounded-lg">
                    <div className="flex items-start justify-between mb-2">
                      <div className="flex items-center gap-2">
                        <AlertTriangle className={`w-4 h-4 ${a.severity === 'critical' ? 'text-red-600' : a.severity === 'high' ? 'text-orange-600' : 'text-amber-600'}`} />
                        <Badge className={`text-xs ${a.severity === 'critical' ? 'bg-red-100 text-red-700' : a.severity === 'high' ? 'bg-orange-100 text-orange-700' : 'bg-amber-100 text-amber-700'}`}>
                          {a.severity}
                        </Badge>
                        <Badge variant="outline" className="text-xs capitalize">{a.classification?.replace('_', ' ')}</Badge>
                      </div>
                      <Badge variant={a.status === 'debunked' ? 'default' : a.status === 'verified' ? 'destructive' : 'outline'} className="text-xs">{a.status}</Badge>
                    </div>
                    <p className="text-sm mb-1">{a.content}</p>
                    <p className="text-xs text-zinc-500 italic mb-1">Fact check: {a.fact_check}</p>
                    <div className="flex items-center gap-4 text-xs text-zinc-400">
                      <span>Platform: {a.platform}</span>
                      <span>Confidence: {(a.confidence * 100).toFixed(0)}%</span>
                      <span>Reach: ~{a.reach_estimate?.toLocaleString()}</span>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="threats">
          <div className="grid md:grid-cols-3 gap-4 mb-4">
            {threats?.by_severity?.map((s: any, i: number) => (
              <Card key={i}>
                <CardContent className="pt-4 pb-3 text-center">
                  <p className="text-2xl font-bold">{s.count}</p>
                  <p className="text-xs text-zinc-500 capitalize">{s.severity} Severity</p>
                </CardContent>
              </Card>
            ))}
          </div>
          <Card>
            <CardHeader><CardTitle className="text-sm">Geospatial Security Threats</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-3">
                {threats?.threats?.map((t: any) => (
                  <div key={t.id} className="p-3 border rounded-lg">
                    <div className="flex items-start justify-between mb-2">
                      <div className="flex items-center gap-2">
                        <Shield className={`w-4 h-4 ${t.severity === 'critical' ? 'text-red-600' : t.severity === 'high' ? 'text-orange-600' : 'text-amber-600'}`} />
                        <span className="font-medium text-sm capitalize">{t.type?.replace('_', ' ')}</span>
                        <Badge className={`text-xs ${t.severity === 'critical' ? 'bg-red-100 text-red-700' : t.severity === 'high' ? 'bg-orange-100 text-orange-700' : 'bg-amber-100 text-amber-700'}`}>
                          {t.severity}
                        </Badge>
                      </div>
                      <Badge variant={t.status === 'resolved' ? 'default' : t.status === 'mitigated' ? 'default' : 'outline'} className="text-xs">{t.status}</Badge>
                    </div>
                    <p className="text-sm text-zinc-600 mb-1">{t.description}</p>
                    <div className="flex items-center gap-4 text-xs text-zinc-500">
                      <span>Location: {t.location}</span>
                      <span>Coords: {t.latitude?.toFixed(2)}, {t.longitude?.toFixed(2)}</span>
                      <span>Affected PUs: {t.affected_pus}</span>
                      <span>Confidence: {(t.confidence * 100).toFixed(0)}%</span>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="cv">
          <div className="grid md:grid-cols-3 gap-4 mb-4">
            {cvEvents?.by_type?.map((t: any, i: number) => (
              <Card key={i}>
                <CardContent className="pt-4 pb-3">
                  <div className="flex items-center gap-2 mb-1">
                    <Camera className="w-4 h-4 text-cyan-600" />
                    <span className="text-xs text-zinc-500 capitalize">{t.type?.replace('_', ' ')}</span>
                  </div>
                  <p className="text-xl font-bold">{t.count}</p>
                  <p className="text-xs text-zinc-400">Avg confidence: {(t.avg_confidence * 100).toFixed(0)}%</p>
                </CardContent>
              </Card>
            ))}
          </div>
          <Card>
            <CardHeader><CardTitle className="text-sm">Computer Vision Events</CardTitle></CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-4">Camera</th><th className="pb-2 pr-4">Event</th><th className="pb-2 pr-4">Value</th><th className="pb-2 pr-4">Confidence</th><th className="pb-2">Time</th>
                  </tr></thead>
                  <tbody>
                    {cvEvents?.events?.map((e: any) => (
                      <tr key={e.id} className="border-b border-zinc-100">
                        <td className="py-2 pr-4 font-mono text-xs">{e.camera_id}</td>
                        <td className="py-2 pr-4"><Badge variant="outline" className="text-xs capitalize">{e.event_type?.replace('_', ' ')}</Badge></td>
                        <td className="py-2 pr-4">{e.value?.toFixed(1)}</td>
                        <td className="py-2 pr-4">{(e.confidence * 100).toFixed(0)}%</td>
                        <td className="py-2 text-xs text-zinc-500">{new Date(e.detected_at).toLocaleString()}</td>
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
