import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { ShieldCheck, RefreshCw, CheckCircle2, XCircle, BarChart3, FileCheck } from 'lucide-react';

export default function DataValidationPage() {
  const [stats, setStats] = useState<any>(null);
  const [rules, setRules] = useState<any[]>([]);
  const [history, setHistory] = useState<any[]>([]);
  const [tab, setTab] = useState<'overview'|'rules'|'history'>('overview');

  useEffect(() => { loadAll(); }, []);

  const loadAll = async () => {
    try {
      const [s, r, h] = await Promise.all([
        api.getEMSValidationStats(),
        api.getEMSValidationRules(),
        api.getEMSValidationHistory(),
      ]);
      setStats(s); setRules(r || []); setHistory(h || []);
    } catch {}
  };

  const severityColor = (s: string) => {
    switch(s) {
      case 'critical': return 'bg-red-100 text-red-800 border-red-200';
      case 'error': return 'bg-orange-100 text-orange-800 border-orange-200';
      case 'warning': return 'bg-yellow-100 text-yellow-800 border-yellow-200';
      case 'info': return 'bg-blue-100 text-blue-800 border-blue-200';
      default: return 'bg-zinc-100 text-zinc-800';
    }
  };

  const ruleTypeColor = (t: string) => {
    switch(t) {
      case 'format': return 'bg-purple-100 text-purple-800';
      case 'range': return 'bg-blue-100 text-blue-800';
      case 'cross_reference': return 'bg-indigo-100 text-indigo-800';
      case 'statistical': return 'bg-teal-100 text-teal-800';
      case 'business': return 'bg-orange-100 text-orange-800';
      default: return 'bg-zinc-100 text-zinc-800';
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-zinc-900">Data Validation Pipeline</h1>
          <p className="text-sm text-zinc-500">Multi-stage validation: format, range, cross-reference, statistical, and business rules</p>
        </div>
        <div className="flex gap-2">
          {(['overview','rules','history'] as const).map(t => (
            <Button key={t} variant={tab === t ? 'default' : 'outline'} size="sm" onClick={() => setTab(t)} className="capitalize">{t}</Button>
          ))}
          <Button variant="outline" size="sm" onClick={loadAll}><RefreshCw className="w-4 h-4" /></Button>
        </div>
      </div>

      {tab === 'overview' && stats && (
        <>
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
            {[
              { label: 'Total Rules', value: stats.total_rules, icon: ShieldCheck, color: 'text-blue-600' },
              { label: 'Active Rules', value: stats.active_rules, icon: FileCheck, color: 'text-green-600' },
              { label: 'Total Checks', value: stats.total_checks, icon: BarChart3, color: 'text-purple-600' },
              { label: 'Passed', value: stats.total_passed, icon: CheckCircle2, color: 'text-green-600' },
              { label: 'Failed', value: stats.total_failed, icon: XCircle, color: 'text-red-600' },
              { label: 'Pass Rate', value: `${stats.pass_rate}%`, icon: ShieldCheck, color: 'text-teal-600' },
            ].map(s => (
              <Card key={s.label}>
                <CardContent className="p-4">
                  <div className="flex items-center gap-2 mb-1">
                    <s.icon className={`w-4 h-4 ${s.color}`} />
                    <span className="text-xs text-zinc-500">{s.label}</span>
                  </div>
                  <p className="text-xl font-bold">{s.value}</p>
                </CardContent>
              </Card>
            ))}
          </div>

          <div className="grid md:grid-cols-2 gap-6">
            <Card>
              <CardHeader><CardTitle className="text-sm">Rules by Type</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-2">
                  {(stats.by_rule_type || []).map((r: any) => (
                    <div key={r.rule_type} className="flex items-center justify-between">
                      <Badge className={`capitalize ${ruleTypeColor(r.rule_type)}`}>{r.rule_type.replace('_', ' ')}</Badge>
                      <span className="font-medium">{r.count} rules</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">Failures by Severity</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-2">
                  {(stats.failures_by_severity || []).map((s: any) => (
                    <div key={s.severity} className="flex items-center justify-between">
                      <Badge className={`capitalize ${severityColor(s.severity)}`}>{s.severity}</Badge>
                      <span className="font-medium text-red-600">{s.count} failures</span>
                    </div>
                  ))}
                  {(!stats.failures_by_severity || stats.failures_by_severity.length === 0) && (
                    <div className="flex items-center gap-2 text-green-600">
                      <CheckCircle2 className="w-4 h-4" />
                      <span className="text-sm">No validation failures</span>
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
          </div>
        </>
      )}

      {tab === 'rules' && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Validation Rules ({rules.length})</CardTitle></CardHeader>
          <CardContent>
            <div className="space-y-3">
              {rules.map((r: any) => (
                <div key={r.id} className={`border rounded-lg p-4 ${r.is_active === 1 ? '' : 'opacity-50'}`}>
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      <ShieldCheck className="w-4 h-4 text-green-600" />
                      <span className="font-medium text-sm">{r.rule_name?.replace(/_/g, ' ')}</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <Badge className={`text-xs ${ruleTypeColor(r.rule_type)}`}>{r.rule_type?.replace('_', ' ')}</Badge>
                      <Badge className={`text-xs ${severityColor(r.severity)}`}>{r.severity}</Badge>
                      <Badge variant="outline" className="text-xs">{r.entity_type}</Badge>
                    </div>
                  </div>
                  <p className="text-sm text-zinc-600">{r.description}</p>
                  <p className="text-xs text-zinc-400 font-mono mt-1">{r.expression}</p>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {tab === 'history' && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Validation History</CardTitle></CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead><tr className="border-b text-left text-zinc-500">
                  <th className="pb-2 pr-4">Entity</th><th className="pb-2 pr-4">Rule</th><th className="pb-2 pr-4">Type</th>
                  <th className="pb-2 pr-4">Result</th><th className="pb-2 pr-4">Severity</th><th className="pb-2">Message</th>
                </tr></thead>
                <tbody>
                  {history.map((h: any, i: number) => (
                    <tr key={i} className="border-b border-zinc-50 hover:bg-zinc-50">
                      <td className="py-2 pr-4 font-mono text-xs">{h.entity_type}:{h.entity_id}</td>
                      <td className="py-2 pr-4 text-xs">{h.rule_name?.replace(/_/g, ' ')}</td>
                      <td className="py-2 pr-4"><Badge className={`text-xs ${ruleTypeColor(h.rule_type)}`}>{h.rule_type}</Badge></td>
                      <td className="py-2 pr-4">
                        {h.passed === 1 || h.passed === '1' ?
                          <CheckCircle2 className="w-4 h-4 text-green-600" /> :
                          <XCircle className="w-4 h-4 text-red-600" />}
                      </td>
                      <td className="py-2 pr-4"><Badge className={`text-xs ${severityColor(h.severity)}`}>{h.severity}</Badge></td>
                      <td className="py-2 text-xs text-zinc-600">{h.message}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {history.length === 0 && <p className="text-center text-zinc-400 py-8">No validation history yet</p>}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
