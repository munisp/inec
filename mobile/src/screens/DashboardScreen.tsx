import React, { useEffect, useState, useCallback } from 'react';
import {
  View, Text, ScrollView, StyleSheet, RefreshControl,
  TouchableOpacity, Dimensions,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface DashboardStats {
  total_elections: number;
  active_elections: number;
  total_results: number;
  total_polling_units: number;
  total_incidents: number;
  results_submitted_pct: number;
}

export default function DashboardScreen({ navigation }: any) {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [online, setOnline] = useState(true);

  const loadData = useCallback(async () => {
    try {
      const data = await apiGet('/dashboard/stats?election_id=1');
      setStats(data);
      setOnline(true);
    } catch {
      setOnline(false);
    }
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  const onRefresh = async () => {
    setRefreshing(true);
    await loadData();
    setRefreshing(false);
  };

  const cards = [
    { title: 'Elections', value: stats?.total_elections || 0, icon: 'shield-checkmark' as const, color: '#15803d', screen: 'Elections' },
    { title: 'Results', value: stats?.total_results || 0, icon: 'bar-chart' as const, color: '#2563eb', screen: 'Results' },
    { title: 'Polling Units', value: stats?.total_polling_units || 0, icon: 'location' as const, color: '#ca8a04', screen: 'Map' },
    { title: 'Incidents', value: stats?.total_incidents || 0, icon: 'alert-circle' as const, color: '#dc2626', screen: 'Incidents' },
  ];

  return (
    <ScrollView
      style={s.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#15803d" />}
    >
      {!online && (
        <View style={s.offlineBanner}>
          <Ionicons name="cloud-offline" size={16} color="#fff" />
          <Text style={s.offlineText}>Offline — showing cached data</Text>
        </View>
      )}

      <View style={s.header}>
        <Text style={s.headerTitle}>Election Dashboard</Text>
        <Text style={s.headerSubtitle}>Real-time election monitoring</Text>
      </View>

      <View style={s.grid}>
        {cards.map((card) => (
          <TouchableOpacity
            key={card.title}
            style={s.card}
            onPress={() => navigation.navigate(card.screen)}
            activeOpacity={0.7}
          >
            <View style={[s.cardIcon, { backgroundColor: card.color + '15' }]}>
              <Ionicons name={card.icon} size={24} color={card.color} />
            </View>
            <Text style={s.cardValue}>{card.value.toLocaleString()}</Text>
            <Text style={s.cardTitle}>{card.title}</Text>
          </TouchableOpacity>
        ))}
      </View>

      {stats && (
        <View style={s.progressCard}>
          <Text style={s.progressTitle}>Results Submission</Text>
          <View style={s.progressBar}>
            <View style={[s.progressFill, { width: `${Math.min(stats.results_submitted_pct || 0, 100)}%` }]} />
          </View>
          <Text style={s.progressText}>{(stats.results_submitted_pct || 0).toFixed(1)}% submitted</Text>
        </View>
      )}

      <View style={s.quickActions}>
        <Text style={s.sectionTitle}>Quick Actions</Text>
        {[
          { title: 'View BVAS Devices', icon: 'scan' as const, screen: 'BVAS' },
          { title: 'Audit Trail', icon: 'document-text' as const, screen: 'Audit' },
          { title: 'Live Map', icon: 'map' as const, screen: 'Map' },
        ].map((action) => (
          <TouchableOpacity
            key={action.title}
            style={s.actionRow}
            onPress={() => navigation.navigate(action.screen)}
          >
            <Ionicons name={action.icon} size={20} color="#15803d" />
            <Text style={s.actionText}>{action.title}</Text>
            <Ionicons name="chevron-forward" size={16} color="#94a3b8" />
          </TouchableOpacity>
        ))}
      </View>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  offlineBanner: { flexDirection: 'row', alignItems: 'center', gap: 8, backgroundColor: '#475569', padding: 8, justifyContent: 'center' },
  offlineText: { color: '#fff', fontSize: 12 },
  header: { padding: 20, paddingBottom: 8 },
  headerTitle: { fontSize: 24, fontWeight: '700', color: '#1e293b' },
  headerSubtitle: { fontSize: 14, color: '#64748b', marginTop: 4 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', padding: 12, gap: 12 },
  card: { width: (Dimensions.get('window').width - 48) / 2, backgroundColor: '#fff', borderRadius: 16, padding: 16, elevation: 2, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 8 },
  cardIcon: { width: 44, height: 44, borderRadius: 12, justifyContent: 'center', alignItems: 'center', marginBottom: 12 },
  cardValue: { fontSize: 28, fontWeight: '700', color: '#1e293b' },
  cardTitle: { fontSize: 13, color: '#64748b', marginTop: 2 },
  progressCard: { margin: 16, backgroundColor: '#fff', borderRadius: 16, padding: 16, elevation: 2 },
  progressTitle: { fontSize: 16, fontWeight: '600', color: '#1e293b', marginBottom: 12 },
  progressBar: { height: 8, backgroundColor: '#e2e8f0', borderRadius: 4, overflow: 'hidden' },
  progressFill: { height: '100%', backgroundColor: '#15803d', borderRadius: 4 },
  progressText: { fontSize: 12, color: '#64748b', marginTop: 6, textAlign: 'right' },
  quickActions: { margin: 16 },
  sectionTitle: { fontSize: 16, fontWeight: '600', color: '#1e293b', marginBottom: 12 },
  actionRow: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#fff', padding: 16, borderRadius: 12, marginBottom: 8, gap: 12, elevation: 1 },
  actionText: { flex: 1, fontSize: 15, color: '#1e293b' },
});
