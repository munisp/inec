import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet, ActivityIndicator } from 'react-native';
import { API_URL as API } from '../src/lib/api';

export default function PublicAPIScreen() {
  const [spec, setSpec] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    (async () => {
      try { const r = await fetch(`${API}/api/spec`); if (r.ok) setSpec(await r.json()); } catch (e) { console.error(e); }
      setLoading(false);
    })();
  }, []);

  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const endpoints = spec?.paths ? Object.entries(spec.paths).slice(0, 30) : [];

  return (
    <ScrollView style={s.container}>
      <Text style={s.title}>Public API</Text>
      <Text style={s.subtitle}>{spec?.info?.title || 'INEC API'} — v{spec?.info?.version || '1.0'}</Text>
      <Text style={s.count}>{endpoints.length}+ endpoints available</Text>
      {endpoints.map(([path, methods]: [string, any], i) => (
        <View key={i} style={s.card}>
          <Text style={s.path}>{path}</Text>
          <View style={s.methods}>
            {Object.keys(methods).filter(m => m !== 'parameters').map(m => (
              <View key={m} style={[s.methodBadge, { backgroundColor: m === 'get' ? '#3b82f6' : m === 'post' ? '#16a34a' : m === 'put' ? '#f59e0b' : '#ef4444' }]}>
                <Text style={s.methodText}>{m.toUpperCase()}</Text>
              </View>
            ))}
          </View>
        </View>
      ))}
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 4 },
  subtitle: { fontSize: 14, color: '#64748b' },
  count: { fontSize: 13, color: '#64748b', marginBottom: 12 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 12, marginBottom: 8, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  path: { fontSize: 13, fontFamily: 'monospace', color: '#1e293b', fontWeight: '600' },
  methods: { flexDirection: 'row', gap: 6, marginTop: 6 },
  methodBadge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 6 },
  methodText: { fontSize: 10, color: '#fff', fontWeight: '700' },
});
