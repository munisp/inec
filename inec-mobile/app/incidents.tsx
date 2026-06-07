import { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Platform, ActivityIndicator, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface Incident {
  id: number;
  title: string;
  description: string;
  severity: string;
  status: string;
  polling_unit_code: string;
  reported_by: number;
  created_at: string;
}

export default function IncidentsScreen() {
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [loading, setLoading] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [severity, setSeverity] = useState('medium');
  const [puCode, setPuCode] = useState('');
  const [search, setSearch] = useState('');
  const [filterStatus, setFilterStatus] = useState('all');
  const [filterSeverity, setFilterSeverity] = useState('all');

  const loadIncidents = async () => {
    setLoading(true);
    try {
      const data = await api<{ incidents: Incident[] }>('/incidents');
      setIncidents(data.incidents || []);
    } catch { /* ignore */ }
    setLoading(false);
  };

  useEffect(() => { loadIncidents(); }, []);

  const submitIncident = async () => {
    if (!title || !description) {
      Alert.alert('Error', 'Title and description required');
      return;
    }
    Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    try {
      await api('/incidents', {
        method: 'POST',
        body: JSON.stringify({ title, description, severity, polling_unit_code: puCode, election_id: 1 }),
      });
      setShowForm(false);
      setTitle(''); setDescription(''); setPuCode('');
      loadIncidents();
    } catch (e) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to submit');
    }
  };

  const updateStatus = async (id: number, status: string) => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      await api(`/incidents/${id}`, { method: 'PATCH', body: JSON.stringify({ status }) });
      loadIncidents();
    } catch (e) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to update');
    }
  };

  const sevColor = (s: string) => s === 'critical' ? '#dc2626' : s === 'high' ? '#f59e0b' : s === 'medium' ? '#3b82f6' : '#64748b';

  const filtered = incidents.filter(inc => {
    if (filterStatus !== 'all' && inc.status !== filterStatus) return false;
    if (filterSeverity !== 'all' && inc.severity !== filterSeverity) return false;
    if (search) {
      const q = search.toLowerCase();
      return inc.title?.toLowerCase().includes(q) || inc.description?.toLowerCase().includes(q) || inc.polling_unit_code?.toLowerCase().includes(q);
    }
    return true;
  });

  const sevDist = incidents.reduce((a, i) => { a[i.severity] = (a[i.severity] || 0) + 1; return a; }, {} as Record<string, number>);
  const statDist = incidents.reduce((a, i) => { a[i.status] = (a[i.status] || 0) + 1; return a; }, {} as Record<string, number>);

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      <View style={styles.header}>
        <Ionicons name="alert-circle" size={28} color="#dc2626" />
        <Text style={styles.titleText}>Incidents</Text>
        <TouchableOpacity style={styles.addBtn} onPress={() => setShowForm(!showForm)}>
          <Ionicons name={showForm ? 'close' : 'add'} size={20} color="#fff" />
        </TouchableOpacity>
      </View>

      {/* Summary Stats */}
      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ paddingHorizontal: 16, marginBottom: 12 }}>
        {[
          { label: 'Total', value: incidents.length, bg: '#f1f5f9', color: '#334155' },
          { label: 'Critical', value: sevDist.critical || 0, bg: '#fef2f2', color: '#dc2626' },
          { label: 'High', value: sevDist.high || 0, bg: '#fff7ed', color: '#ea580c' },
          { label: 'Open', value: (statDist.reported || 0) + (statDist.investigating || 0), bg: '#fffbeb', color: '#d97706' },
          { label: 'Resolved', value: statDist.resolved || 0, bg: '#f0fdf4', color: '#16a34a' },
        ].map(s => (
          <View key={s.label} style={{ backgroundColor: s.bg, borderRadius: 10, padding: 12, marginRight: 8, minWidth: 72, alignItems: 'center' }}>
            <Text style={{ fontSize: 20, fontWeight: '700', color: s.color }}>{s.value}</Text>
            <Text style={{ fontSize: 11, color: '#64748b', marginTop: 2 }}>{s.label}</Text>
          </View>
        ))}
      </ScrollView>

      <View style={styles.searchBox}>
        <Ionicons name="search" size={18} color="#94a3b8" />
        <TextInput style={styles.searchInput} placeholder="Search incidents..." value={search} onChangeText={setSearch} />
      </View>

      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ paddingHorizontal: 16, marginBottom: 8 }}>
        {['all', 'reported', 'investigating', 'resolved', 'dismissed'].map(s => (
          <TouchableOpacity key={s} style={[styles.filterChip, filterStatus === s && styles.filterChipActive]} onPress={() => setFilterStatus(s)}>
            <Text style={[styles.filterChipText, filterStatus === s && styles.filterChipTextActive]}>{s === 'all' ? 'All Status' : s.charAt(0).toUpperCase() + s.slice(1)}</Text>
          </TouchableOpacity>
        ))}
      </ScrollView>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ paddingHorizontal: 16, marginBottom: 12 }}>
        {['all', 'low', 'medium', 'high', 'critical'].map(s => (
          <TouchableOpacity key={s} style={[styles.filterChip, filterSeverity === s && { backgroundColor: sevColor(s) }]} onPress={() => setFilterSeverity(s)}>
            <Text style={[styles.filterChipText, filterSeverity === s && { color: '#fff' }]}>{s === 'all' ? 'All Severity' : s.charAt(0).toUpperCase() + s.slice(1)}</Text>
          </TouchableOpacity>
        ))}
      </ScrollView>

      <Text style={styles.countText}>{filtered.length} of {incidents.length} incidents</Text>

      {showForm && (
        <View style={styles.formCard}>
          <TextInput style={styles.input} placeholder="Incident title" value={title} onChangeText={setTitle} />
          <TextInput style={[styles.input, { height: 80 }]} placeholder="Description" value={description} onChangeText={setDescription} multiline />
          <TextInput style={styles.input} placeholder="Polling unit code (optional)" value={puCode} onChangeText={setPuCode} />
          <View style={styles.sevRow}>
            {['low', 'medium', 'high', 'critical'].map(s => (
              <TouchableOpacity key={s} style={[styles.sevBtn, severity === s && { backgroundColor: sevColor(s) }]} onPress={() => setSeverity(s)}>
                <Text style={[styles.sevBtnText, severity === s && { color: '#fff' }]}>{s}</Text>
              </TouchableOpacity>
            ))}
          </View>
          <TouchableOpacity style={styles.submitBtn} onPress={submitIncident}>
            <Ionicons name="send" size={16} color="#fff" />
            <Text style={styles.submitText}>Report Incident</Text>
          </TouchableOpacity>
        </View>
      )}

      {loading && <ActivityIndicator size="large" color="#dc2626" style={{ marginTop: 24 }} />}

      {filtered.map(inc => (
        <View key={inc.id} style={[styles.incidentCard, { borderLeftColor: sevColor(inc.severity) }]}>
          <View style={styles.incHeader}>
            <Text style={styles.incTitle}>{inc.title}</Text>
            <View style={[styles.badge, { backgroundColor: sevColor(inc.severity) + '20' }]}>
              <Text style={[styles.badgeText, { color: sevColor(inc.severity) }]}>{inc.severity}</Text>
            </View>
          </View>
          <Text style={styles.incDesc} numberOfLines={2}>{inc.description}</Text>
          {inc.polling_unit_code && <Text style={styles.incPU}>{inc.polling_unit_code}</Text>}
          <View style={styles.incFooter}>
            <Text style={styles.incStatus}>{inc.status}</Text>
            <Text style={styles.incTime}>{new Date(inc.created_at).toLocaleDateString()}</Text>
          </View>
          {inc.status !== 'resolved' && inc.status !== 'dismissed' && (
            <View style={styles.actionRow}>
              {inc.status === 'reported' && (
                <TouchableOpacity style={[styles.actionBtn, { backgroundColor: '#dbeafe' }]} onPress={() => updateStatus(inc.id, 'investigating')}>
                  <Ionicons name="search" size={14} color="#2563eb" />
                  <Text style={[styles.actionText, { color: '#2563eb' }]}>Investigate</Text>
                </TouchableOpacity>
              )}
              <TouchableOpacity style={[styles.actionBtn, { backgroundColor: '#dcfce7' }]} onPress={() => updateStatus(inc.id, 'resolved')}>
                <Ionicons name="checkmark-circle" size={14} color="#166534" />
                <Text style={[styles.actionText, { color: '#166534' }]}>Resolve</Text>
              </TouchableOpacity>
              <TouchableOpacity style={[styles.actionBtn, { backgroundColor: '#f1f5f9' }]} onPress={() => updateStatus(inc.id, 'dismissed')}>
                <Ionicons name="close-circle" size={14} color="#64748b" />
                <Text style={[styles.actionText, { color: '#64748b' }]}>Dismiss</Text>
              </TouchableOpacity>
            </View>
          )}
        </View>
      ))}

      {!loading && filtered.length === 0 && (
        <View style={{ alignItems: 'center', marginTop: 40 }}>
          <Ionicons name="alert-circle-outline" size={48} color="#94a3b8" />
          <Text style={{ color: '#64748b', marginTop: 8 }}>No incidents match your filters</Text>
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 16, paddingTop: Platform.OS === 'ios' ? 60 : 16 },
  titleText: { fontSize: 22, fontWeight: '700', color: '#1e293b', flex: 1 },
  addBtn: { width: 36, height: 36, borderRadius: 18, backgroundColor: '#dc2626', alignItems: 'center', justifyContent: 'center' },
  searchBox: { flexDirection: 'row', alignItems: 'center', marginHorizontal: 16, marginBottom: 10, padding: 10, backgroundColor: '#fff', borderRadius: 10, borderWidth: 1, borderColor: '#e2e8f0', gap: 8 },
  searchInput: { flex: 1, fontSize: 14 },
  filterChip: { paddingHorizontal: 12, paddingVertical: 6, borderRadius: 16, backgroundColor: '#f1f5f9', marginRight: 8 },
  filterChipActive: { backgroundColor: '#dc2626' },
  filterChipText: { fontSize: 12, fontWeight: '600', color: '#64748b' },
  filterChipTextActive: { color: '#fff' },
  countText: { fontSize: 13, color: '#64748b', marginHorizontal: 16, marginBottom: 8 },
  formCard: { marginHorizontal: 16, marginBottom: 16, padding: 16, backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#fecaca' },
  input: { borderWidth: 1, borderColor: '#e2e8f0', borderRadius: 8, padding: 10, marginBottom: 10, fontSize: 14 },
  sevRow: { flexDirection: 'row', gap: 6, marginBottom: 10 },
  sevBtn: { flex: 1, paddingVertical: 8, borderRadius: 6, backgroundColor: '#f1f5f9', alignItems: 'center' },
  sevBtnText: { fontSize: 12, fontWeight: '600', textTransform: 'capitalize', color: '#64748b' },
  submitBtn: { flexDirection: 'row', backgroundColor: '#dc2626', paddingVertical: 12, borderRadius: 8, alignItems: 'center', justifyContent: 'center', gap: 6 },
  submitText: { color: '#fff', fontWeight: '600', fontSize: 14 },
  incidentCard: { marginHorizontal: 16, marginBottom: 8, padding: 14, backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#e2e8f0', borderLeftWidth: 4 },
  incHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 },
  incTitle: { fontSize: 14, fontWeight: '600', color: '#1e293b', flex: 1, marginRight: 8 },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 6 },
  badgeText: { fontSize: 11, fontWeight: '700', textTransform: 'capitalize' },
  incDesc: { fontSize: 13, color: '#475569', marginBottom: 6 },
  incPU: { fontSize: 11, color: '#3b82f6', fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace', marginBottom: 4 },
  incFooter: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  incStatus: { fontSize: 12, color: '#64748b', textTransform: 'capitalize', fontWeight: '600' },
  incTime: { fontSize: 11, color: '#94a3b8' },
  actionRow: { flexDirection: 'row', gap: 8, marginTop: 10, paddingTop: 10, borderTopWidth: 1, borderTopColor: '#f1f5f9' },
  actionBtn: { flexDirection: 'row', alignItems: 'center', gap: 4, paddingHorizontal: 10, paddingVertical: 6, borderRadius: 6 },
  actionText: { fontSize: 12, fontWeight: '600' },
});
