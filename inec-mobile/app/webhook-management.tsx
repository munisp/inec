import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl, TouchableOpacity } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface Webhook { id: number; url: string; events: string[]; status: string; last_triggered: string; success_rate: number; }

export default function WebhookManagementScreen() {
  const [hooks, setHooks] = useState<Webhook[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try { const r = await fetch(`${API}/webhooks`); if (r.ok) { const d = await r.json(); setHooks(Array.isArray(d) ? d : d.webhooks || []); } } catch (e) { console.error(e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  return (
    <View style={s.container}>
      <Text style={s.title}>Webhook Management</Text>
      <Text style={s.count}>{hooks.length} configured webhooks</Text>
      <FlatList data={hooks} keyExtractor={h => String(h.id)}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <Text style={s.url} numberOfLines={1}>{item.url}</Text>
              <View style={[s.badge, { backgroundColor: item.status === 'active' ? '#dcfce7' : '#fee2e2' }]}>
                <Text style={{ color: item.status === 'active' ? '#16a34a' : '#dc2626', fontSize: 11, fontWeight: '600' }}>{item.status}</Text>
              </View>
            </View>
            <Text style={s.events}>{(item.events || []).join(', ')}</Text>
            <Text style={s.sub}>Success rate: {(item.success_rate || 0).toFixed(0)}%</Text>
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
  url: { fontSize: 13, fontFamily: 'monospace', color: '#1e293b', flex: 1 },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 10, marginLeft: 8 },
  events: { fontSize: 12, color: '#3b82f6', marginTop: 6 },
  sub: { fontSize: 12, color: '#64748b', marginTop: 4 },
});
