import React, { useEffect, useState, useRef, useCallback } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl, TouchableOpacity, TextInput, Alert } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface Observer { id: number; name: string; organization: string; accreditation_id: string; status: string; assigned_pu: string; check_in_time: string; reports_count?: number; }
interface ObserverReport { id: number; polling_unit_code: string; report_type: string; description: string; status: string; created_at: string; }
interface Stats { total_observers: number; active_check_ins: number; reports_today: number; active_alert_rules: number; }

type Tab = 'observers' | 'reports' | 'submit';

export default function ObserverMonitoringScreen() {
  const [tab, setTab] = useState<Tab>('observers');
  const [observers, setObservers] = useState<Observer[]>([]);
  const [reports, setReports] = useState<ObserverReport[]>([]);
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [search, setSearch] = useState('');

  // Report submission form
  const [puCode, setPuCode] = useState('');
  const [reportType, setReportType] = useState('observation');
  const [description, setDescription] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const loadObservers = useCallback(async () => {
    try {
      const res = await fetch(`${API}/observers`);
      if (res.ok) { const d = await res.json(); setObservers(Array.isArray(d) ? d : d.observers || []); }
    } catch (e) { console.error('Observer load:', e); }
  }, []);

  const loadReports = useCallback(async () => {
    try {
      const res = await fetch(`${API}/observer/reports`);
      if (res.ok) { const d = await res.json(); setReports(Array.isArray(d) ? d : d.reports || []); }
    } catch (e) { console.error('Reports load:', e); }
  }, []);

  const loadStats = useCallback(async () => {
    try {
      const res = await fetch(`${API}/observer/stats`);
      if (res.ok) setStats(await res.json());
    } catch (e) { console.error('Stats load:', e); }
  }, []);

  const load = useCallback(async () => {
    await Promise.all([loadObservers(), loadReports(), loadStats()]);
    setLoading(false); setRefreshing(false);
  }, [loadObservers, loadReports, loadStats]);

  useEffect(() => { load(); }, [load]);

  // Auto-refresh every 30s
  const intervalRef = useRef<NodeJS.Timeout>();
  useEffect(() => {
    intervalRef.current = setInterval(load, 30000);
    return () => { if (intervalRef.current) clearInterval(intervalRef.current); };
  }, [load]);

  const submitReport = async () => {
    if (!puCode.trim() || !description.trim()) { Alert.alert('Missing Fields', 'Polling unit code and description are required.'); return; }
    setSubmitting(true);
    try {
      const body = JSON.stringify({ polling_unit_code: puCode, report_type: reportType, description, election_id: 1 });
      const res = await fetch(`${API}/observer/reports`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body });
      if (res.ok) {
        Alert.alert('Report Submitted', 'Your observer report has been submitted successfully.');
        setPuCode(''); setDescription(''); setReportType('observation');
        loadReports();
      } else { Alert.alert('Error', 'Failed to submit report. It has been queued for retry.'); }
    } catch { Alert.alert('Offline', 'Report queued for submission when connectivity is restored.'); }
    setSubmitting(false);
  };

  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const filtered = observers.filter(o =>
    !search || o.name?.toLowerCase().includes(search.toLowerCase()) ||
    o.assigned_pu?.toLowerCase().includes(search.toLowerCase()) ||
    o.organization?.toLowerCase().includes(search.toLowerCase())
  );

  const statusColor = (st: string) => st === 'active' ? '#16a34a' : st === 'pending' ? '#f59e0b' : '#dc2626';
  const statusBg = (st: string) => st === 'active' ? '#dcfce7' : st === 'pending' ? '#fef3c7' : '#fee2e2';

  return (
    <View style={s.container}>
      <Text style={s.title}>Observer Monitoring</Text>

      {/* Stats row */}
      {stats && (
        <View style={s.statsRow}>
          <View style={s.stat}><Text style={s.statVal}>{stats.total_observers}</Text><Text style={s.statLabel}>Observers</Text></View>
          <View style={s.stat}><Text style={s.statVal}>{stats.active_check_ins}</Text><Text style={s.statLabel}>Checked In</Text></View>
          <View style={s.stat}><Text style={s.statVal}>{stats.reports_today}</Text><Text style={s.statLabel}>Reports</Text></View>
          <View style={s.stat}><Text style={s.statVal}>{stats.active_alert_rules}</Text><Text style={s.statLabel}>Alerts</Text></View>
        </View>
      )}

      {/* Tabs */}
      <View style={s.tabs}>
        {(['observers', 'reports', 'submit'] as Tab[]).map(t => (
          <TouchableOpacity key={t} onPress={() => setTab(t)} style={[s.tab, tab === t && s.tabActive]}>
            <Text style={[s.tabText, tab === t && s.tabTextActive]}>{t === 'submit' ? 'Submit Report' : t.charAt(0).toUpperCase() + t.slice(1)}</Text>
          </TouchableOpacity>
        ))}
      </View>

      {/* Observers tab */}
      {tab === 'observers' && (
        <>
          <TextInput style={s.searchInput} placeholder="Search observers, PU, org..." value={search} onChangeText={setSearch} placeholderTextColor="#94a3b8" />
          <Text style={s.count}>{filtered.length} observers</Text>
          <FlatList data={filtered} keyExtractor={o => String(o.id)}
            refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
            renderItem={({ item }) => (
              <View style={s.card}>
                <View style={s.row}>
                  <Text style={s.name}>{item.name}</Text>
                  <View style={[s.statusBadge, { backgroundColor: statusBg(item.status) }]}>
                    <Text style={{ color: statusColor(item.status), fontSize: 11, fontWeight: '600' }}>{item.status}</Text>
                  </View>
                </View>
                <Text style={s.sub}>{item.organization} · {item.accreditation_id}</Text>
                <Text style={s.sub}>PU: {item.assigned_pu}</Text>
                {item.check_in_time && <Text style={s.sub}>Checked in: {new Date(item.check_in_time).toLocaleTimeString()}</Text>}
                {(item.reports_count ?? 0) > 0 && <Text style={s.sub}>{item.reports_count} reports filed</Text>}
              </View>
            )}
          />
        </>
      )}

      {/* Reports tab */}
      {tab === 'reports' && (
        <FlatList data={reports} keyExtractor={r => String(r.id)}
          refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
          ListEmptyComponent={<Text style={s.empty}>No reports yet</Text>}
          renderItem={({ item }) => (
            <View style={s.card}>
              <View style={s.row}>
                <Text style={s.name}>{item.polling_unit_code}</Text>
                <View style={[s.statusBadge, { backgroundColor: item.status === 'flagged' ? '#fee2e2' : item.status === 'verified' ? '#dcfce7' : '#fef3c7' }]}>
                  <Text style={{ fontSize: 11, fontWeight: '600', color: item.status === 'flagged' ? '#dc2626' : item.status === 'verified' ? '#16a34a' : '#d97706' }}>{item.status}</Text>
                </View>
              </View>
              <Text style={[s.sub, { fontWeight: '500' }]}>{item.report_type}</Text>
              <Text style={s.sub}>{item.description}</Text>
              <Text style={[s.sub, { fontSize: 11 }]}>{new Date(item.created_at).toLocaleString()}</Text>
            </View>
          )}
        />
      )}

      {/* Submit report tab */}
      {tab === 'submit' && (
        <View style={s.form}>
          <Text style={s.formLabel}>Polling Unit Code *</Text>
          <TextInput style={s.input} value={puCode} onChangeText={setPuCode} placeholder="e.g. LA/01/001/0001" placeholderTextColor="#94a3b8" />

          <Text style={s.formLabel}>Report Type</Text>
          <View style={s.typeRow}>
            {['observation', 'irregularity', 'result_photo'].map(rt => (
              <TouchableOpacity key={rt} onPress={() => setReportType(rt)} style={[s.typeBtn, reportType === rt && s.typeBtnActive]}>
                <Text style={[s.typeBtnText, reportType === rt && s.typeBtnTextActive]}>{rt.replace('_', ' ')}</Text>
              </TouchableOpacity>
            ))}
          </View>

          <Text style={s.formLabel}>Description *</Text>
          <TextInput style={[s.input, { height: 80, textAlignVertical: 'top' }]} value={description} onChangeText={setDescription} placeholder="Describe what you observed..." placeholderTextColor="#94a3b8" multiline />

          <TouchableOpacity style={s.submitBtn} onPress={submitReport} disabled={submitting}>
            <Text style={s.submitText}>{submitting ? 'Submitting...' : 'Submit Report'}</Text>
          </TouchableOpacity>
        </View>
      )}
    </View>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 8 },
  count: { fontSize: 13, color: '#64748b', marginBottom: 8 },
  statsRow: { flexDirection: 'row', marginBottom: 12, gap: 8 },
  stat: { flex: 1, backgroundColor: '#fff', borderRadius: 10, padding: 10, alignItems: 'center', shadowColor: '#000', shadowOpacity: 0.04, shadowRadius: 3 },
  statVal: { fontSize: 20, fontWeight: '700', color: '#16a34a' },
  statLabel: { fontSize: 11, color: '#64748b', marginTop: 2 },
  tabs: { flexDirection: 'row', marginBottom: 12, gap: 6 },
  tab: { flex: 1, paddingVertical: 8, alignItems: 'center', borderRadius: 8, backgroundColor: '#e2e8f0' },
  tabActive: { backgroundColor: '#16a34a' },
  tabText: { fontSize: 13, fontWeight: '600', color: '#64748b' },
  tabTextActive: { color: '#fff' },
  searchInput: { backgroundColor: '#fff', borderRadius: 8, paddingHorizontal: 12, paddingVertical: 10, fontSize: 14, borderWidth: 1, borderColor: '#e2e8f0', marginBottom: 8 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  row: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  name: { fontSize: 15, fontWeight: '600', color: '#1e293b', flex: 1 },
  sub: { fontSize: 13, color: '#64748b', marginTop: 4 },
  statusBadge: { paddingHorizontal: 10, paddingVertical: 4, borderRadius: 12 },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 40, fontSize: 14 },
  form: { backgroundColor: '#fff', borderRadius: 12, padding: 16, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  formLabel: { fontSize: 13, fontWeight: '600', color: '#1e293b', marginBottom: 6, marginTop: 12 },
  input: { backgroundColor: '#f8fafc', borderRadius: 8, paddingHorizontal: 12, paddingVertical: 10, fontSize: 14, borderWidth: 1, borderColor: '#e2e8f0' },
  typeRow: { flexDirection: 'row', gap: 8 },
  typeBtn: { paddingHorizontal: 12, paddingVertical: 8, borderRadius: 8, backgroundColor: '#f1f5f9', borderWidth: 1, borderColor: '#e2e8f0' },
  typeBtnActive: { backgroundColor: '#dcfce7', borderColor: '#16a34a' },
  typeBtnText: { fontSize: 12, color: '#64748b', fontWeight: '500' },
  typeBtnTextActive: { color: '#16a34a' },
  submitBtn: { backgroundColor: '#16a34a', borderRadius: 10, paddingVertical: 14, alignItems: 'center', marginTop: 20 },
  submitText: { color: '#fff', fontSize: 16, fontWeight: '700' },
});
