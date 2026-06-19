import { useState, useEffect, useCallback } from 'react';
import { api } from '@/lib/api';
import { DEMO_USERS } from '@/lib/demo-data';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Users, Plus, Search, Shield, Trash2, Edit2, Key, Activity } from 'lucide-react';

interface User {
  id: number;
  username: string;
  full_name: string;
  role: string;
  staff_id: string;
  state_code: string;
  kyc_status: string;
  created_at: string;
}

interface Session {
  id: string;
  user_id: number;
  ip_address: string;
  user_agent: string;
  created_at: string;
  expires_at: string;
}

const ROLES = ['admin', 'presiding_officer', 'collation_officer', 'observer', 'public'];

const roleColors: Record<string, string> = {
  admin: 'bg-red-100 text-red-800',
  presiding_officer: 'bg-green-100 text-green-800',
  collation_officer: 'bg-blue-100 text-blue-800',
  observer: 'bg-purple-100 text-purple-800',
  public: 'bg-zinc-100 text-zinc-600',
};

type View = 'users' | 'sessions' | 'create' | 'edit';

export default function UserManagementPage() {
  const [users, setUsers] = useState<User[]>([]);
  const [total, setTotal] = useState(0);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loading, setLoading] = useState(true);
  const [view, setView] = useState<View>('users');
  const [search, setSearch] = useState('');
  const [filterRole, setFilterRole] = useState('all');
  const [selected, setSelected] = useState<User | null>(null);
  const [form, setForm] = useState({ username: '', full_name: '', password: '', role: 'presiding_officer', staff_id: '', state_code: '' });
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');
  const [showDelete, setShowDelete] = useState<number | null>(null);

  const loadUsers = useCallback(async () => {
    setLoading(true);
    try {
      const params: Record<string, string> = { limit: '100' };
      if (search) params.search = search;
      if (filterRole !== 'all') params.role = filterRole;
      const data = await api.getUsers(params) as { users: User[]; total: number };
      setUsers(Array.isArray(data.users) ? data.users : []);
      setTotal(data.total || 0);
    } catch {
      setUsers(DEMO_USERS as unknown as User[]);
      setTotal(DEMO_USERS.length);
    }
    setLoading(false);
  }, [search, filterRole]);

  const loadSessions = useCallback(async () => {
    try {
      const data = await api.getAuthSessions() as unknown as Session[];
      setSessions(Array.isArray(data) ? data : []);
    } catch { setSessions([]); }
  }, []);

  useEffect(() => { loadUsers(); }, [loadUsers]);
  useEffect(() => { if (view === 'sessions') loadSessions(); }, [view, loadSessions]);

  const handleCreate = async () => {
    if (!form.username || !form.password || !form.role) { setMsg('Username, password, and role are required'); return; }
    setSaving(true);
    try {
      await api.createUser(form);
      setMsg('User created successfully');
      setView('users');
      loadUsers();
    } catch (e: unknown) { setMsg(`Error: ${(e as Error).message}`); }
    setSaving(false);
  };

  const handleUpdate = async () => {
    if (!selected) return;
    setSaving(true);
    try {
      await api.updateUser(selected.id, { full_name: form.full_name, role: form.role, staff_id: form.staff_id, state_code: form.state_code });
      setMsg('User updated');
      setView('users');
      loadUsers();
    } catch (e: unknown) { setMsg(`Error: ${(e as Error).message}`); }
    setSaving(false);
  };

  const handleDelete = async (id: number) => {
    try {
      await api.deleteUser(id);
      setShowDelete(null);
      loadUsers();
    } catch (e: unknown) { setMsg(`Delete failed: ${(e as Error).message}`); }
  };

  const handleRevokeSession = async (sessionId?: string) => {
    try {
      if (sessionId) { await api.revokeSession(sessionId); }
      else { await api.revokeAllSessions(); }
      loadSessions();
    } catch (e: unknown) { setMsg(`Error: ${(e as Error).message}`); }
  };

  const openEdit = (u: User) => {
    setSelected(u);
    setForm({ username: u.username, full_name: u.full_name, password: '', role: u.role, staff_id: u.staff_id || '', state_code: u.state_code || '' });
    setView('edit');
  };

  if (loading && view === 'users') return <div className="flex items-center justify-center h-64"><Activity className="w-6 h-6 animate-spin text-green-700" /></div>;

  if (view === 'create' || view === 'edit') {
    return (
      <div className="space-y-4">
        <Button variant="ghost" size="sm" onClick={() => { setView('users'); setMsg(''); }}>← Back</Button>
        <Card>
          <CardContent className="pt-6 space-y-4">
            <h2 className="text-lg font-semibold">{view === 'create' ? 'Create New User' : `Edit User: ${selected?.username}`}</h2>
            {msg && <p className="text-sm text-blue-600">{msg}</p>}
            <div className="grid gap-3 md:grid-cols-2">
              {view === 'create' && (
                <div className="space-y-1">
                  <label className="text-sm font-medium">Username</label>
                  <Input value={form.username} onChange={e => setForm({ ...form, username: e.target.value })} placeholder="e.g. john.doe" />
                </div>
              )}
              <div className="space-y-1">
                <label className="text-sm font-medium">Full Name</label>
                <Input value={form.full_name} onChange={e => setForm({ ...form, full_name: e.target.value })} placeholder="e.g. John Doe" />
              </div>
              {view === 'create' && (
                <div className="space-y-1">
                  <label className="text-sm font-medium">Password</label>
                  <Input type="password" value={form.password} onChange={e => setForm({ ...form, password: e.target.value })} />
                </div>
              )}
              <div className="space-y-1">
                <label className="text-sm font-medium">Role</label>
                <Select value={form.role} onValueChange={v => setForm({ ...form, role: v })}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {ROLES.map(r => <SelectItem key={r} value={r}>{r.split('_').map(w => w.charAt(0).toUpperCase() + w.slice(1)).join(' ')}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <label className="text-sm font-medium">Staff ID</label>
                <Input value={form.staff_id} onChange={e => setForm({ ...form, staff_id: e.target.value })} placeholder="e.g. INEC-001" />
              </div>
              <div className="space-y-1">
                <label className="text-sm font-medium">State Code</label>
                <Input value={form.state_code} onChange={e => setForm({ ...form, state_code: e.target.value })} placeholder="e.g. KN, LA" />
              </div>
            </div>
            <Button onClick={view === 'create' ? handleCreate : handleUpdate} disabled={saving} className="bg-green-700 hover:bg-green-800">
              {saving ? 'Saving...' : view === 'create' ? 'Create User' : 'Save Changes'}
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-2">
          <Button variant={view === 'users' ? 'default' : 'outline'} size="sm" onClick={() => setView('users')} className="gap-1">
            <Users className="w-4 h-4" /> Users
          </Button>
          <Button variant={view === 'sessions' ? 'default' : 'outline'} size="sm" onClick={() => setView('sessions')} className="gap-1">
            <Shield className="w-4 h-4" /> Sessions
          </Button>
        </div>
        {view === 'users' && (
          <Button onClick={() => { setForm({ username: '', full_name: '', password: '', role: 'presiding_officer', staff_id: '', state_code: '' }); setMsg(''); setView('create'); }} className="bg-green-700 hover:bg-green-800 gap-1">
            <Plus className="w-4 h-4" /> Add User
          </Button>
        )}
      </div>

      {msg && <p className="text-sm text-blue-600 bg-blue-50 p-2 rounded">{msg}</p>}

      {view === 'users' && (
        <>
          <div className="flex items-center gap-3 flex-wrap">
            <div className="relative flex-1 min-w-[200px]">
              <Search className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-zinc-400" />
              <Input placeholder="Search by name or username..." value={search} onChange={e => setSearch(e.target.value)} className="pl-9" />
            </div>
            <Select value={filterRole} onValueChange={setFilterRole}>
              <SelectTrigger className="w-44"><SelectValue placeholder="All Roles" /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Roles</SelectItem>
                {ROLES.map(r => <SelectItem key={r} value={r}>{r.split('_').map(w => w.charAt(0).toUpperCase() + w.slice(1)).join(' ')}</SelectItem>)}
              </SelectContent>
            </Select>
            <Badge variant="outline">{total} users</Badge>
          </div>

          <Card>
            <CardContent className="overflow-x-auto p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Username</TableHead>
                    <TableHead>Full Name</TableHead>
                    <TableHead>Role</TableHead>
                    <TableHead>Staff ID</TableHead>
                    <TableHead>State</TableHead>
                    <TableHead>KYC</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {users.map(u => (
                    <TableRow key={u.id}>
                      <TableCell className="font-medium">{u.username}</TableCell>
                      <TableCell>{u.full_name}</TableCell>
                      <TableCell><Badge className={roleColors[u.role] || 'bg-zinc-100 text-zinc-600'}>{u.role?.replace(/_/g, ' ')}</Badge></TableCell>
                      <TableCell className="font-mono text-xs">{u.staff_id || '-'}</TableCell>
                      <TableCell>{u.state_code || '-'}</TableCell>
                      <TableCell><Badge variant="outline">{u.kyc_status || 'none'}</Badge></TableCell>
                      <TableCell className="text-xs text-zinc-400">{u.created_at ? new Date(u.created_at).toLocaleDateString() : '-'}</TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button variant="ghost" size="sm" onClick={() => openEdit(u)}><Edit2 className="w-3.5 h-3.5" /></Button>
                          <Dialog open={showDelete === u.id} onOpenChange={open => setShowDelete(open ? u.id : null)}>
                            <DialogTrigger asChild>
                              <Button variant="ghost" size="sm" className="text-red-600 hover:text-red-700"><Trash2 className="w-3.5 h-3.5" /></Button>
                            </DialogTrigger>
                            <DialogContent>
                              <DialogHeader><DialogTitle>Delete {u.username}?</DialogTitle></DialogHeader>
                              <p className="text-sm text-zinc-500">This action cannot be undone.</p>
                              <div className="flex gap-2 justify-end">
                                <Button variant="outline" onClick={() => setShowDelete(null)}>Cancel</Button>
                                <Button variant="destructive" onClick={() => handleDelete(u.id)}>Delete</Button>
                              </div>
                            </DialogContent>
                          </Dialog>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                  {users.length === 0 && (
                    <TableRow><TableCell colSpan={8} className="text-center py-8 text-zinc-500">No users found</TableCell></TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      )}

      {view === 'sessions' && (
        <Card>
          <CardContent className="pt-4">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-semibold">Active Sessions ({sessions.length})</h2>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={() => handleRevokeSession()} className="text-red-600 gap-1"><Key className="w-3.5 h-3.5" /> Revoke All</Button>
                <Button variant="outline" size="sm" onClick={async () => { const res = await api.rotateAPIKey() as unknown as { api_key: string }; setMsg(`New API key: ${res.api_key}`); }} className="gap-1"><Key className="w-3.5 h-3.5" /> Rotate API Key</Button>
              </div>
            </div>
            {sessions.length === 0 ? (
              <p className="text-zinc-500 text-center py-8">No active sessions</p>
            ) : (
              <Table>
                <TableHeader><TableRow>
                  <TableHead>Session ID</TableHead>
                  <TableHead>IP Address</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="text-right">Action</TableHead>
                </TableRow></TableHeader>
                <TableBody>
                  {sessions.map(s => (
                    <TableRow key={s.id}>
                      <TableCell className="font-mono text-xs">{s.id.slice(0, 12)}...</TableCell>
                      <TableCell>{s.ip_address}</TableCell>
                      <TableCell className="text-xs">{new Date(s.created_at).toLocaleString()}</TableCell>
                      <TableCell className="text-xs">{new Date(s.expires_at).toLocaleString()}</TableCell>
                      <TableCell className="text-right">
                        <Button variant="ghost" size="sm" onClick={() => handleRevokeSession(s.id)} className="text-red-600">Revoke</Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
