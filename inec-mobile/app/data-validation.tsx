import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface ValidationResult { id: number; rule_name: string; status: string; severity: string; entity_type: string; entity_id: string; message: string; }

export default function DataValidationScreen() {
  const [results, setResults] = useState<ValidationResult[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const res = await fetch(`${API}/validation/results`);
      if (res.ok) { const d = await res.json(); setResults(Array.isArray(d) ? d : d.results || []); }
    } catch (e) { console.error('Validation load:', e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const sevColors: Record<string, string> = { critical: '#dc2626', error: '#ef4444', warning: '#f59e0b', info: '#3b82f6' };

  return (
    <View style={s.container}>
      <Text style={s.title}>Data Validation</Text>
      <Text style={s.count}>{results.length} validation checks</Text>
      <FlatList data={results} keyExtractor={r => String(r.id)}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <Text style={s.rule}>{item.rule_name}</Text>
              <View style={[s.badge, { backgroundColor: item.status === 'passed' ? '#dcfce7' : '#fee2e2' }]}>
                <Text style={{ color: item.status === 'passed' ? '#16a34a' : '#dc2626', fontSize: 11, fontWeight: '600' }}>{item.status}</Text>
              </View>
            </View>
            <Text style={s.msg}>{item.message}</Text>
            <View style={s.meta}>
              <View style={[s.sevBadge, { backgroundColor: sevColors[item.severity] || '#6b7280' }]}>
                <Text style={s.sevText}>{item.severity}</Text>
              </View>
              <Text style={s.sub}>{item.entity_type}: {item.entity_id}</Text>
            </View>
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
  rule: { fontSize: 14, fontWeight: '600', color: '#1e293b', flex: 1 },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 10, marginLeft: 8 },
  msg: { fontSize: 13, color: '#475569', marginTop: 6 },
  meta: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 8 },
  sevBadge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 8 },
  sevText: { fontSize: 11, color: '#fff', fontWeight: '600' },
  sub: { fontSize: 12, color: '#64748b' },
});
