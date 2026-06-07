import { useState, useEffect, useCallback } from 'react';
import { View, Text, ScrollView, TouchableOpacity, ActivityIndicator, RefreshControl, Dimensions } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

const { width } = Dimensions.get('window');

interface PartyTotal { party: string; votes: number }
interface TVData {
  total_pus: number; reported_pus: number; completion_pct: number;
  total_votes: number; party_totals: PartyTotal[];
  state_results: Record<string, PartyTotal[]>; last_updated: string;
}

const partyColors: Record<string, string> = {
  APC: '#2563eb', PDP: '#dc2626', LP: '#16a34a', NNPP: '#d97706',
  APGA: '#7c3aed', SDP: '#0891b2', ADC: '#db2777', YPP: '#65a30d',
};

export default function TVDashboardScreen() {
  const [data, setData] = useState<TVData | null>(null);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [selectedState, setSelectedState] = useState('');

  const load = useCallback(async () => {
    try {
      const res = await api<TVData>('/public/tv-dashboard?election_id=1');
      setData(res);
    } catch { setData(null); }
  }, []);

  useEffect(() => { setLoading(true); load().finally(() => setLoading(false)); }, [load]);
  useEffect(() => { const i = setInterval(load, 15000); return () => clearInterval(i); }, [load]);

  const onRefresh = async () => { setRefreshing(true); await load(); setRefreshing(false); Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success); };

  if (loading && !data) return <View style={{ flex: 1, justifyContent: 'center', alignItems: 'center', backgroundColor: '#0f172a' }}><ActivityIndicator size="large" color="#3b82f6" /></View>;

  const maxVotes = (data?.party_totals?.[0]?.votes || 1);
  const states = Object.keys(data?.state_results || {});

  return (
    <ScrollView style={{ flex: 1, backgroundColor: '#0f172a' }} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#fff" />}>
      <View style={{ padding: 16 }}>
        <Text style={{ fontSize: 28, fontWeight: '800', color: '#fff', textAlign: 'center', marginBottom: 4 }} accessibilityRole="header">INEC Live Results</Text>
        <Text style={{ fontSize: 12, color: '#64748b', textAlign: 'center', marginBottom: 20 }}>
          Updated: {data ? new Date(data.last_updated).toLocaleTimeString() : '-'}
        </Text>

        <View style={{ flexDirection: 'row', gap: 8, marginBottom: 20 }}>
          {[
            { label: 'Total PUs', value: data?.total_pus?.toLocaleString() || '0', color: '#3b82f6' },
            { label: 'Reporting', value: data?.reported_pus?.toLocaleString() || '0', color: '#22c55e' },
            { label: 'Complete', value: `${data?.completion_pct?.toFixed(1) || '0'}%`, color: '#eab308' },
          ].map(s => (
            <View key={s.label} style={{ flex: 1, backgroundColor: '#1e293b', borderRadius: 12, padding: 12, alignItems: 'center' }}>
              <Text style={{ fontSize: 22, fontWeight: '700', color: s.color }}>{s.value}</Text>
              <Text style={{ fontSize: 10, color: '#64748b', marginTop: 2 }}>{s.label}</Text>
            </View>
          ))}
        </View>

        <View style={{ backgroundColor: '#1e293b', borderRadius: 8, height: 8, marginBottom: 20, overflow: 'hidden' }}>
          <View style={{ height: 8, borderRadius: 8, backgroundColor: '#22c55e', width: `${data?.completion_pct || 0}%` }} />
        </View>

        <Text style={{ fontSize: 18, fontWeight: '700', color: '#fff', marginBottom: 12 }}>National Results</Text>
        {data?.party_totals?.map(p => (
          <View key={p.party} style={{ marginBottom: 10 }}>
            <View style={{ flexDirection: 'row', justifyContent: 'space-between', marginBottom: 4 }}>
              <Text style={{ color: partyColors[p.party] || '#9ca3af', fontWeight: '700', fontSize: 15 }}>{p.party}</Text>
              <Text style={{ color: '#94a3b8', fontSize: 13 }}>{p.votes.toLocaleString()}</Text>
            </View>
            <View style={{ backgroundColor: '#1e293b', borderRadius: 6, height: 24, overflow: 'hidden' }}>
              <View style={{ height: 24, borderRadius: 6, backgroundColor: partyColors[p.party] || '#6b7280', width: `${(p.votes / maxVotes) * 100}%` }} />
            </View>
          </View>
        ))}

        <Text style={{ fontSize: 14, color: '#64748b', textAlign: 'center', marginTop: 8, marginBottom: 20 }}>
          Total Votes: <Text style={{ color: '#fff', fontWeight: '700' }}>{data?.total_votes?.toLocaleString() || '0'}</Text>
        </Text>

        <Text style={{ fontSize: 18, fontWeight: '700', color: '#fff', marginBottom: 12 }}>State Results</Text>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ marginBottom: 12 }}>
          {states.slice(0, 15).map(s => (
            <TouchableOpacity key={s} onPress={() => { setSelectedState(s); Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light); }}
              style={{ backgroundColor: selectedState === s ? '#3b82f6' : '#1e293b', paddingHorizontal: 14, paddingVertical: 8, borderRadius: 16, marginRight: 6 }}
              accessibilityLabel={`Show results for state ${s}`}>
              <Text style={{ color: selectedState === s ? '#fff' : '#94a3b8', fontSize: 12, fontWeight: '600' }}>{s}</Text>
            </TouchableOpacity>
          ))}
        </ScrollView>

        {selectedState && data?.state_results[selectedState] && (
          <View style={{ backgroundColor: '#1e293b', borderRadius: 12, padding: 14 }}>
            {data.state_results[selectedState].slice(0, 5).map(p => {
              const stMax = data.state_results[selectedState][0]?.votes || 1;
              return (
                <View key={p.party} style={{ flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                  <Text style={{ width: 40, color: partyColors[p.party] || '#9ca3af', fontWeight: '600', fontSize: 13 }}>{p.party}</Text>
                  <View style={{ flex: 1, backgroundColor: '#334155', borderRadius: 4, height: 16, overflow: 'hidden' }}>
                    <View style={{ height: 16, borderRadius: 4, backgroundColor: partyColors[p.party] || '#6b7280', width: `${(p.votes / stMax) * 100}%` }} />
                  </View>
                  <Text style={{ width: 50, color: '#94a3b8', fontSize: 11, textAlign: 'right' }}>{p.votes.toLocaleString()}</Text>
                </View>
              );
            })}
          </View>
        )}
      </View>
    </ScrollView>
  );
}
