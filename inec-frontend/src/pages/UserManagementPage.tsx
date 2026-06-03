import { useState, useEffect, useCallback } from 'react';
import { api } from '@/lib/api';

interface User {
  id: number;
  username: string;
  full_name: string;
  role: string;
  staff_id: string;
  created_at: string;
  last_login?: string;
}

interface Session {
  id: string;
  user_id: number;
  ip_address: string;
  user_agent: string;
  created_at: string;
  expires_at: string;
}

const ROLES = ['admin', 'officer', 'observer', 'public'];

export default function UserManagementPage() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [promoteForm, setPromoteForm] = useState({ userId: '', role: 'officer' });
  const [promoteMsg, setPromoteMsg] = useState('');
  const [revokeMsg, setRevokeMsg] = useState('');

  const loadSessions = useCallback(async () => {
    try { const data = await api.getAuthSessions() as unknown as Session[]; setSessions(Array.isArray(data) ? data : []); } catch {}
  }, []);

  useEffect(() => { loadSessions(); }, [loadSessions]);

  const handlePromote = async () => {
    if (!promoteForm.userId) return;
    try {
      await api.promoteUser(parseInt(promoteForm.userId), promoteForm.role);
      setPromoteMsg(`User ${promoteForm.userId} promoted to ${promoteForm.role}`);
      setPromoteForm({ userId: '', role: 'officer' });
    } catch (e: unknown) { setPromoteMsg(`Error: ${(e as Error).message}`); }
  };

  const handleRevokeSession = async (sessionId?: string) => {
    try {
      if (sessionId) {
        await api.revokeSession(sessionId);
        setRevokeMsg('Session revoked');
      } else {
        await api.revokeAllSessions();
        setRevokeMsg('All sessions revoked');
      }
      loadSessions();
    } catch (e: unknown) { setRevokeMsg(`Error: ${(e as Error).message}`); }
  };

  const handleRotateAPIKey = async () => {
    try {
      const res = await api.rotateAPIKey() as unknown as { api_key: string };
      setRevokeMsg(`New API key: ${res.api_key}`);
    } catch (e: unknown) { setRevokeMsg(`Error: ${(e as Error).message}`); }
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold dark:text-white">User & Access Management</h1>

      <div className="grid md:grid-cols-2 gap-6">
        {/* Role Promotion */}
        <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
          <h2 className="text-lg font-semibold mb-4 dark:text-white">Promote User Role</h2>
          <div className="space-y-3">
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="User ID" value={promoteForm.userId} onChange={e => setPromoteForm({ ...promoteForm, userId: e.target.value })} />
            <select className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" value={promoteForm.role} onChange={e => setPromoteForm({ ...promoteForm, role: e.target.value })}>
              {ROLES.map(r => <option key={r} value={r}>{r.charAt(0).toUpperCase() + r.slice(1)}</option>)}
            </select>
            <button onClick={handlePromote} className="w-full bg-blue-600 text-white rounded py-2 hover:bg-blue-700">Promote</button>
          </div>
          {promoteMsg && <p className="mt-3 text-sm text-green-600 dark:text-green-400">{promoteMsg}</p>}
        </div>

        {/* Security Actions */}
        <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
          <h2 className="text-lg font-semibold mb-4 dark:text-white">Security Actions</h2>
          <div className="space-y-3">
            <button onClick={() => handleRevokeSession()} className="w-full bg-red-600 text-white rounded py-2 hover:bg-red-700">Revoke All Sessions</button>
            <button onClick={handleRotateAPIKey} className="w-full bg-orange-600 text-white rounded py-2 hover:bg-orange-700">Rotate API Key</button>
          </div>
          {revokeMsg && <p className="mt-3 text-sm text-blue-600 dark:text-blue-400">{revokeMsg}</p>}
        </div>
      </div>

      {/* Active Sessions */}
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
        <h2 className="text-lg font-semibold mb-4 dark:text-white">Active Sessions ({sessions.length})</h2>
        {sessions.length === 0 ? (
          <p className="text-gray-500 dark:text-gray-400">No active sessions found.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead><tr className="border-b dark:border-gray-600">
                <th className="text-left py-2 dark:text-gray-300">Session ID</th>
                <th className="text-left py-2 dark:text-gray-300">IP Address</th>
                <th className="text-left py-2 dark:text-gray-300">Created</th>
                <th className="text-left py-2 dark:text-gray-300">Expires</th>
                <th className="text-left py-2 dark:text-gray-300">Action</th>
              </tr></thead>
              <tbody>
                {sessions.map(s => (
                  <tr key={s.id} className="border-b dark:border-gray-700">
                    <td className="py-2 font-mono text-xs dark:text-gray-300">{s.id.slice(0, 12)}...</td>
                    <td className="py-2 dark:text-gray-300">{s.ip_address}</td>
                    <td className="py-2 dark:text-gray-300">{new Date(s.created_at).toLocaleString()}</td>
                    <td className="py-2 dark:text-gray-300">{new Date(s.expires_at).toLocaleString()}</td>
                    <td className="py-2"><button onClick={() => handleRevokeSession(s.id)} className="text-red-600 hover:underline text-sm">Revoke</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
