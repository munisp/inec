import { useEffect, useState, useCallback } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Progress } from '@/components/ui/progress';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell,
} from 'recharts';
import {
  Users, Megaphone, Car, HandHeart, TrendingUp, Upload,
  Plus, Search, Filter, RefreshCw, MapPin, X,
} from 'lucide-react';
import GOTVMapPage from './GOTVMapPage';
import GOTVLeaderboard from './GOTVLeaderboard';
import GOTVSegments from './GOTVSegments';
import GOTVWarRoom from './GOTVWarRoom';
import GOTVAnalytics from './GOTVAnalytics';

// ─── Nigerian States for dropdowns ─────────────────────────────────────────
const NIGERIAN_STATES = [
  'Abia','Adamawa','Akwa Ibom','Anambra','Bauchi','Bayelsa','Benue','Borno',
  'Cross River','Delta','Ebonyi','Edo','Ekiti','Enugu','FCT','Gombe','Imo',
  'Jigawa','Kaduna','Kano','Katsina','Kebbi','Kogi','Kwara','Lagos','Nasarawa',
  'Niger','Ogun','Ondo','Osun','Oyo','Plateau','Rivers','Sokoto','Taraba',
  'Yobe','Zamfara',
];

// ─── Types ─────────────────────────────────────────────────────────────────

interface DashboardData {
  party_id: number;
  total_contacts: number;
  total_volunteers: number;
  total_pledges: number;
  active_campaigns: number;
  pending_rides: number;
}

interface Campaign {
  campaign_id: string;
  name: string;
  campaign_type: string;
  status: string;
  target_state: string | null;
  total_contacts: number;
  contacts_reached: number;
  created_by: string;
  created_at: string;
}

interface Contact {
  contact_id: string;
  phone_masked: string;
  full_name: string;
  state_code: string | null;
  lga_code: string | null;
  voter_status: string;
  tags: string[];
  opted_out: boolean;
  created_at: string;
}

interface Volunteer {
  volunteer_id: string;
  full_name: string;
  role: string;
  is_active: boolean;
  has_vehicle: boolean;
  doors_knocked: number;
  calls_made: number;
  rides_given: number;
  created_at: string;
}

interface Pledge {
  pledge_id: string;
  contact_id: string;
  election_id: number | null;
  pledge_type: string;
  status: string;
  reminder_sent: boolean;
  created_at: string;
}

interface Ride {
  request_id: string;
  contact_id: string;
  volunteer_id: string | null;
  polling_unit_code: string;
  status: string;
  distance_km: number | null;
  requested_at: string;
}

// ─── Color Constants ───────────────────────────────────────────────────────

const STATUS_COLORS: Record<string, string> = {
  draft: 'bg-gray-100 text-gray-800',
  scheduled: 'bg-blue-100 text-blue-800',
  active: 'bg-green-100 text-green-800',
  paused: 'bg-yellow-100 text-yellow-800',
  completed: 'bg-purple-100 text-purple-800',
  cancelled: 'bg-red-100 text-red-800',
};

const VOTER_STATUS_COLORS: Record<string, string> = {
  unknown: 'bg-gray-100 text-gray-800',
  pledged: 'bg-blue-100 text-blue-800',
  confirmed: 'bg-green-100 text-green-800',
  declined: 'bg-red-100 text-red-800',
  unreachable: 'bg-orange-100 text-orange-800',
};

const PLEDGE_STATUS_COLORS: Record<string, string> = {
  pledged: 'bg-blue-100 text-blue-800',
  reminded: 'bg-yellow-100 text-yellow-800',
  confirmed_day_of: 'bg-green-100 text-green-800',
  fulfilled: 'bg-emerald-100 text-emerald-800',
  broken: 'bg-red-100 text-red-800',
};

const RIDE_STATUS_COLORS: Record<string, string> = {
  pending: 'bg-yellow-100 text-yellow-800',
  matched: 'bg-blue-100 text-blue-800',
  en_route: 'bg-indigo-100 text-indigo-800',
  picked_up: 'bg-green-100 text-green-800',
  dropped_off: 'bg-emerald-100 text-emerald-800',
  cancelled: 'bg-red-100 text-red-800',
  no_show: 'bg-gray-100 text-gray-800',
};

const PIE_COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899'];

// ─── Component ─────────────────────────────────────────────────────────────

type Tab = 'dashboard' | 'campaigns' | 'contacts' | 'volunteers' | 'pledges' | 'rides' | 'map' | 'leaderboard' | 'segments' | 'warroom' | 'analytics';

