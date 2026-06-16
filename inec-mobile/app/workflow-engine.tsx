import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface Workflow { id: number; name: string; status: string; type: string; started_at: string; completed_at: string; steps_total: number; steps_completed: number; }

export default function WorkflowEngineScreen() {
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const res = await fetch(`${API}/workflows`);
      if (res.ok) { const d = await res.json(); setWorkflows(Array.isArray(d) ? d : d.workflows || []); }
    } catch (e) { console.error('Workflow load:', e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const statusColors: Record<string, string> = { running: '#3b82f6', completed: '#16a34a', failed: '#dc2626', paused: '#f59e0b', pending: '#94a3b8' };

  return (
    <View style={s.container}>
      <Text style={s.title}>Workflow Engine</Text>
      <Text style={s.count}>{workflows.length} workflows</Text>
      <FlatList data={workflows} keyExtractor={w => String(w.id)}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <Text style={s.name}>{item.name}</Text>
              <View style={[s.badge, { backgroundColor: statusColors[item.status] || '#6b7280' }]}>
                <Text style={s.badgeText}>{item.status}</Text>
              </View>
            </View>
            <Text style={s.sub}>Type: {item.type}</Text>
            <View style={s.progressBar}>
              <View style={[s.progressFill, { width: `${item.steps_total > 0 ? (item.steps_completed / item.steps_total) * 100 : 0}%` }]} />
            </View>
            <Text style={s.progressText}>{item.steps_completed}/{item.steps_total} steps</Text>
          </View>
        )}
      />
    </View>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 4 },
  count: { fontSize: 13, color: '#64748b', marginBottom: 12 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  row: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  name: { fontSize: 15, fontWeight: '600', color: '#1e293b', flex: 1 },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 10, marginLeft: 8 },
  badgeText: { fontSize: 11, color: '#fff', fontWeight: '600' },
  sub: { fontSize: 13, color: '#64748b', marginTop: 4 },
  progressBar: { height: 6, backgroundColor: '#e2e8f0', borderRadius: 3, marginTop: 10 },
  progressFill: { height: 6, backgroundColor: '#3b82f6', borderRadius: 3 },
  progressText: { fontSize: 11, color: '#64748b', marginTop: 4 },
});
