import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, TouchableOpacity } from 'react-native';
import { api } from '../src/lib/api';

// R4-52: /training/modules never existed; real route is GET /training/courses.
interface TrainingModule { id: number; title: string; course_type: string; target_role: string; difficulty: string; duration_minutes: number; modules_count: number; is_mandatory: boolean; }

export default function TrainingScreen() {
  const [modules, setModules] = useState<TrainingModule[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    (async () => {
      try {
        const d = await api<{ courses: TrainingModule[] } | TrainingModule[]>('/training/courses');
        setModules(Array.isArray(d) ? d : d.courses || []);
      } catch (e) { console.error('Training load:', e); }
      setLoading(false);
    })();
  }, []);

  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const diffColors: Record<string, string> = { beginner: '#16a34a', intermediate: '#f59e0b', advanced: '#ef4444', expert: '#7c3aed' };

  return (
    <View style={s.container}>
      <Text style={s.title}>Training Center</Text>
      <Text style={s.count}>{modules.length} courses available</Text>
      <FlatList data={modules} keyExtractor={m => String(m.id)} renderItem={({ item }) => (
        <TouchableOpacity style={s.card}>
          <View style={s.row}>
            <Text style={s.moduleTitle} numberOfLines={2}>{item.title}</Text>
            {item.is_mandatory && <View style={s.mandatory}><Text style={s.mandatoryText}>Required</Text></View>}
          </View>
          <View style={s.meta}>
            <View style={[s.diffBadge, { backgroundColor: diffColors[item.difficulty] || '#6b7280' }]}>
              <Text style={s.diffText}>{item.difficulty}</Text>
            </View>
            <Text style={s.sub}>{item.duration_minutes} min</Text>
            <Text style={s.sub}>{item.target_role}</Text>
          </View>
          <Text style={s.progressText}>{item.modules_count} modules{item.course_type ? ` · ${item.course_type}` : ''}</Text>
        </TouchableOpacity>
      )} />
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
  moduleTitle: { fontSize: 15, fontWeight: '600', color: '#1e293b', flex: 1 },
  mandatory: { backgroundColor: '#fef2f2', paddingHorizontal: 8, paddingVertical: 2, borderRadius: 8, marginLeft: 8 },
  mandatoryText: { fontSize: 11, color: '#dc2626', fontWeight: '600' },
  meta: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 8 },
  diffBadge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 8 },
  diffText: { fontSize: 11, color: '#fff', fontWeight: '600' },
  sub: { fontSize: 12, color: '#64748b' },
  progressBar: { height: 6, backgroundColor: '#e2e8f0', borderRadius: 3, marginTop: 10 },
  progressFill: { height: 6, backgroundColor: '#16a34a', borderRadius: 3 },
  progressText: { fontSize: 11, color: '#64748b', marginTop: 4 },
});