export default function GOTVPortalPage() {
  const [activeTab, setActiveTab] = useState<Tab>('dashboard');
  const [dashboard, setDashboard] = useState<DashboardData | null>(null);
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [contacts, setContacts] = useState<Contact[]>([]);
  const [volunteers, setVolunteers] = useState<Volunteer[]>([]);
  const [pledges, setPledges] = useState<Pledge[]>([]);
  const [rides, setRides] = useState<Ride[]>([]);
  const [loading, setLoading] = useState(true);
  const [searchTerm, setSearchTerm] = useState('');

  // Form modals state
  const [showCampaignForm, setShowCampaignForm] = useState(false);
  const [showContactForm, setShowContactForm] = useState(false);
  const [showVolunteerForm, setShowVolunteerForm] = useState(false);
  const [showPledgeForm, setShowPledgeForm] = useState(false);
  const [showRideForm, setShowRideForm] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [formSubmitting, setFormSubmitting] = useState(false);

  // Campaign form
  const [campaignName, setCampaignName] = useState('');
  const [campaignType, setCampaignType] = useState('sms');
  const [campaignState, setCampaignState] = useState('');
  const [campaignMessage, setCampaignMessage] = useState('');

  // Contact form
  const [contactName, setContactName] = useState('');
  const [contactPhone, setContactPhone] = useState('');
  const [contactState, setContactState] = useState('');
  const [contactLga, setContactLga] = useState('');

  // Volunteer form
  const [volName, setVolName] = useState('');
  const [volPhone, setVolPhone] = useState('');
  const [volRole, setVolRole] = useState('canvasser');
  const [volHasVehicle, setVolHasVehicle] = useState(false);
  const [volState, setVolState] = useState('');

  // Pledge form
  const [pledgeContactId, setPledgeContactId] = useState('');
  const [pledgeType, setPledgeType] = useState('will_vote');

  // Ride form
  const [rideContactId, setRideContactId] = useState('');
  const [ridePuCode, setRidePuCode] = useState('');
  const [ridePickupLat, setRidePickupLat] = useState('');
  const [ridePickupLng, setRidePickupLng] = useState('');

  const loadDashboard = useCallback(async () => {
    try {
      const data = await api.getGOTVDashboard();
      setDashboard(data as DashboardData);
    } catch {
      /* fallback handled by empty state */
    }
  }, []);

  const loadCampaigns = useCallback(async () => {
    try {
      const data = await api.getGOTVCampaigns() as { campaigns: Campaign[] };
      setCampaigns(data.campaigns || []);
    } catch { /* empty */ }
  }, []);

  const loadContacts = useCallback(async () => {
    try {
      const data = await api.getGOTVContacts() as { contacts: Contact[] };
      setContacts(data.contacts || []);
    } catch { /* empty */ }
  }, []);

  const loadVolunteers = useCallback(async () => {
    try {
      const data = await api.getGOTVVolunteers() as { volunteers: Volunteer[] };
      setVolunteers(data.volunteers || []);
    } catch { /* empty */ }
  }, []);

  const loadPledges = useCallback(async () => {
    try {
      const data = await api.getGOTVPledges() as { pledges: Pledge[] };
      setPledges(data.pledges || []);
    } catch { /* empty */ }
  }, []);

  const loadRides = useCallback(async () => {
    try {
      const data = await api.getGOTVRides() as { rides: Ride[] };
      setRides(data.rides || []);
    } catch { /* empty */ }
  }, []);

  useEffect(() => {
    setLoading(true);
    Promise.all([loadDashboard(), loadCampaigns(), loadContacts(), loadVolunteers(), loadPledges(), loadRides()])
      .finally(() => setLoading(false));
  }, [loadDashboard, loadCampaigns, loadContacts, loadVolunteers, loadPledges, loadRides]);

  const refreshTab = () => {
    switch (activeTab) {
      case 'dashboard': loadDashboard(); break;
      case 'campaigns': loadCampaigns(); break;
      case 'contacts': loadContacts(); break;
      case 'volunteers': loadVolunteers(); break;
      case 'pledges': loadPledges(); break;
      case 'rides': loadRides(); break;
    }
  };

  // ─── Form Handlers ──────────────────────────────────────────────────────

  const submitCampaign = async () => {
    if (!campaignName.trim()) { setFormError('Campaign name is required'); return; }
    setFormSubmitting(true); setFormError(null);
    try {
      await api.createGOTVCampaign({
        name: campaignName, campaign_type: campaignType,
        target_state: campaignState || null, message_template: campaignMessage || null,
      });
      setShowCampaignForm(false);
      setCampaignName(''); setCampaignType('sms'); setCampaignState(''); setCampaignMessage('');
      loadCampaigns(); loadDashboard();
    } catch (e: unknown) { setFormError(e instanceof Error ? e.message : 'Failed to create campaign'); }
    setFormSubmitting(false);
  };

  const submitContact = async () => {
    if (!contactPhone.trim()) { setFormError('Phone number is required'); return; }
    setFormSubmitting(true); setFormError(null);
    try {
      await api.createGOTVContact({
        phone: contactPhone, full_name: contactName || null,
        state_code: contactState || null, lga_code: contactLga || null,
        consent: true,
      });
      setShowContactForm(false);
      setContactName(''); setContactPhone(''); setContactState(''); setContactLga('');
      loadContacts(); loadDashboard();
    } catch (e: unknown) { setFormError(e instanceof Error ? e.message : 'Failed to add contact'); }
    setFormSubmitting(false);
  };

  const submitVolunteer = async () => {
    if (!volName.trim() || !volPhone.trim()) { setFormError('Name and phone are required'); return; }
    setFormSubmitting(true); setFormError(null);
    try {
      await api.createGOTVVolunteer({
        full_name: volName, phone: volPhone, role: volRole,
        has_vehicle: volHasVehicle, state_code: volState || null,
      });
      setShowVolunteerForm(false);
      setVolName(''); setVolPhone(''); setVolRole('canvasser'); setVolHasVehicle(false); setVolState('');
      loadVolunteers(); loadDashboard();
    } catch (e: unknown) { setFormError(e instanceof Error ? e.message : 'Failed to add volunteer'); }
    setFormSubmitting(false);
  };

  const submitPledge = async () => {
    if (!pledgeContactId) { setFormError('Select a contact'); return; }
    setFormSubmitting(true); setFormError(null);
    try {
      await api.createGOTVPledge({
        contact_id: pledgeContactId, pledge_type: pledgeType,
      });
      setShowPledgeForm(false);
      setPledgeContactId(''); setPledgeType('will_vote');
      loadPledges(); loadDashboard();
    } catch (e: unknown) { setFormError(e instanceof Error ? e.message : 'Failed to record pledge'); }
    setFormSubmitting(false);
  };

  const submitRide = async () => {
    if (!rideContactId || !ridePuCode) { setFormError('Contact and polling unit are required'); return; }
    setFormSubmitting(true); setFormError(null);
    try {
      await api.createGOTVRide({
        contact_id: rideContactId, polling_unit_code: ridePuCode,
        pickup_lat: ridePickupLat ? parseFloat(ridePickupLat) : null,
        pickup_lng: ridePickupLng ? parseFloat(ridePickupLng) : null,
      });
      setShowRideForm(false);
      setRideContactId(''); setRidePuCode(''); setRidePickupLat(''); setRidePickupLng('');
      loadRides(); loadDashboard();
    } catch (e: unknown) { setFormError(e instanceof Error ? e.message : 'Failed to request ride'); }
    setFormSubmitting(false);
  };

  // ─── Modal Component ──────────────────────────────────────────────────

  const Modal = ({ open, onClose, title, children }: { open: boolean; onClose: () => void; title: string; children: React.ReactNode }) => {
    if (!open) return null;
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
        <div className="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
          <div className="flex items-center justify-between p-4 border-b">
            <h2 className="text-lg font-semibold">{title}</h2>
            <button onClick={onClose} className="p-1 hover:bg-gray-100 rounded"><X className="h-4 w-4" /></button>
          </div>
          <div className="p-4 space-y-4">
            {formError && <div className="text-sm text-red-600 bg-red-50 p-2 rounded">{formError}</div>}
            {children}
          </div>
        </div>
      </div>
    );
  };

  const FormField = ({ label, children }: { label: string; children: React.ReactNode }) => (
    <div className="space-y-1">
      <label className="text-sm font-medium text-gray-700">{label}</label>
      {children}
    </div>
  );

  // ─── Dashboard Tab ─────────────────────────────────────────────────────

  const renderDashboard = () => {
    if (!dashboard) return <div className="text-center py-8 text-muted-foreground">No data available</div>;

    const stats = [
      { label: 'Contacts', value: dashboard.total_contacts, icon: Users, color: 'text-blue-600' },
      { label: 'Volunteers', value: dashboard.total_volunteers, icon: HandHeart, color: 'text-green-600' },
      { label: 'Pledges', value: dashboard.total_pledges, icon: TrendingUp, color: 'text-purple-600' },
      { label: 'Active Campaigns', value: dashboard.active_campaigns, icon: Megaphone, color: 'text-orange-600' },
      { label: 'Pending Rides', value: dashboard.pending_rides, icon: Car, color: 'text-indigo-600' },
    ];

    const pledgeData = [
      { name: 'Pledged', value: pledges.filter(p => p.status === 'pledged').length },
      { name: 'Reminded', value: pledges.filter(p => p.status === 'reminded').length },
      { name: 'Confirmed', value: pledges.filter(p => p.status === 'confirmed_day_of').length },
      { name: 'Fulfilled', value: pledges.filter(p => p.status === 'fulfilled').length },
      { name: 'Broken', value: pledges.filter(p => p.status === 'broken').length },
    ].filter(d => d.value > 0);

    const volByRole = volunteers.reduce((acc, v) => {
      acc[v.role] = (acc[v.role] || 0) + 1;
      return acc;
    }, {} as Record<string, number>);
    const roleData = Object.entries(volByRole).map(([name, value]) => ({ name, value }));

    return (
      <div className="space-y-6">
        <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
          {stats.map((s) => (
            <Card key={s.label}>
              <CardContent className="pt-6">
                <div className="flex items-center gap-2">
                  <s.icon className={`h-5 w-5 ${s.color}`} />
                  <span className="text-sm text-muted-foreground">{s.label}</span>
                </div>
                <div className="text-2xl font-bold mt-2">{s.value.toLocaleString()}</div>
              </CardContent>
            </Card>
          ))}
        </div>

        <div className="grid md:grid-cols-2 gap-6">
          {pledgeData.length > 0 && (
            <Card>
              <CardHeader><CardTitle>Pledge Funnel</CardTitle></CardHeader>
              <CardContent>
                <ResponsiveContainer width="100%" height={250}>
                  <BarChart data={pledgeData}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis dataKey="name" />
                    <YAxis />
                    <Tooltip />
                    <Bar dataKey="value" fill="#3b82f6" radius={[4, 4, 0, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </CardContent>
            </Card>
          )}

          {roleData.length > 0 && (
            <Card>
              <CardHeader><CardTitle>Volunteers by Role</CardTitle></CardHeader>
              <CardContent>
                <ResponsiveContainer width="100%" height={250}>
                  <PieChart>
                    <Pie data={roleData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={80} label>
                      {roleData.map((_, i) => <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />)}
                    </Pie>
                    <Tooltip />
                  </PieChart>
                </ResponsiveContainer>
              </CardContent>
            </Card>
          )}
        </div>

        {campaigns.filter(c => c.status === 'active').length > 0 && (
          <Card>
            <CardHeader><CardTitle>Active Campaigns</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-3">
                {campaigns.filter(c => c.status === 'active').map(c => (
                  <div key={c.campaign_id} className="flex items-center justify-between p-3 bg-muted rounded-lg">
                    <div>
                      <span className="font-medium">{c.name}</span>
                      <Badge className="ml-2" variant="outline">{c.campaign_type}</Badge>
                    </div>
                    <div className="text-right">
                      <div className="text-sm">{c.contacts_reached}/{c.total_contacts} reached</div>
                      <Progress value={c.total_contacts > 0 ? (c.contacts_reached / c.total_contacts) * 100 : 0} className="w-32 mt-1" />
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        )}
      </div>
    );
  };

  // ─── Campaigns Tab ─────────────────────────────────────────────────────

  const renderCampaigns = () => (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Search className="h-4 w-4 text-muted-foreground" />
          <Input placeholder="Search campaigns..." value={searchTerm} onChange={e => setSearchTerm(e.target.value)} className="w-64" />
        </div>
        <Button size="sm" onClick={() => { setShowCampaignForm(true); setFormError(null); }}><Plus className="h-4 w-4 mr-1" /> New Campaign</Button>
      </div>
      <Modal open={showCampaignForm} onClose={() => setShowCampaignForm(false)} title="Create New Campaign">
        <FormField label="Campaign Name *">
          <Input value={campaignName} onChange={e => setCampaignName(e.target.value)} placeholder="e.g. Lagos State SMS Outreach" />
        </FormField>
        <FormField label="Campaign Type">
          <select className="w-full border rounded-md p-2 text-sm" value={campaignType} onChange={e => setCampaignType(e.target.value)}>
            <optgroup label="Direct Messaging">
              <option value="sms">SMS (Africa's Talking / Twilio)</option>
              <option value="whatsapp">WhatsApp Business</option>
              <option value="push">Push Notification (FCM)</option>
              <option value="ussd">USSD Push</option>
              <option value="email">Email (SendGrid / Mailgun)</option>
            </optgroup>
            <optgroup label="Social Media">
              <option value="twitter">Twitter / X</option>
              <option value="facebook">Facebook Page Post</option>
              <option value="instagram">Instagram Post</option>
              <option value="tiktok">TikTok</option>
            </optgroup>
            <optgroup label="Interactive">
              <option value="whatsapp_interactive">WhatsApp Interactive (Buttons)</option>
            </optgroup>
            <optgroup label="Field Operations">
              <option value="door_to_door">Door-to-Door Canvass</option>
              <option value="phone_bank">Phone Bank</option>
              <option value="ride_to_polls">Ride-to-Polls</option>
            </optgroup>
          </select>
        </FormField>
        <FormField label="Target State">
          <select className="w-full border rounded-md p-2 text-sm" value={campaignState} onChange={e => setCampaignState(e.target.value)}>
            <option value="">All States</option>
            {NIGERIAN_STATES.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </FormField>
        <FormField label="Message Template">
          <textarea className="w-full border rounded-md p-2 text-sm" rows={3} value={campaignMessage} onChange={e => setCampaignMessage(e.target.value)} placeholder="Dear {{name}}, remember to vote at {{polling_unit}} on {{election_date}}. Your party {{party}} is counting on you!" />
          <p className="text-xs text-gray-500 mt-1">Variables: {'{'}{'{'} name {'}'}{'}'}, {'{'}{'{'} first_name {'}'}{'}'}, {'{'}{'{'} polling_unit {'}'}{'}'}, {'{'}{'{'} ward {'}'}{'}'}, {'{'}{'{'} party {'}'}{'}'}, {'{'}{'{'} election_date {'}'}{'}'}  </p>
        </FormField>
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="outline" onClick={() => setShowCampaignForm(false)}>Cancel</Button>
          <Button onClick={submitCampaign} disabled={formSubmitting}>{formSubmitting ? 'Creating...' : 'Create Campaign'}</Button>
        </div>
      </Modal>
      <div className="space-y-2">
        {campaigns
          .filter(c => !searchTerm || c.name.toLowerCase().includes(searchTerm.toLowerCase()))
          .map(c => (
            <Card key={c.campaign_id}>
              <CardContent className="py-4">
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="font-medium">{c.name}</h3>
                    <div className="flex items-center gap-2 mt-1">
                      <Badge className={STATUS_COLORS[c.status] || ''}>{c.status}</Badge>
                      <Badge variant="outline">{c.campaign_type}</Badge>
                      {c.target_state && <Badge variant="secondary">{c.target_state}</Badge>}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <div className="text-right text-sm mr-4">
                      <div>{c.contacts_reached}/{c.total_contacts} contacts</div>
                      <div className="text-muted-foreground">{new Date(c.created_at).toLocaleDateString()}</div>
                    </div>
                    {(c.status === 'draft' || c.status === 'scheduled') && (
                      <Button size="sm" className="bg-green-600 hover:bg-green-700" onClick={async () => {
                        try { await api.launchGOTVCampaign(c.campaign_id); loadCampaigns(); loadDashboard(); } catch (e: unknown) { alert(e instanceof Error ? e.message : 'Launch failed'); }
                      }}>Launch</Button>
                    )}
                    {c.status === 'active' && (
                      <Button size="sm" variant="outline" onClick={async () => {
                        try { await api.pauseGOTVCampaign(c.campaign_id); loadCampaigns(); loadDashboard(); } catch (e: unknown) { alert(e instanceof Error ? e.message : 'Pause failed'); }
                      }}>Pause</Button>
                    )}
                    {c.status === 'paused' && (
                      <Button size="sm" className="bg-blue-600 hover:bg-blue-700" onClick={async () => {
                        try { await api.resumeGOTVCampaign(c.campaign_id); loadCampaigns(); loadDashboard(); } catch (e: unknown) { alert(e instanceof Error ? e.message : 'Resume failed'); }
                      }}>Resume</Button>
                    )}
                    {c.status === 'draft' && (
                      <Button size="sm" variant="destructive" onClick={async () => {
                        if (!confirm('Delete this campaign?')) return;
                        try { await api.deleteGOTVCampaign(c.campaign_id); loadCampaigns(); loadDashboard(); } catch (e: unknown) { alert(e instanceof Error ? e.message : 'Delete failed'); }
                      }}>Delete</Button>
                    )}
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        {campaigns.length === 0 && <div className="text-center py-8 text-muted-foreground">No campaigns yet</div>}
      </div>
    </div>
  );

  // ─── Contacts Tab ──────────────────────────────────────────────────────

  const renderContacts = () => (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Search className="h-4 w-4 text-muted-foreground" />
          <Input placeholder="Search contacts..." value={searchTerm} onChange={e => setSearchTerm(e.target.value)} className="w-64" />
        </div>
        <div className="flex gap-2">
          <input type="file" accept=".csv" className="hidden" id="csv-import" onChange={async (e) => {
            const file = e.target.files?.[0];
            if (!file) return;
            const formData = new FormData();
            formData.append('file', file);
            try { await api.importGOTVContacts(formData); loadContacts(); loadDashboard(); } catch (err: unknown) { alert(err instanceof Error ? err.message : 'Import failed'); }
            e.target.value = '';
          }} />
          <Button size="sm" variant="outline" onClick={() => document.getElementById('csv-import')?.click()}><Upload className="h-4 w-4 mr-1" /> Import CSV</Button>
          <Button size="sm" onClick={() => { setShowContactForm(true); setFormError(null); }}><Plus className="h-4 w-4 mr-1" /> Add Contact</Button>
        </div>
      </div>
      <Modal open={showContactForm} onClose={() => setShowContactForm(false)} title="Add Contact">
        <FormField label="Phone Number *">
          <Input value={contactPhone} onChange={e => setContactPhone(e.target.value)} placeholder="08012345678" />
        </FormField>
        <FormField label="Full Name">
          <Input value={contactName} onChange={e => setContactName(e.target.value)} placeholder="Adebayo Ogunwale" />
        </FormField>
        <FormField label="State">
          <select className="w-full border rounded-md p-2 text-sm" value={contactState} onChange={e => setContactState(e.target.value)}>
            <option value="">Select State</option>
            {NIGERIAN_STATES.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </FormField>
        <FormField label="LGA">
          <Input value={contactLga} onChange={e => setContactLga(e.target.value)} placeholder="Ikeja" />
        </FormField>
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="outline" onClick={() => setShowContactForm(false)}>Cancel</Button>
          <Button onClick={submitContact} disabled={formSubmitting}>{formSubmitting ? 'Adding...' : 'Add Contact'}</Button>
        </div>
      </Modal>
      <div className="rounded-md border">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="p-3 text-left">Name</th>
              <th className="p-3 text-left">Phone</th>
              <th className="p-3 text-left">State</th>
              <th className="p-3 text-left">Status</th>
              <th className="p-3 text-left">Tags</th>
            </tr>
          </thead>
          <tbody>
            {contacts
              .filter(c => !searchTerm || c.full_name.toLowerCase().includes(searchTerm.toLowerCase()) || c.phone_masked.includes(searchTerm))
              .map(c => (
                <tr key={c.contact_id} className="border-b">
                  <td className="p-3 font-medium">{c.full_name || '—'}</td>
                  <td className="p-3 font-mono text-sm">{c.phone_masked}</td>
                  <td className="p-3">{c.state_code || '—'}</td>
                  <td className="p-3"><Badge className={VOTER_STATUS_COLORS[c.voter_status] || ''}>{c.voter_status}</Badge></td>
                  <td className="p-3">{(c.tags || []).map(t => <Badge key={t} variant="outline" className="mr-1">{t}</Badge>)}</td>
                </tr>
              ))}
          </tbody>
        </table>
        {contacts.length === 0 && <div className="text-center py-8 text-muted-foreground">No contacts yet — import a CSV to get started</div>}
      </div>
    </div>
  );

  // ─── Volunteers Tab ────────────────────────────────────────────────────

  const renderVolunteers = () => (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <Input placeholder="Search volunteers..." value={searchTerm} onChange={e => setSearchTerm(e.target.value)} className="w-64" />
        <Button size="sm" onClick={() => { setShowVolunteerForm(true); setFormError(null); }}><Plus className="h-4 w-4 mr-1" /> Add Volunteer</Button>
      </div>
      <Modal open={showVolunteerForm} onClose={() => setShowVolunteerForm(false)} title="Add Volunteer">
        <FormField label="Full Name *">
          <Input value={volName} onChange={e => setVolName(e.target.value)} placeholder="Chinedu Eze" />
        </FormField>
        <FormField label="Phone Number *">
          <Input value={volPhone} onChange={e => setVolPhone(e.target.value)} placeholder="08098765432" />
        </FormField>
        <FormField label="Role">
          <select className="w-full border rounded-md p-2 text-sm" value={volRole} onChange={e => setVolRole(e.target.value)}>
            <option value="canvasser">Canvasser (Door-to-door)</option>
            <option value="driver">Driver (Ride-to-polls)</option>
            <option value="caller">Phone Caller</option>
            <option value="observer">Polling Unit Observer</option>
            <option value="coordinator">Ward Coordinator</option>
          </select>
        </FormField>
        <FormField label="State">
          <select className="w-full border rounded-md p-2 text-sm" value={volState} onChange={e => setVolState(e.target.value)}>
            <option value="">Select State</option>
            {NIGERIAN_STATES.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </FormField>
        <div className="flex items-center gap-2">
          <input type="checkbox" id="hasVehicle" checked={volHasVehicle} onChange={e => setVolHasVehicle(e.target.checked)} />
          <label htmlFor="hasVehicle" className="text-sm">Has vehicle available</label>
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="outline" onClick={() => setShowVolunteerForm(false)}>Cancel</Button>
          <Button onClick={submitVolunteer} disabled={formSubmitting}>{formSubmitting ? 'Adding...' : 'Add Volunteer'}</Button>
        </div>
      </Modal>
      <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-4">
        {volunteers
          .filter(v => !searchTerm || v.full_name.toLowerCase().includes(searchTerm.toLowerCase()))
          .map(v => (
            <Card key={v.volunteer_id}>
              <CardContent className="pt-4">
                <div className="flex items-center justify-between mb-2">
                  <h3 className="font-medium">{v.full_name}</h3>
                  <Badge variant={v.is_active ? 'default' : 'secondary'}>{v.is_active ? 'Active' : 'Inactive'}</Badge>
                </div>
                <div className="flex items-center gap-2 mb-3">
                  <Badge variant="outline">{v.role}</Badge>
                  {v.has_vehicle && <Badge className="bg-green-100 text-green-800">Has Vehicle</Badge>}
                </div>
                <div className="grid grid-cols-3 gap-2 text-center text-sm">
                  <div>
                    <div className="font-bold">{v.doors_knocked}</div>
                    <div className="text-muted-foreground">Doors</div>
                  </div>
                  <div>
                    <div className="font-bold">{v.calls_made}</div>
                    <div className="text-muted-foreground">Calls</div>
                  </div>
                  <div>
                    <div className="font-bold">{v.rides_given}</div>
                    <div className="text-muted-foreground">Rides</div>
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
      </div>
      {volunteers.length === 0 && <div className="text-center py-8 text-muted-foreground">No volunteers registered yet</div>}
    </div>
  );

  // ─── Pledges Tab ───────────────────────────────────────────────────────

  const renderPledges = () => (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <Input placeholder="Search pledges..." value={searchTerm} onChange={e => setSearchTerm(e.target.value)} className="w-64" />
        <Button size="sm" onClick={() => { setShowPledgeForm(true); setFormError(null); }}><Plus className="h-4 w-4 mr-1" /> Record Pledge</Button>
      </div>
      <Modal open={showPledgeForm} onClose={() => setShowPledgeForm(false)} title="Record Pledge">
        <FormField label="Contact *">
          <select className="w-full border rounded-md p-2 text-sm" value={pledgeContactId} onChange={e => setPledgeContactId(e.target.value)}>
            <option value="">Select a contact</option>
            {contacts.map(c => <option key={c.contact_id} value={c.contact_id}>{c.full_name || c.phone_masked} ({c.state_code || 'N/A'})</option>)}
          </select>
        </FormField>
        <FormField label="Pledge Type">
          <select className="w-full border rounded-md p-2 text-sm" value={pledgeType} onChange={e => setPledgeType(e.target.value)}>
            <option value="will_vote">Will Vote</option>
            <option value="will_volunteer">Will Volunteer</option>
            <option value="needs_ride">Needs a Ride</option>
            <option value="will_donate">Will Donate</option>
          </select>
        </FormField>
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="outline" onClick={() => setShowPledgeForm(false)}>Cancel</Button>
          <Button onClick={submitPledge} disabled={formSubmitting}>{formSubmitting ? 'Recording...' : 'Record Pledge'}</Button>
        </div>
      </Modal>
      <div className="rounded-md border">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="p-3 text-left">Pledge ID</th>
              <th className="p-3 text-left">Contact</th>
              <th className="p-3 text-left">Type</th>
              <th className="p-3 text-left">Status</th>
              <th className="p-3 text-left">Reminder</th>
              <th className="p-3 text-left">Date</th>
            </tr>
          </thead>
          <tbody>
            {pledges.map(p => (
              <tr key={p.pledge_id} className="border-b">
                <td className="p-3 font-mono text-xs">{p.pledge_id}</td>
                <td className="p-3">{contacts.find(c => c.contact_id === p.contact_id)?.full_name || contacts.find(c => c.contact_id === p.contact_id)?.phone_masked || p.contact_id}</td>
                <td className="p-3"><Badge variant="outline">{p.pledge_type}</Badge></td>
                <td className="p-3"><Badge className={PLEDGE_STATUS_COLORS[p.status] || ''}>{p.status}</Badge></td>
                <td className="p-3">{p.reminder_sent ? 'Sent' : 'Pending'}</td>
                <td className="p-3 text-muted-foreground">{new Date(p.created_at).toLocaleDateString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {pledges.length === 0 && <div className="text-center py-8 text-muted-foreground">No pledges recorded yet</div>}
      </div>
    </div>
  );

  // ─── Rides Tab ─────────────────────────────────────────────────────────

  const renderRides = () => (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Filter className="h-4 w-4 text-muted-foreground" />
          <Input placeholder="Filter rides..." value={searchTerm} onChange={e => setSearchTerm(e.target.value)} className="w-64" />
        </div>
        <Button size="sm" onClick={() => { setShowRideForm(true); setFormError(null); }}><Car className="h-4 w-4 mr-1" /> New Ride Request</Button>
      </div>
      <Modal open={showRideForm} onClose={() => setShowRideForm(false)} title="New Ride Request">
        <FormField label="Contact *">
          <select className="w-full border rounded-md p-2 text-sm" value={rideContactId} onChange={e => setRideContactId(e.target.value)}>
            <option value="">Select a contact</option>
            {contacts.map(c => <option key={c.contact_id} value={c.contact_id}>{c.full_name || c.phone_masked}</option>)}
          </select>
        </FormField>
        <FormField label="Polling Unit Code *">
          <Input value={ridePuCode} onChange={e => setRidePuCode(e.target.value)} placeholder="e.g. LA/IKJ/001" />
        </FormField>
        <div className="grid grid-cols-2 gap-2">
          <FormField label="Pickup Latitude">
            <Input value={ridePickupLat} onChange={e => setRidePickupLat(e.target.value)} placeholder="6.5244" />
          </FormField>
          <FormField label="Pickup Longitude">
            <Input value={ridePickupLng} onChange={e => setRidePickupLng(e.target.value)} placeholder="3.3792" />
          </FormField>
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="outline" onClick={() => setShowRideForm(false)}>Cancel</Button>
          <Button onClick={submitRide} disabled={formSubmitting}>{formSubmitting ? 'Requesting...' : 'Request Ride'}</Button>
        </div>
      </Modal>
      <div className="rounded-md border">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="p-3 text-left">Request ID</th>
              <th className="p-3 text-left">Contact</th>
              <th className="p-3 text-left">Driver</th>
              <th className="p-3 text-left">PU</th>
              <th className="p-3 text-left">Status</th>
              <th className="p-3 text-left">Distance</th>
            </tr>
          </thead>
          <tbody>
            {rides
              .filter(r => !searchTerm || r.request_id.includes(searchTerm) || r.status.includes(searchTerm))
              .map(r => (
                <tr key={r.request_id} className="border-b">
                  <td className="p-3 font-mono text-xs">{r.request_id}</td>
                  <td className="p-3">{contacts.find(c => c.contact_id === r.contact_id)?.full_name || contacts.find(c => c.contact_id === r.contact_id)?.phone_masked || r.contact_id}</td>
                  <td className="p-3">{r.volunteer_id ? (volunteers.find(v => v.volunteer_id === r.volunteer_id)?.full_name || r.volunteer_id) : '—'}</td>
                  <td className="p-3">{r.polling_unit_code}</td>
                  <td className="p-3"><Badge className={RIDE_STATUS_COLORS[r.status] || ''}>{r.status}</Badge></td>
                  <td className="p-3">{r.distance_km ? `${r.distance_km} km` : '—'}</td>
                </tr>
              ))}
          </tbody>
        </table>
        {rides.length === 0 && <div className="text-center py-8 text-muted-foreground">No ride requests yet</div>}
      </div>
    </div>
  );

  // ─── Main Render ───────────────────────────────────────────────────────

  const tabs: { key: Tab; label: string; icon: typeof Users }[] = [
    { key: 'dashboard', label: 'Dashboard', icon: TrendingUp },
    { key: 'map', label: 'Live Map', icon: MapPin },
    { key: 'campaigns', label: 'Campaigns', icon: Megaphone },
    { key: 'contacts', label: 'Contacts', icon: Users },
    { key: 'volunteers', label: 'Volunteers', icon: HandHeart },
    { key: 'pledges', label: 'Pledges', icon: TrendingUp },
    { key: 'rides', label: 'Rides', icon: Car },
    { key: 'leaderboard', label: 'Leaderboard', icon: TrendingUp },
    { key: 'segments', label: 'Segments', icon: Filter },
    { key: 'warroom', label: 'War Room', icon: Megaphone },
    { key: 'analytics', label: 'Analytics', icon: TrendingUp },
  ];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">GOTV Party Portal</h1>
          <p className="text-muted-foreground">Get Out The Vote — Voter Mobilization Dashboard</p>
        </div>
        <Button size="sm" variant="outline" onClick={refreshTab}>
          <RefreshCw className="h-4 w-4 mr-1" /> Refresh
        </Button>
      </div>

      <div className="flex border-b">
        {tabs.map(tab => (
          <button
            key={tab.key}
            onClick={() => { setActiveTab(tab.key); setSearchTerm(''); }}
            className={`flex items-center gap-2 px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              activeTab === tab.key
                ? 'border-primary text-primary'
                : 'border-transparent text-muted-foreground hover:text-foreground'
            }`}
          >
            <tab.icon className="h-4 w-4" />
            {tab.label}
          </button>
        ))}
      </div>

      {loading ? (
        <div className="text-center py-12 text-muted-foreground">Loading GOTV data...</div>
      ) : (
        <>
          {activeTab === 'dashboard' && renderDashboard()}
          {activeTab === 'map' && <GOTVMapPage />}
          {activeTab === 'campaigns' && renderCampaigns()}
          {activeTab === 'contacts' && renderContacts()}
          {activeTab === 'volunteers' && renderVolunteers()}
          {activeTab === 'pledges' && renderPledges()}
          {activeTab === 'rides' && renderRides()}
          {activeTab === 'leaderboard' && <GOTVLeaderboard />}
          {activeTab === 'segments' && <GOTVSegments />}
          {activeTab === 'warroom' && <GOTVWarRoom />}
          {activeTab === 'analytics' && <GOTVAnalytics />}
        </>
      )}
    </div>
  );
}
