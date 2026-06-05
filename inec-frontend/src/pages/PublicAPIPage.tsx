import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { useI18n } from '@/lib/i18n';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Key, Copy, BookOpen, Plus, Shield } from 'lucide-react';

interface APIKey {
  id: number;
  name: string;
  owner: string;
  permissions: string;
  rate_limit: number;
  is_active: boolean;
  created_at: string;
  last_used_at?: string;
}

const ENDPOINTS = [
  { method: 'GET', path: '/api/v1/elections', desc: 'List all elections', auth: true },
  { method: 'GET', path: '/api/v1/results', desc: 'List results with pagination & filtering', auth: true },
  { method: 'GET', path: '/api/v1/results/{id}', desc: 'Get detailed result by ID', auth: true },
  { method: 'GET', path: '/api/v1/states', desc: 'List all states with geo zones', auth: true },
  { method: 'GET', path: '/api/v1/polling-units', desc: 'List polling units with filtering', auth: true },
  { method: 'GET', path: '/api/v1/collation', desc: 'Get collation data by level', auth: true },
  { method: 'GET', path: '/api/v1/ai/anomalies', desc: 'AI-detected anomalies', auth: true },
  { method: 'GET', path: '/api/v1/ai/integrity', desc: 'Election integrity score', auth: true },
  { method: 'GET', path: '/api/v1/docs', desc: 'OpenAPI 3.0 specification', auth: false },
  { method: 'POST', path: '/api/v1/keys', desc: 'Generate new API key', auth: false },
  { method: 'GET', path: '/api/v1/keys', desc: 'List API keys', auth: false },
  { method: 'GET', path: '/api/v1/usage', desc: 'API usage statistics', auth: false },
];

