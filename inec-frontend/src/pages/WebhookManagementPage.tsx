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
  const [search, setSearch] = useState('');
  const [editId, setEditId] = useState<number | null>(null);
  const [editForm, setEditForm] = useState({ url: '', events: [] as string[], active: true });

  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const data = await api.getWebhooks() as unknown as { webhooks?: Webhook[] } | Webhook[];
      const list = Array.isArray(data) ? data : (data?.webhooks || []);
      setWebhooks(list);
    } catch { setWebhooks([]); }
    setLoading(false);
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleCreate = async () => {
    if (!form.url || form.events.length === 0) return;
    try {
      await api.createWebhook(form.url, form.events, form.secret || undefined);
      setShowForm(false); setForm({ url: '', events: [], secret: '' }); setError(''); load();
    } catch (e: unknown) { setError(`Create failed: ${(e as Error).message}`); }
  };

  const handleDelete = async (id: number) => {
    if (!confirm('Delete this webhook?')) return;
    try { await api.deleteWebhook(id); setError(''); load(); } catch (e: unknown) { setError(`Delete failed: ${(e as Error).message}`); }
  };

  const handleUpdate = async () => {
    if (editId === null) return;
    try {
      await api.updateWebhook(editId, { url: editForm.url, events: editForm.events, active: editForm.active });
      setEditId(null); setError(''); load();
    } catch (e: unknown) { setError(`Update failed: ${(e as Error).message}`); }
  };

  const openEdit = (wh: Webhook) => {
    setEditId(wh.id);
    setEditForm({ url: wh.url, events: [...(wh.events || [])], active: wh.active });
  };

  const toggleEvent = (event: string) => {
    setForm(f => ({ ...f, events: f.events.includes(event) ? f.events.filter(e => e !== event) : [...f.events, event] }));
  };

  const toggleEditEvent = (event: string) => {
    setEditForm(f => ({ ...f, events: f.events.includes(event) ? f.events.filter(e => e !== event) : [...f.events, event] }));
  };

  const filtered = webhooks.filter(wh => {
    if (!search) return true;
    const s = search.toLowerCase();
    return wh.url?.toLowerCase().includes(s) || wh.events?.some(ev => ev.toLowerCase().includes(s));
  });

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center flex-wrap gap-3">
        <h1 className="text-2xl font-bold dark:text-white">Webhook Management</h1>
        <div className="flex items-center gap-2">
          <input className="border dark:border-gray-600 rounded px-3 py-1.5 text-sm dark:bg-gray-700 dark:text-white w-48" placeholder="Search webhooks..." value={search} onChange={e => setSearch(e.target.value)} />
          <button onClick={() => setShowForm(!showForm)} className="bg-green-600 text-white px-4 py-2 rounded hover:bg-green-700 text-sm">
            {showForm ? 'Cancel' : '+ New Webhook'}
          </button>
        </div>
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

      {editId !== null && (
        <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow border-2 border-blue-500">
          <h2 className="text-lg font-semibold mb-4 dark:text-white">Edit Webhook #{editId}</h2>
          <div className="space-y-4">
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="Endpoint URL" value={editForm.url} onChange={e => setEditForm({ ...editForm, url: e.target.value })} />
            <label className="flex items-center gap-2 text-sm dark:text-gray-300">
              <input type="checkbox" checked={editForm.active} onChange={e => setEditForm({ ...editForm, active: e.target.checked })} /> Active
            </label>
            <div>
              <p className="text-sm font-medium mb-2 dark:text-gray-300">Events:</p>
              <div className="flex flex-wrap gap-2">
                {EVENT_TYPES.map(ev => (
                  <button key={ev} onClick={() => toggleEditEvent(ev)}
                    className={`px-3 py-1 rounded text-sm ${editForm.events.includes(ev) ? 'bg-blue-600 text-white' : 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300'}`}>
                    {ev}
                  </button>
                ))}
              </div>
            </div>
            <div className="flex gap-2">
              <button onClick={handleUpdate} className="bg-blue-600 text-white px-6 py-2 rounded hover:bg-blue-700">Save</button>
              <button onClick={() => setEditId(null)} className="bg-gray-300 dark:bg-gray-600 text-gray-700 dark:text-gray-200 px-6 py-2 rounded hover:bg-gray-400">Cancel</button>
            </div>
          </div>
        </div>
      )}

      {error && <p className="text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 p-3 rounded">{error}</p>}

      {loading ? <p className="text-gray-500">Loading...</p> : filtered.length === 0 ? (
        <div className="bg-white dark:bg-gray-800 rounded-lg p-8 text-center shadow">
          <p className="text-gray-500 dark:text-gray-400">{webhooks.length === 0 ? 'No webhooks configured. Create one to receive real-time event notifications.' : 'No webhooks match your search.'}</p>
        </div>
      ) : (
        <div className="space-y-4">
          {filtered.map(wh => (
            <div key={wh.id} className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow flex justify-between items-start">
              <div>
                <div className="flex items-center gap-2 mb-1">
                  <p className="font-mono text-sm dark:text-white">{wh.url}</p>
                  <span className={`text-xs px-2 py-0.5 rounded ${wh.active ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}`}>{wh.active ? 'Active' : 'Inactive'}</span>
                </div>
                <div className="flex flex-wrap gap-1 mt-2">
                  {(wh.events || []).map(ev => (
                    <span key={ev} className="bg-blue-100 dark:bg-blue-900/30 text-blue-800 dark:text-blue-300 text-xs px-2 py-0.5 rounded">{ev}</span>
                  ))}
                </div>
                <p className="text-xs text-gray-400 mt-1">
                  Created: {new Date(wh.created_at).toLocaleDateString()}
                  {wh.last_triggered && <> &middot; Last triggered: {new Date(wh.last_triggered).toLocaleString()}</>}
                  {(wh.failure_count ?? 0) > 0 && <> &middot; <span className="text-red-500">Failures: {wh.failure_count}</span></>}
                </p>
              </div>
              <div className="flex gap-2 shrink-0">
                <button onClick={() => openEdit(wh)} className="text-blue-600 hover:text-blue-800 text-sm">Edit</button>
                <button onClick={() => handleDelete(wh.id)} className="text-red-600 hover:text-red-800 text-sm">Delete</button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
