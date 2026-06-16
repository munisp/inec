import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet, TouchableOpacity, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface ConventionStats {
  total_delegates: number;
  accredited_delegates: number;
  checked_in: number;
  total_aspirants: number;
  screened_aspirants: number;
  active_rounds: number;
  quorum_met: boolean;
  turnout_pct: number;
}

export default function PrimariesDashboardScreen({ navigation }: any) {
  const [stats, setStats] = useState<ConventionStats | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/gotv/primaries/elections/1/dashboard');
      setStats(data);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  const items = [
    { title: 'Aspirants', icon: 'person-add' as const, screen: 'Aspirants', value: stats?.total_aspirants || 0, color: '#7c3aed' },
    { title: 'Delegates', icon: 'people' as const, screen: 'Delegates', value: stats?.total_delegates || 0, color: '#2563eb' },
    { title: 'Voting Rounds', icon: 'checkmark-circle' as const, screen: 'Voting', value: stats?.active_rounds || 0, color: '#15803d' },
    { title: 'Remote Voting', icon: 'phone-portrait' as const, screen: 'RemoteVoting', value: null, color: '#ea580c' },
  ];

  return (
    <ScrollView style={s.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}>
      <View style={s.header}>
        <Text style={s.title}>Convention Dashboard</Text>
        <View style={[s.quorumBadge, { backgroundColor: stats?.quorum_met ? '#dcfce7' : '#fef3c7' }]}>
          <Ionicons name={stats?.quorum_met ? 'checkmark-circle' : 'warning'} size={14} color={stats?.quorum_met ? '#15803d' : '#ca8a04'} />
          <Text style={[s.quorumText, { color: stats?.quorum_met ? '#15803d' : '#ca8a04' }]}>
            {stats?.quorum_met ? 'Quorum Met' : 'No Quorum'}
          </Text>
        </View>
      </View>

      <View style={s.statsGrid}>
        <View style={s.bigStat}>
          <Text style={s.bigValue}>{(stats?.turnout_pct || 0).toFixed(1)}%</Text>
          <Text style={s.bigLabel}>Turnout</Text>
        </View>
        <View style={s.statsColumn}>
          <View style={s.smallStat}>
            <Text style={s.smallValue}>{stats?.accredited_delegates || 0}</Text>
            <Text style={s.smallLabel}>Accredited</Text>
          </View>
          <View style={s.smallStat}>
            <Text style={s.smallValue}>{stats?.checked_in || 0}</Text>
            <Text style={s.smallLabel}>Checked In</Text>
          </View>
        </View>
      </View>

      <View style={s.grid}>
        {items.map((item) => (
          <TouchableOpacity key={item.title} style={s.menuCard} onPress={() => navigation.navigate(item.screen)}>
            <View style={[s.menuIcon, { backgroundColor: item.color + '15' }]}>
              <Ionicons name={item.icon} size={22} color={item.color} />
            </View>
            <Text style={s.menuTitle}>{item.title}</Text>
            {item.value != null && <Text style={s.menuValue}>{item.value}</Text>}
          </TouchableOpacity>
        ))}
      </View>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', padding: 16 },
  title: { fontSize: 20, fontWeight: '700', color: '#1e293b' },
  quorumBadge: { flexDirection: 'row', alignItems: 'center', gap: 4, paddingHorizontal: 10, paddingVertical: 4, borderRadius: 8 },
  quorumText: { fontSize: 12, fontWeight: '600' },
  statsGrid: { flexDirection: 'row', padding: 16, paddingTop: 0, gap: 12 },
  bigStat: { flex: 1, backgroundColor: '#7c3aed', borderRadius: 16, padding: 20, justifyContent: 'center', alignItems: 'center' },
  bigValue: { fontSize: 36, fontWeight: '700', color: '#fff' },
  bigLabel: { fontSize: 13, color: '#e2d4f5', marginTop: 4 },
  statsColumn: { flex: 1, gap: 12 },
  smallStat: { flex: 1, backgroundColor: '#fff', borderRadius: 12, padding: 14, justifyContent: 'center', alignItems: 'center', elevation: 1 },
  smallValue: { fontSize: 22, fontWeight: '700', color: '#1e293b' },
  smallLabel: { fontSize: 11, color: '#64748b', marginTop: 2 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', padding: 12, gap: 12 },
  menuCard: { width: '47%', backgroundColor: '#fff', borderRadius: 14, padding: 16, elevation: 2, alignItems: 'center' },
  menuIcon: { width: 48, height: 48, borderRadius: 14, justifyContent: 'center', alignItems: 'center', marginBottom: 10 },
  menuTitle: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  menuValue: { fontSize: 18, fontWeight: '700', color: '#7c3aed', marginTop: 4 },
});
