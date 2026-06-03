import { useState, useEffect, useCallback } from 'react';
import { api } from '@/lib/api';

interface Webhook {
  id: number;
  url: string;
  events: string[];
  active: boolean;
  created_at: string;
  last_triggered?: string;
  failure_count?: number;
}

const EVENT_TYPES = [
  'result.submitted', 'result.validated', 'result.finalized', 'result.disputed',
  'election.created', 'election.status_changed',
  'incident.created', 'incident.updated',
  'collation.completed', 'anomaly.detected',
];

export default function WebhookManagementPage() {
  const [webhooks, setWebhooks] = useState<Webhook[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ url: '', events: [] as string[], secret: '' });
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try { const data = await api.getWebhooks() as unknown as Webhook[]; setWebhooks(Array.isArray(data) ? data : []); } catch { setWebhooks([]); }
    setLoading(false);
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleCreate = async () => {
    if (!form.url || form.events.length === 0) return;
    try {
      await api.createWebhook(form.url, form.events, form.secret || undefined);
      setShowForm(false); setForm({ url: '', events: [], secret: '' }); load();
    } catch {}
  };

  const handleDelete = async (id: number) => {
    if (!confirm('Delete this webhook?')) return;
    try { await api.deleteWebhook(id); load(); } catch {}
  };

  const toggleEvent = (event: string) => {
    setForm(f => ({ ...f, events: f.events.includes(event) ? f.events.filter(e => e !== event) : [...f.events, event] }));
  };

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold dark:text-white">Webhook Management</h1>
        <button onClick={() => setShowForm(!showForm)} className="bg-green-600 text-white px-4 py-2 rounded hover:bg-green-700">
          {showForm ? 'Cancel' : '+ New Webhook'}
        </button>
      </div>

      {showForm && (
        <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
          <h2 className="text-lg font-semibold mb-4 dark:text-white">Create Webhook</h2>
          <div className="space-y-4">
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="Endpoint URL (https://...)" value={form.url} onChange={e => setForm({ ...form, url: e.target.value })} />
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="Secret (optional, for HMAC signing)" value={form.secret} onChange={e => setForm({ ...form, secret: e.target.value })} />
            <div>
              <p className="text-sm font-medium mb-2 dark:text-gray-300">Events to subscribe:</p>
              <div className="flex flex-wrap gap-2">
                {EVENT_TYPES.map(ev => (
                  <button key={ev} onClick={() => toggleEvent(ev)}
                    className={`px-3 py-1 rounded text-sm ${form.events.includes(ev) ? 'bg-green-600 text-white' : 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300'}`}>
                    {ev}
                  </button>
                ))}
              </div>
            </div>
            <button onClick={handleCreate} className="bg-green-600 text-white px-6 py-2 rounded hover:bg-green-700">Create</button>
          </div>
        </div>
      )}

      {loading ? <p className="text-gray-500">Loading...</p> : webhooks.length === 0 ? (
        <div className="bg-white dark:bg-gray-800 rounded-lg p-8 text-center shadow">
          <p className="text-gray-500 dark:text-gray-400">No webhooks configured. Create one to receive real-time event notifications.</p>
        </div>
      ) : (
        <div className="space-y-4">
          {webhooks.map(wh => (
            <div key={wh.id} className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow flex justify-between items-start">
              <div>
                <p className="font-mono text-sm dark:text-white">{wh.url}</p>
                <div className="flex flex-wrap gap-1 mt-2">
                  {(wh.events || []).map(ev => (
                    <span key={ev} className="bg-blue-100 dark:bg-blue-900/30 text-blue-800 dark:text-blue-300 text-xs px-2 py-0.5 rounded">{ev}</span>
                  ))}
                </div>
                <p className="text-xs text-gray-400 mt-1">Created: {new Date(wh.created_at).toLocaleDateString()}</p>
              </div>
              <button onClick={() => handleDelete(wh.id)} className="text-red-600 hover:text-red-800 text-sm">Delete</button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
