import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface Stakeholder { id: number; name: string; type: string; organization: string; email: string; phone: string; status: string; }

export default function StakeholderScreen() {
  const [items, setItems] = useState<Stakeholder[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const res = await fetch(`${API}/stakeholders`);
      if (res.ok) { const d = await res.json(); setItems(Array.isArray(d) ? d : d.stakeholders || []); }
    } catch (e) { console.error('Stakeholder load:', e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const typeColors: Record<string, string> = { party_agent: '#3b82f6', observer: '#8b5cf6', media: '#f59e0b', ngo: '#10b981', government: '#ef4444' };

  return (
    <View style={s.container}>
      <Text style={s.title}>Stakeholders</Text>
      <Text style={s.count}>{items.length} registered</Text>
      <FlatList data={items} keyExtractor={i => String(i.id)}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <Text style={s.name}>{item.name}</Text>
              <View style={[s.badge, { backgroundColor: typeColors[item.type] || '#6b7280' }]}>
                <Text style={s.badgeText}>{item.type?.replace('_', ' ')}</Text>
              </View>
            </View>
            <Text style={s.sub}>{item.organization}</Text>
            <Text style={s.sub}>{item.email} · {item.phone}</Text>
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
  name: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 10 },
  badgeText: { fontSize: 11, color: '#fff', fontWeight: '600' },
  sub: { fontSize: 13, color: '#64748b', marginTop: 4 },
});
