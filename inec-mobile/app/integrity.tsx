import { useState, useEffect, useCallback } from 'react';
import { View, Text, ScrollView, TouchableOpacity, TextInput, ActivityIndicator, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface IntegrityResult {
  polling_unit_code: string;
  composite_score: number;
  benford_compliance: number;
  geofence_compliance: number;
  timing_score: number;
  observer_presence: number;
  anomaly_score: number;
  rating: string;
  total_votes: number;
}

export default function IntegrityScreen() {
  const [data, setData] = useState<IntegrityResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [search, setSearch] = useState('');

  const load = useCallback(async () => {
    try {
      const res = await api<{ heatmap: IntegrityResult[]; total: number }>('/ai/integrity-heatmap?election_id=1');
      setData(res.heatmap || []);
    } catch { setData([]); }
  }, []);

  useEffect(() => { setLoading(true); load().finally(() => setLoading(false)); }, [load]);

  const onRefresh = async () => { setRefreshing(true); await load(); setRefreshing(false); Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success); };

  const ratingColor = (r: string) => {
    if (r === 'excellent') return '#16a34a';
    if (r === 'good') return '#2563eb';
    if (r === 'fair') return '#d97706';
    return '#dc2626';
  };

  const filtered = data.filter(d => !search || d.polling_unit_code.toLowerCase().includes(search.toLowerCase()));
  const avgScore = filtered.length > 0 ? filtered.reduce((s, r) => s + r.composite_score, 0) / filtered.length : 0;

  return (
    <ScrollView style={{ flex: 1, backgroundColor: '#f8fafc' }} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}>
      <View style={{ padding: 16 }}>
        <Text style={{ fontSize: 24, fontWeight: '700', color: '#0f172a', marginBottom: 16 }} accessibilityRole="header">AI Integrity Score</Text>

        <TextInput placeholder="Search PU code..." value={search} onChangeText={setSearch}
          style={{ backgroundColor: '#fff', borderRadius: 10, padding: 12, marginBottom: 16, borderWidth: 1, borderColor: '#e2e8f0' }}
          accessibilityLabel="Search by polling unit code" />

        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ marginBottom: 16 }}>
          {[
            { label: 'Analyzed', value: data.length, bg: '#f1f5f9', color: '#334155' },
            { label: 'Excellent', value: data.filter(d => d.rating === 'excellent').length, bg: '#f0fdf4', color: '#16a34a' },
            { label: 'Good', value: data.filter(d => d.rating === 'good').length, bg: '#eff6ff', color: '#2563eb' },
            { label: 'Fair/Poor', value: data.filter(d => d.rating === 'fair' || d.rating === 'poor').length, bg: '#fef2f2', color: '#dc2626' },
            { label: 'Avg Score', value: `${(avgScore * 100).toFixed(0)}%`, bg: '#faf5ff', color: '#7c3aed' },
          ].map(s => (
            <View key={s.label} style={{ backgroundColor: s.bg, borderRadius: 10, padding: 12, marginRight: 8, minWidth: 80, alignItems: 'center' }}>
              <Text style={{ fontSize: 20, fontWeight: '700', color: s.color }}>{s.value}</Text>
              <Text style={{ fontSize: 11, color: '#64748b', marginTop: 2 }}>{s.label}</Text>
            </View>
          ))}
        </ScrollView>

        {loading && <ActivityIndicator size="large" style={{ marginTop: 40 }} />}

        {!loading && filtered.map(item => (
          <TouchableOpacity key={item.polling_unit_code} onPress={() => Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light)}
            style={{ backgroundColor: '#fff', borderRadius: 12, padding: 14, marginBottom: 10, borderLeftWidth: 4, borderLeftColor: ratingColor(item.rating) }}
            accessibilityLabel={`Polling unit ${item.polling_unit_code}, score ${(item.composite_score * 100).toFixed(0)}%, ${item.rating}`}>
            <View style={{ flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' }}>
              <View>
                <Text style={{ fontWeight: '600', fontSize: 14, fontFamily: 'monospace' }}>{item.polling_unit_code}</Text>
                <Text style={{ fontSize: 12, color: '#64748b', marginTop: 2 }}>{item.total_votes.toLocaleString()} votes</Text>
              </View>
              <View style={{ alignItems: 'flex-end' }}>
                <Text style={{ fontSize: 22, fontWeight: '700', color: ratingColor(item.rating) }}>{(item.composite_score * 100).toFixed(0)}%</Text>
                <View style={{ backgroundColor: ratingColor(item.rating) + '20', paddingHorizontal: 8, paddingVertical: 2, borderRadius: 12, marginTop: 2 }}>
                  <Text style={{ fontSize: 10, fontWeight: '600', color: ratingColor(item.rating), textTransform: 'capitalize' }}>{item.rating}</Text>
                </View>
              </View>
            </View>
            <View style={{ marginTop: 10, gap: 4 }}>
              {[
                { label: 'Benford', score: item.benford_compliance },
                { label: 'Geofence', score: item.geofence_compliance },
                { label: 'Timing', score: item.timing_score },
                { label: 'Observer', score: item.observer_presence },
                { label: 'Anomaly', score: item.anomaly_score },
              ].map(b => (
                <View key={b.label} style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
                  <Text style={{ width: 60, fontSize: 10, color: '#94a3b8' }}>{b.label}</Text>
                  <View style={{ flex: 1, height: 4, backgroundColor: '#e2e8f0', borderRadius: 2 }}>
                    <View style={{ width: `${b.score * 100}%`, height: 4, borderRadius: 2, backgroundColor: b.score >= 0.8 ? '#16a34a' : b.score >= 0.5 ? '#d97706' : '#dc2626' }} />
                  </View>
                  <Text style={{ width: 30, fontSize: 10, color: '#94a3b8', textAlign: 'right' }}>{(b.score * 100).toFixed(0)}%</Text>
                </View>
              ))}
            </View>
          </TouchableOpacity>
        ))}

        {!loading && filtered.length === 0 && (
          <View style={{ alignItems: 'center', marginTop: 40 }}>
            <Ionicons name="shield-checkmark-outline" size={48} color="#94a3b8" />
            <Text style={{ color: '#94a3b8', marginTop: 8 }}>No integrity data available</Text>
          </View>
        )}
      </View>
    </ScrollView>
  );
}