export default function PublicAPIPage() {
  const { t } = useI18n();
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyOwner, setNewKeyOwner] = useState('');
  const [generatedKey, setGeneratedKey] = useState('');
  const [loading, setLoading] = useState(false);
  const [usage, setUsage] = useState<Record<string, unknown> | null>(null);
  const [copied, setCopied] = useState('');

  const loadKeys = async () => {
    try {
      const res = await api.getAPIKeys();
      setKeys(res.data || res.keys || []);
    } catch { /* empty */ }
  };

  const loadUsage = async () => {
    try {
      const res = await api.getAPIUsage();
      setUsage(res);
    } catch { /* empty */ }
  };

  useEffect(() => { loadKeys(); }, []);

  const handleGenerate = async () => {
    if (!newKeyName || !newKeyOwner) return;
    setLoading(true);
    try {
      const res = await api.generateAPIKey(newKeyName, newKeyOwner);
      setGeneratedKey(res.api_key || res.key || '');
      setNewKeyName('');
      setNewKeyOwner('');
      loadKeys();
    } catch { /* empty */ }
    setLoading(false);
  };

  const copyToClipboard = (text: string, label: string) => {
    navigator.clipboard.writeText(text);
    setCopied(label);
    setTimeout(() => setCopied(''), 2000);
  };

  const methodColor = (m: string) => {
    if (m === 'GET') return 'bg-blue-100 text-blue-800';
    if (m === 'POST') return 'bg-green-100 text-green-800';
    if (m === 'PUT') return 'bg-yellow-100 text-yellow-800';
    return 'bg-zinc-100 text-zinc-800';
  };

  return (
    <div className="space-y-6" role="main" aria-label={t('public_api')}>
      <div>
        <h1 className="text-2xl font-bold text-zinc-900">{t('public_api')}</h1>
        <p className="text-sm text-zinc-500 mt-1">{t('public_api_desc')}</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-blue-100">
                <BookOpen className="w-5 h-5 text-blue-700" />
              </div>
              <div>
                <p className="text-sm text-zinc-500">{t('api_version')}</p>
                <p className="text-lg font-bold">v1.0</p>
              </div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-green-100">
                <Shield className="w-5 h-5 text-green-700" />
              </div>
              <div>
                <p className="text-sm text-zinc-500">{t('rate_limit')}</p>
                <p className="text-lg font-bold">100 {t('req_per_min')}</p>
              </div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-purple-100">
                <Key className="w-5 h-5 text-purple-700" />
              </div>
              <div>
                <p className="text-sm text-zinc-500">{t('active_keys')}</p>
                <p className="text-lg font-bold">{keys.filter(k => k.is_active).length}</p>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="docs" className="space-y-4">
        <TabsList>
          <TabsTrigger value="docs">{t('api_docs')}</TabsTrigger>
          <TabsTrigger value="keys">{t('api_keys')}</TabsTrigger>
          <TabsTrigger value="usage" onClick={loadUsage}>{t('usage')}</TabsTrigger>
          <TabsTrigger value="examples">{t('examples')}</TabsTrigger>
        </TabsList>

        <TabsContent value="docs" className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-base">{t('api_endpoints')}</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-2">
                {ENDPOINTS.map((ep, i) => (
                  <div key={i} className="flex items-center gap-3 p-3 rounded-lg hover:bg-zinc-50 border">
                    <Badge className={`${methodColor(ep.method)} font-mono text-xs min-w-[50px] justify-center`}>
                      {ep.method}
                    </Badge>
                    <code className="text-sm font-mono text-zinc-700 flex-1">{ep.path}</code>
                    <span className="text-sm text-zinc-500 hidden md:inline">{ep.desc}</span>
                    {ep.auth && <Badge variant="outline" className="text-xs"><Key className="w-3 h-3 mr-1" />{t('auth_required')}</Badge>}
                    <Button variant="ghost" size="sm" onClick={() => copyToClipboard(ep.path, ep.path)} aria-label={`Copy ${ep.path}`}>
                      <Copy className="w-3 h-3" />
                    </Button>
                    {copied === ep.path && <span className="text-xs text-green-600">{t('copied')}</span>}
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle className="text-base">{t('authentication')}</CardTitle></CardHeader>
            <CardContent className="text-sm text-zinc-600 space-y-2">
              <p>{t('auth_desc')}</p>
              <div className="bg-zinc-900 text-green-400 p-4 rounded-lg font-mono text-xs overflow-x-auto">
                <p>{`curl -H "X-API-Key: YOUR_KEY" \\`}</p>
                <p>{`  ${window.location.origin}/api/v1/results`}</p>
              </div>
              <p className="text-zinc-500">{t('auth_query_param')}</p>
              <div className="bg-zinc-900 text-green-400 p-4 rounded-lg font-mono text-xs overflow-x-auto">
                <p>{`${window.location.origin}/api/v1/results?api_key=YOUR_KEY`}</p>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="keys" className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-base flex items-center gap-2"><Plus className="w-4 h-4" /> {t('generate_key')}</CardTitle></CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label htmlFor="key-name" className="block text-sm font-medium text-zinc-700 mb-1">{t('key_name')}</label>
                  <input id="key-name" type="text" value={newKeyName} onChange={(e) => setNewKeyName(e.target.value)} placeholder="My App" className="w-full border rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-green-500" />
                </div>
                <div>
                  <label htmlFor="key-owner" className="block text-sm font-medium text-zinc-700 mb-1">{t('owner')}</label>
                  <input id="key-owner" type="text" value={newKeyOwner} onChange={(e) => setNewKeyOwner(e.target.value)} placeholder="organization@example.com" className="w-full border rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-green-500" />
                </div>
              </div>
              <Button onClick={handleGenerate} disabled={loading || !newKeyName || !newKeyOwner} className="bg-green-700 hover:bg-green-800">
                <Key className="w-4 h-4 mr-2" />
                {loading ? t('generating') : t('generate_key')}
              </Button>
              {generatedKey && (
                <div className="p-4 bg-green-50 border border-green-200 rounded-lg" role="alert">
                  <p className="text-sm font-medium text-green-800 mb-2">{t('key_generated')}</p>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 bg-white border rounded px-3 py-2 text-sm font-mono break-all">{generatedKey}</code>
                    <Button variant="outline" size="sm" onClick={() => copyToClipboard(generatedKey, 'key')}>
                      <Copy className="w-4 h-4" />
                    </Button>
                  </div>
                  <p className="text-xs text-green-600 mt-2">{t('key_warning')}</p>
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle className="text-base">{t('existing_keys')}</CardTitle></CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm" role="table" aria-label={t('api_keys')}>
                  <thead>
                    <tr className="border-b text-left">
                      <th className="py-2 px-3 font-medium">ID</th>
                      <th className="py-2 px-3 font-medium">{t('name')}</th>
                      <th className="py-2 px-3 font-medium">{t('owner')}</th>
                      <th className="py-2 px-3 font-medium">{t('permissions')}</th>
                      <th className="py-2 px-3 font-medium">{t('rate_limit')}</th>
                      <th className="py-2 px-3 font-medium">{t('status')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {keys.length === 0 ? (
                      <tr><td colSpan={6} className="py-8 text-center text-zinc-400">{t('no_keys')}</td></tr>
                    ) : keys.map((k) => (
                      <tr key={k.id} className="border-b hover:bg-zinc-50">
                        <td className="py-2 px-3">{k.id}</td>
                        <td className="py-2 px-3 font-medium">{k.name}</td>
                        <td className="py-2 px-3">{k.owner}</td>
                        <td className="py-2 px-3"><Badge variant="outline">{k.permissions}</Badge></td>
                        <td className="py-2 px-3">{k.rate_limit}/min</td>
                        <td className="py-2 px-3">
                          <Badge className={k.is_active ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}>
                            {k.is_active ? t('active') : t('inactive')}
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

        <TabsContent value="usage" className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-base">{t('api_usage')}</CardTitle></CardHeader>
            <CardContent>
              {usage ? (
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                  {Object.entries(usage).map(([k, v]) => (
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

        <TabsContent value="examples" className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-base">{t('code_examples')}</CardTitle></CardHeader>
            <CardContent className="space-y-6">
              <div>
                <h3 className="font-medium text-zinc-900 mb-2">Python</h3>
                <pre className="bg-zinc-900 text-green-400 p-4 rounded-lg text-xs overflow-x-auto font-mono">
{`import requests

API_KEY = "your_api_key_here"
BASE = "${window.location.origin}/api/v1"
headers = {"X-API-Key": API_KEY}

# Get election results
results = requests.get(f"{BASE}/results", headers=headers)
print(results.json())

# Get AI anomalies
anomalies = requests.get(f"{BASE}/ai/anomalies?election_id=1", headers=headers)
print(anomalies.json())

# Get integrity score
integrity = requests.get(f"{BASE}/ai/integrity?election_id=1", headers=headers)
print(integrity.json())`}
                </pre>
              </div>
              <div>
                <h3 className="font-medium text-zinc-900 mb-2">JavaScript / Node.js</h3>
                <pre className="bg-zinc-900 text-green-400 p-4 rounded-lg text-xs overflow-x-auto font-mono">
{`const API_KEY = "your_api_key_here";
const BASE = "${window.location.origin}/api/v1";

const res = await fetch(\`\${BASE}/results\`, {
  headers: { "X-API-Key": API_KEY }
});
const data = await res.json();
// Process results
document.getElementById("output").textContent = JSON.stringify(data, null, 2);`}
                </pre>
              </div>
              <div>
                <h3 className="font-medium text-zinc-900 mb-2">cURL</h3>
                <pre className="bg-zinc-900 text-green-400 p-4 rounded-lg text-xs overflow-x-auto font-mono">
{`# List results
curl -H "X-API-Key: YOUR_KEY" ${window.location.origin}/api/v1/results

# Get anomalies
curl -H "X-API-Key: YOUR_KEY" ${window.location.origin}/api/v1/ai/anomalies?election_id=1

# Get OpenAPI spec
curl ${window.location.origin}/api/v1/docs`}
                </pre>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
