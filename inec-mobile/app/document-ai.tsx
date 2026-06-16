import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface DocAnalysis { id: number; document_type: string; status: string; confidence: number; extracted_fields: number; created_at: string; }

export default function DocumentAIScreen() {
  const [analyses, setAnalyses] = useState<DocAnalysis[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try { const r = await fetch(`${API}/document-ai/analyses`); if (r.ok) { const d = await r.json(); setAnalyses(Array.isArray(d) ? d : d.analyses || []); } } catch (e) { console.error(e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  return (
    <View style={s.container}>
      <Text style={s.title}>Document AI</Text>
      <Text style={s.subtitle}>AI-powered document analysis and extraction</Text>
      <FlatList data={analyses} keyExtractor={a => String(a.id)}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <Text style={s.type}>{item.document_type}</Text>
              <View style={[s.badge, { backgroundColor: item.status === 'completed' ? '#dcfce7' : '#fef3c7' }]}>
                <Text style={{ fontSize: 11, fontWeight: '600', color: item.status === 'completed' ? '#16a34a' : '#d97706' }}>{item.status}</Text>
              </View>
            </View>
            <Text style={s.sub}>Confidence: {(item.confidence * 100).toFixed(1)}% · {item.extracted_fields} fields extracted</Text>
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
  subtitle: { fontSize: 14, color: '#64748b', marginBottom: 12 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  row: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  type: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 10 },
  sub: { fontSize: 13, color: '#64748b', marginTop: 6 },
});
