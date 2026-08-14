import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator, TouchableOpacity } from 'react-native';
import { api } from '../src/lib/api';

// R4-52: /workflows never existed. The real route GET
// /middleware/temporal/workflows returns only the engine status (there is no
// workflow-list endpoint), so this screen reports engine health honestly.
export default function WorkflowEngineScreen() {
  const [status, setStatus] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const [loading, setLoading] = useState(true);

  const load = async () => {
    setFailed(false);
    try {
      const d = await api<{ status: string }>('/middleware/temporal/workflows');
      setStatus(d.status || 'unknown');
    } catch (e) { console.error('Workflow engine status:', e); setStatus(null); setFailed(true); }
    setLoading(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  return (
    <View style={s.container}>
      <Text style={s.title}>Workflow Engine</Text>
      <Text style={s.count}>Temporal orchestration engine</Text>
      <View style={s.card}>
        <View style={s.row}>
          <Text style={s.name}>Temporal engine</Text>
          <View style={[s.badge, { backgroundColor: failed ? '#dc2626' : '#16a34a' }]}>
            <Text style={s.badgeText}>{failed ? 'unreachable' : (status || 'unknown')}</Text>
          </View>
        </View>
        <Text style={s.sub}>Individual workflow status and execution history are managed from the web console (Middleware → Temporal).</Text>
      </View>
      <TouchableOpacity style={s.retry} onPress={() => { setLoading(true); load(); }}>
        <Text style={s.retryText}>Refresh</Text>
      </TouchableOpacity>
    </View>
  );
}

const s = StyleSheet.create({
  retry: { backgroundColor: '#16a34a', borderRadius: 10, padding: 12, alignItems: 'center', marginTop: 8 },
  retryText: { color: '#fff', fontWeight: '600' },
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
