import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Users, AlertTriangle, FileWarning, Bell, QrCode, Shield, CheckCircle, Send, Search, Plus } from 'lucide-react';

export default function StakeholderPage() {
  const [stats, setStats] = useState<any>(null);
  const [stakeholders, setStakeholders] = useState<any>(null);
  const [incidents, setIncidents] = useState<any>(null);
  const [grievances, setGrievances] = useState<any>(null);
  const [notifications, setNotifications] = useState<any>(null);
  const [tab, setTab] = useState('overview');
  const [search, setSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [createForm, setCreateForm] = useState({ org_name: '', type: 'political_party', contact_person: '', email: '', phone: '', state_code: '' });
  const [showGrievanceForm, setShowGrievanceForm] = useState(false);
  const [grievanceForm, setGrievanceForm] = useState({ category: 'process', description: '' });

  useEffect(() => {
    api.getStakeholderStats().then(setStats).catch(() => {});
    api.getStakeholders().then(setStakeholders).catch(() => {});
    api.getStakeholderIncidents().then(setIncidents).catch(() => {});
    api.getGrievances().then(setGrievances).catch(() => {});
    api.getPushNotifications().then(setNotifications).catch(() => {});
  }, []);

  const [notifForm, setNotifForm] = useState({ title: '', body: '' });
  const [sending, setSending] = useState(false);

  const handleResolve = async (id: number) => {
    const resolution = prompt('Enter resolution details:');
    if (!resolution) return;
    await api.resolveGrievance(id, resolution);
    api.getGrievances().then(setGrievances);
  };

  const handleCreateStakeholder = async () => {
    if (!createForm.org_name) return;
    try {
      await api.createStakeholder(createForm);
      setShowCreate(false);
      setCreateForm({ org_name: '', type: 'political_party', contact_person: '', email: '', phone: '', state_code: '' });
      api.getStakeholders().then(setStakeholders);
      api.getStakeholderStats().then(setStats);
    } catch { void 0; }
  };

  const handleCreateGrievance = async () => {
    if (!grievanceForm.description) return;
    try {
      await api.createGrievance(grievanceForm);
      setShowGrievanceForm(false);
      setGrievanceForm({ category: 'process', description: '' });
      api.getGrievances().then(setGrievances);
    } catch { void 0; }
  };

  const handleSendNotification = async () => {
    if (!notifForm.title || !notifForm.body) return;
    setSending(true);
    try {
      await api.sendNotification({ title: notifForm.title, body: notifForm.body, target_type: 'all' });
      setNotifForm({ title: '', body: '' });
      api.getPushNotifications().then(setNotifications);
    } catch (e) { void e; }
    setSending(false);
  };

  const sevColors: Record<string, string> = {
    critical: 'bg-red-100 text-red-700',
    high: 'bg-orange-100 text-orange-700',
    medium: 'bg-amber-100 text-amber-700',
    low: 'bg-green-100 text-green-700',
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h2 className="text-2xl font-bold">Stakeholder Engagement</h2>
          <p className="text-zinc-500 text-sm">Unified dashboard for parties, observers, media, and agents</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
            <Input placeholder="Search stakeholders..." value={search} onChange={e => setSearch(e.target.value)} className="pl-8 w-52" />
          </div>
          <Dialog open={showCreate} onOpenChange={setShowCreate}>
            <DialogTrigger asChild>
              <Button className="bg-green-700 hover:bg-green-800 gap-1"><Plus className="w-4 h-4" /> Register</Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Register Stakeholder</DialogTitle></DialogHeader>
              <div className="space-y-3">
                <Input placeholder="Organization Name" value={createForm.org_name} onChange={e => setCreateForm({ ...createForm, org_name: e.target.value })} />
                <Select value={createForm.type} onValueChange={v => setCreateForm({ ...createForm, type: v })}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="political_party">Political Party</SelectItem>
                    <SelectItem value="observer_org">Observer Organization</SelectItem>
                    <SelectItem value="media">Media</SelectItem>
                    <SelectItem value="civil_society">Civil Society</SelectItem>
                    <SelectItem value="international">International Observer</SelectItem>
                  </SelectContent>
                </Select>
                <Input placeholder="Contact Person" value={createForm.contact_person} onChange={e => setCreateForm({ ...createForm, contact_person: e.target.value })} />
                <Input placeholder="Email" value={createForm.email} onChange={e => setCreateForm({ ...createForm, email: e.target.value })} />
                <Input placeholder="Phone" value={createForm.phone} onChange={e => setCreateForm({ ...createForm, phone: e.target.value })} />
                <Input placeholder="State Code (e.g. LA)" value={createForm.state_code} onChange={e => setCreateForm({ ...createForm, state_code: e.target.value })} />
                <Button onClick={handleCreateStakeholder} className="w-full bg-green-700 hover:bg-green-800">Register</Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </div>

      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
          {[
            { label: 'Total Stakeholders', value: stats.total_stakeholders, icon: Users, color: 'blue' },
            { label: 'Approved', value: stats.approved, icon: Shield, color: 'green' },
            { label: 'Pending', value: stats.pending, icon: QrCode, color: 'amber' },
            { label: 'Incidents', value: stats.incidents?.total, icon: AlertTriangle, color: 'red' },
            { label: 'Critical', value: stats.incidents?.critical, icon: AlertTriangle, color: 'rose' },
            { label: 'Grievances', value: stats.grievances?.total, icon: FileWarning, color: 'purple' },
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

      {stats?.by_type && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Stakeholders by Type</CardTitle></CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-3">
              {stats.by_type.map((t: any, i: number) => (
                <div key={i} className="flex items-center gap-2 px-3 py-2 border rounded-lg">
                  <span className="text-sm capitalize">{t.type?.replace('_', ' ')}</span>
                  <Badge variant="outline">{t.count}</Badge>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="overview">Stakeholders</TabsTrigger>
          <TabsTrigger value="incidents">Incidents</TabsTrigger>
          <TabsTrigger value="grievances">Grievances</TabsTrigger>
          <TabsTrigger value="notifications">Notifications</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <Card>
            <CardContent className="pt-4">
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-4">Name</th><th className="pb-2 pr-4">Organization</th><th className="pb-2 pr-4">Type</th><th className="pb-2 pr-4">Credential</th><th className="pb-2">Status</th>
                  </tr></thead>
                  <tbody>
                    {stakeholders?.stakeholders?.filter((s: any) => {
                      if (!search) return true;
                      const q = search.toLowerCase();
                      return s.name?.toLowerCase().includes(q) || s.organization?.toLowerCase().includes(q) || s.type?.toLowerCase().includes(q);
                    }).map((s: any) => (
                      <tr key={s.id} className="border-b border-zinc-100">
                        <td className="py-2 pr-4 font-medium">{s.name}</td>
                        <td className="py-2 pr-4 text-xs">{s.organization}</td>
                        <td className="py-2 pr-4"><Badge variant="outline" className="text-xs capitalize">{s.type?.replace('_', ' ')}</Badge></td>
                        <td className="py-2 pr-4 font-mono text-xs">{s.credential_id}</td>
                        <td className="py-2">
                          <Badge variant={s.accreditation_status === 'approved' ? 'default' : s.accreditation_status === 'suspended' ? 'destructive' : 'outline'} className="text-xs">
                            {s.accreditation_status}
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

        <TabsContent value="incidents">
          <Card>
            <CardHeader><CardTitle className="text-sm">Incident Reports with Geolocation</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-3">
                {incidents?.incidents?.map((inc: any) => (
                  <div key={inc.id} className="p-3 border rounded-lg">
                    <div className="flex items-start justify-between mb-2">
                      <div className="flex items-center gap-2">
                        <AlertTriangle className={`w-4 h-4 ${inc.severity === 'critical' ? 'text-red-600' : inc.severity === 'high' ? 'text-orange-600' : 'text-amber-600'}`} />
                        <span className="font-medium text-sm capitalize">{inc.type?.replace('_', ' ')}</span>
                        <Badge className={`text-xs ${sevColors[inc.severity] || ''}`}>{inc.severity}</Badge>
                      </div>
                      <Badge variant={inc.status === 'resolved' ? 'default' : 'outline'} className="text-xs">{inc.status}</Badge>
                    </div>
                    <p className="text-sm text-zinc-600 mb-1">{inc.description}</p>
                    <div className="flex items-center gap-4 text-xs text-zinc-500">
                      <span>Reporter: {inc.reporter}</span>
                      {inc.latitude && <span>Location: {inc.latitude?.toFixed(4)}, {inc.longitude?.toFixed(4)}</span>}
                      <span>{new Date(inc.reported_at).toLocaleString()}</span>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="grievances">
          <Card className="mb-4">
            <CardContent className="pt-4">
              <div className="flex items-center justify-between">
                <h4 className="text-sm font-semibold">Grievance Redressal Tracker</h4>
                <Dialog open={showGrievanceForm} onOpenChange={setShowGrievanceForm}>
                  <DialogTrigger asChild>
                    <Button size="sm" variant="outline" className="gap-1"><Plus className="w-3.5 h-3.5" /> File Grievance</Button>
                  </DialogTrigger>
                  <DialogContent>
                    <DialogHeader><DialogTitle>File Grievance</DialogTitle></DialogHeader>
                    <div className="space-y-3">
                      <Select value={grievanceForm.category} onValueChange={v => setGrievanceForm({ ...grievanceForm, category: v })}>
                        <SelectTrigger><SelectValue placeholder="Category" /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="process">Process</SelectItem>
                          <SelectItem value="access">Access</SelectItem>
                          <SelectItem value="intimidation">Intimidation</SelectItem>
                          <SelectItem value="results">Results</SelectItem>
                          <SelectItem value="other">Other</SelectItem>
                        </SelectContent>
                      </Select>
                      <Input placeholder="Describe the grievance..." value={grievanceForm.description} onChange={e => setGrievanceForm({ ...grievanceForm, description: e.target.value })} />
                      <Button onClick={handleCreateGrievance} className="w-full">Submit</Button>
                    </div>
                  </DialogContent>
                </Dialog>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-4">ID</th><th className="pb-2 pr-4">Stakeholder</th><th className="pb-2 pr-4">Type</th><th className="pb-2 pr-4">Subject</th><th className="pb-2 pr-4">Priority</th><th className="pb-2 pr-4">Status</th><th className="pb-2">Action</th>
                  </tr></thead>
                  <tbody>
                    {grievances?.grievances?.map((g: any) => (
                      <tr key={g.id} className="border-b border-zinc-100">
                        <td className="py-2 pr-4">#{g.id}</td>
                        <td className="py-2 pr-4">{g.stakeholder}</td>
                        <td className="py-2 pr-4 text-xs capitalize">{g.type?.replace('_', ' ')}</td>
                        <td className="py-2 pr-4 text-xs">{g.subject}</td>
                        <td className="py-2 pr-4">
                          <Badge className={`text-xs ${g.priority === 'urgent' ? 'bg-red-100 text-red-700' : g.priority === 'high' ? 'bg-orange-100 text-orange-700' : 'bg-zinc-100 text-zinc-700'}`}>{g.priority}</Badge>
                        </td>
                        <td className="py-2 pr-4">
                          <Badge variant={g.status === 'resolved' ? 'default' : 'outline'} className="text-xs">{g.status?.replace('_', ' ')}</Badge>
                        </td>
                        <td className="py-2">
                          {g.status !== 'resolved' && (
                            <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => handleResolve(g.id)}>
                              <CheckCircle className="w-3.5 h-3.5 mr-1" /> Resolve
                            </Button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="notifications">
          <Card className="mb-4">
            <CardContent className="pt-4">
              <h4 className="text-sm font-semibold mb-2 flex items-center gap-2"><Send className="w-4 h-4" /> Send Notification</h4>
              <div className="flex gap-2">
                <Input placeholder="Title" value={notifForm.title} onChange={e => setNotifForm({ ...notifForm, title: e.target.value })} className="flex-1" />
                <Input placeholder="Message body" value={notifForm.body} onChange={e => setNotifForm({ ...notifForm, body: e.target.value })} className="flex-[2]" />
                <Button size="sm" onClick={handleSendNotification} disabled={sending || !notifForm.title || !notifForm.body}>
                  {sending ? 'Sending...' : 'Send'}
                </Button>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardHeader><CardTitle className="text-sm">Push Notifications</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-3">
                {notifications?.notifications?.map((n: any) => (
                  <div key={n.id} className="flex items-start gap-3 p-3 border rounded-lg">
                    <div className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 ${
                      n.type === 'emergency' ? 'bg-red-50' : n.type === 'alert' ? 'bg-amber-50' : 'bg-blue-50'
                    }`}>
                      <Bell className={`w-4 h-4 ${
                        n.type === 'emergency' ? 'text-red-600' : n.type === 'alert' ? 'text-amber-600' : 'text-blue-600'
                      }`} />
                    </div>
                    <div className="flex-1">
                      <div className="flex items-center gap-2 mb-1">
                        <span className="font-medium text-sm">{n.title}</span>
                        <Badge variant="outline" className="text-xs capitalize">{n.type}</Badge>
                        <Badge variant="outline" className="text-xs">{n.target_type}{n.target_value ? `: ${n.target_value}` : ''}</Badge>
                      </div>
                      <p className="text-sm text-zinc-600">{n.body}</p>
                      <p className="text-xs text-zinc-400 mt-1">
                        {n.read_count}/{n.total_recipients} read &middot; {new Date(n.sent_at).toLocaleString()}
                      </p>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
