import { useState } from 'react';
import { api } from '@/lib/api';
import { useI18n } from '@/lib/i18n';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { MessageSquare, Phone, BarChart3, Send, CheckCircle2 } from 'lucide-react';

interface SMSResult {
  status?: string;
  message?: string;
  response?: string;
  channel?: string;
  polling_unit?: string;
  results?: Record<string, unknown>;
}

interface USSDSession {
  response: string;
  session_active: boolean;
}

export default function SMSVerificationPage() {
  const { t } = useI18n();
  const [phone, setPhone] = useState('');
  const [puCode, setPuCode] = useState('');
  const [smsResult, setSmsResult] = useState<SMSResult | null>(null);
  const [smsLoading, setSmsLoading] = useState(false);

  const [ussdPhone] = useState('');
  const [ussdText, setUssdText] = useState('');
  const [, setUssdSession] = useState<USSDSession | null>(null);
  const [ussdHistory, setUssdHistory] = useState<string[]>([]);
  const [ussdLoading, setUssdLoading] = useState(false);
  const [sessionId] = useState(() => `sess_${Date.now()}`);

  const [stats, setStats] = useState<Record<string, unknown> | null>(null);

  const handleSMSVerify = async () => {
    if (!phone || !puCode) return;
    setSmsLoading(true);
    setSmsResult(null);
    try {
      const res = await api.smsVerify(phone, puCode);
      setSmsResult(res);
    } catch (e: unknown) {
      setSmsResult({ status: 'error', message: e instanceof Error ? e.message : 'Request failed' });
    }
    setSmsLoading(false);
  };

  const handleUSSDSend = async () => {
    setUssdLoading(true);
    try {
      const res = await api.ussdGateway(sessionId, ussdPhone || '+2348000000000', ussdText);
      setUssdSession(res);
      setUssdHistory(prev => [...prev, `> ${ussdText || '(start)'}`, res.response || res.message || '']);
      setUssdText('');
    } catch (e: unknown) {
      setUssdHistory(prev => [...prev, `Error: ${e instanceof Error ? e.message : 'Failed'}`]);
    }
    setUssdLoading(false);
  };

  const loadStats = async () => {
    try {
      const res = await api.getSMSStats();
      setStats(res);
    } catch {
      setStats({ error: 'Could not load stats' });
    }
  };

  return (
    <div className="space-y-6" role="main" aria-label={t('sms_verification')}>
      <div>
        <h1 className="text-2xl font-bold text-zinc-900">{t('sms_verification')}</h1>
        <p className="text-sm text-zinc-500 mt-1">{t('sms_desc')}</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-blue-100">
                <MessageSquare className="w-5 h-5 text-blue-700" />
              </div>
              <div>
                <p className="text-sm text-zinc-500">{t('sms_channel')}</p>
                <p className="text-lg font-bold">{t('text_verify')}</p>
              </div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-purple-100">
                <Phone className="w-5 h-5 text-purple-700" />
              </div>
              <div>
                <p className="text-sm text-zinc-500">{t('ussd_channel')}</p>
                <p className="text-lg font-bold">*347*123#</p>
              </div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-green-100">
                <BarChart3 className="w-5 h-5 text-green-700" />
              </div>
              <div>
                <p className="text-sm text-zinc-500">{t('no_internet')}</p>
                <p className="text-lg font-bold">{t('works_offline')}</p>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="sms" className="space-y-4">
        <TabsList>
          <TabsTrigger value="sms">{t('sms_verify')}</TabsTrigger>
          <TabsTrigger value="ussd">{t('ussd_simulator')}</TabsTrigger>
          <TabsTrigger value="stats" onClick={loadStats}>{t('statistics')}</TabsTrigger>
          <TabsTrigger value="guide">{t('user_guide')}</TabsTrigger>
        </TabsList>

        <TabsContent value="sms" className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-base">{t('sms_result_verify')}</CardTitle></CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label htmlFor="sms-phone" className="block text-sm font-medium text-zinc-700 mb-1">{t('phone_number')}</label>
                  <input
                    id="sms-phone"
                    type="tel"
                    value={phone}
                    onChange={(e) => setPhone(e.target.value)}
                    placeholder="+234..."
                    className="w-full border rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-green-500 focus:border-green-500"
                    aria-describedby="phone-hint"
                  />
                  <p id="phone-hint" className="text-xs text-zinc-400 mt-1">{t('phone_hint')}</p>
                </div>
                <div>
                  <label htmlFor="sms-pu" className="block text-sm font-medium text-zinc-700 mb-1">{t('polling_unit_code')}</label>
                  <input
                    id="sms-pu"
                    type="text"
                    value={puCode}
                    onChange={(e) => setPuCode(e.target.value)}
                    placeholder="FC/01/001/001"
                    className="w-full border rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-green-500 focus:border-green-500"
                    aria-describedby="pu-hint"
                  />
                  <p id="pu-hint" className="text-xs text-zinc-400 mt-1">{t('pu_code_hint')}</p>
                </div>
              </div>
              <Button onClick={handleSMSVerify} disabled={smsLoading || !phone || !puCode} className="bg-green-700 hover:bg-green-800">
                <Send className="w-4 h-4 mr-2" />
                {smsLoading ? t('sending') : t('verify_result')}
              </Button>

              {smsResult && (
                <div className={`mt-4 p-4 rounded-lg border ${smsResult.status === 'error' ? 'bg-red-50 border-red-200' : 'bg-green-50 border-green-200'}`} role="alert" aria-live="polite">
                  <div className="flex items-center gap-2 mb-2">
                    <CheckCircle2 className={`w-5 h-5 ${smsResult.status === 'error' ? 'text-red-600' : 'text-green-600'}`} />
                    <span className="font-medium">{smsResult.status === 'error' ? t('error') : t('result_found')}</span>
                  </div>
                  <p className="text-sm whitespace-pre-line">{smsResult.response || smsResult.message}</p>
                  {smsResult.results && (
                    <pre className="mt-2 text-xs bg-white p-2 rounded overflow-x-auto">{JSON.stringify(smsResult.results, null, 2)}</pre>
                  )}
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="ussd" className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-base">{t('ussd_simulator')}</CardTitle></CardHeader>
            <CardContent>
              <div className="max-w-sm mx-auto">
                <div className="bg-zinc-900 rounded-2xl p-4 text-white">
                  <div className="text-center text-xs text-zinc-400 mb-2">USSD *347*123#</div>
                  <div className="bg-zinc-800 rounded-lg p-3 min-h-[200px] max-h-[300px] overflow-y-auto text-sm font-mono space-y-1" aria-live="polite" role="log">
                    {ussdHistory.length === 0 ? (
                      <p className="text-zinc-500 text-center">{t('ussd_start_hint')}</p>
                    ) : ussdHistory.map((line, i) => (
                      <p key={i} className={line.startsWith('>') ? 'text-green-400' : 'text-white'}>{line}</p>
                    ))}
                  </div>
                  <div className="flex gap-2 mt-3">
                    <input
                      type="text"
                      value={ussdText}
                      onChange={(e) => setUssdText(e.target.value)}
                      onKeyDown={(e) => e.key === 'Enter' && handleUSSDSend()}
                      placeholder={t('enter_option')}
                      className="flex-1 bg-zinc-700 border-zinc-600 rounded px-3 py-2 text-sm text-white placeholder-zinc-400 focus:ring-2 focus:ring-green-500"
                      aria-label={t('ussd_input')}
                    />
                    <Button onClick={handleUSSDSend} disabled={ussdLoading} size="sm" className="bg-green-600 hover:bg-green-700">
                      <Send className="w-4 h-4" />
                    </Button>
                  </div>
                </div>
                <p className="text-xs text-zinc-500 mt-2 text-center">{t('ussd_instructions')}</p>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="stats" className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-base">{t('sms_ussd_stats')}</CardTitle></CardHeader>
            <CardContent>
              {stats ? (
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                  {Object.entries(stats).map(([k, v]) => (
                    <div key={k} className="text-center p-4 bg-zinc-50 rounded-lg">
                      <p className="text-2xl font-bold text-zinc-900">{String(v)}</p>
                      <p className="text-xs text-zinc-500 capitalize mt-1">{k.replace(/_/g, ' ')}</p>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-zinc-400 text-center py-8">{t('click_tab_load')}</p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="guide" className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-base">{t('how_to_use')}</CardTitle></CardHeader>
            <CardContent className="space-y-6">
              <div>
                <h3 className="font-medium text-zinc-900 mb-2 flex items-center gap-2">
                  <MessageSquare className="w-4 h-4" /> {t('sms_guide_title')}
                </h3>
                <ol className="list-decimal list-inside text-sm text-zinc-600 space-y-1">
                  <li>{t('sms_step_1')}</li>
                  <li>{t('sms_step_2')}</li>
                  <li>{t('sms_step_3')}</li>
                </ol>
              </div>
              <div>
                <h3 className="font-medium text-zinc-900 mb-2 flex items-center gap-2">
                  <Phone className="w-4 h-4" /> {t('ussd_guide_title')}
                </h3>
                <ol className="list-decimal list-inside text-sm text-zinc-600 space-y-1">
                  <li>{t('ussd_step_1')}</li>
                  <li>{t('ussd_step_2')}</li>
                  <li>{t('ussd_step_3')}</li>
                  <li>{t('ussd_step_4')}</li>
                </ol>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
