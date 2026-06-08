import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface DuplicateGroup { id: number; match_type: string; confidence: number; voter_count: number; status: string; state_code: string; }

export default function DuplicateDetectionScreen() {
  const [groups, setGroups] = useState<DuplicateGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const res = await fetch(`${API}/dedup/groups`);
      if (res.ok) { const d = await res.json(); setGroups(Array.isArray(d) ? d : d.groups || []); }
    } catch (e) { console.error('Dedup load:', e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  return (
    <View style={s.container}>
      <Text style={s.title}>Duplicate Detection</Text>
      <Text style={s.count}>{groups.length} potential duplicate groups found</Text>
      <FlatList data={groups} keyExtractor={g => String(g.id)}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <Text style={s.type}>{item.match_type?.replace('_', ' ')}</Text>
              <Text style={[s.status, { color: item.status === 'resolved' ? '#16a34a' : '#f59e0b' }]}>{item.status}</Text>
            </View>
            <Text style={s.sub}>{item.voter_count} voters · State: {item.state_code}</Text>
            <View style={s.confBar}>
              <View style={[s.confFill, { width: `${item.confidence}%`, backgroundColor: item.confidence > 90 ? '#dc2626' : item.confidence > 70 ? '#f59e0b' : '#3b82f6' }]} />
            </View>
            <Text style={s.confText}>{item.confidence.toFixed(1)}% match confidence</Text>
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
  type: { fontSize: 15, fontWeight: '600', color: '#1e293b', textTransform: 'capitalize' },
  status: { fontSize: 13, fontWeight: '600' },
  sub: { fontSize: 13, color: '#64748b', marginTop: 4 },
  confBar: { height: 6, backgroundColor: '#e2e8f0', borderRadius: 3, marginTop: 10 },
  confFill: { height: 6, borderRadius: 3 },
  confText: { fontSize: 11, color: '#64748b', marginTop: 4 },
});
