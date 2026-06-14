import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface WarRoomData {
  active_volunteers: number;
  total_dispatched: number;
  pending_rides: number;
  incidents_today: number;
  coverage_pct: number;
  canvass_trails: number;
  alerts: { message: string; severity: string; timestamp: string }[];
}

export default function WarRoomScreen() {
  const [data, setData] = useState<WarRoomData | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const d = await apiGet('/gotv/warroom/dashboard');
      setData(d);
    } catch {}
  };

  useEffect(() => { load(); const iv = setInterval(load, 15000); return () => clearInterval(iv); }, []);

  const metrics = [
    { label: 'Active Volunteers', value: data?.active_volunteers || 0, icon: 'people' as const, color: '#15803d' },
    { label: 'Dispatched', value: data?.total_dispatched || 0, icon: 'send' as const, color: '#2563eb' },
    { label: 'Pending Rides', value: data?.pending_rides || 0, icon: 'car' as const, color: '#ca8a04' },
    { label: 'Incidents', value: data?.incidents_today || 0, icon: 'alert-circle' as const, color: '#dc2626' },
    { label: 'Coverage', value: `${(data?.coverage_pct || 0).toFixed(0)}%`, icon: 'map' as const, color: '#7c3aed' },
    { label: 'Canvass Trails', value: data?.canvass_trails || 0, icon: 'walk' as const, color: '#ea580c' },
  ];

  return (
    <ScrollView style={s.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}>
      <View style={s.liveIndicator}>
        <View style={s.liveDot} />
        <Text style={s.liveText}>LIVE — refreshes every 15s</Text>
      </View>

      <View style={s.grid}>
        {metrics.map((m) => (
          <View key={m.label} style={s.metricCard}>
            <Ionicons name={m.icon} size={20} color={m.color} />
            <Text style={s.metricValue}>{m.value}</Text>
            <Text style={s.metricLabel}>{m.label}</Text>
          </View>
        ))}
      </View>

      <Text style={s.sectionTitle}>Live Alerts</Text>
      {(data?.alerts || []).length === 0 ? (
        <View style={s.emptyAlerts}>
          <Ionicons name="checkmark-circle" size={24} color="#15803d" />
          <Text style={s.emptyText}>No active alerts</Text>
        </View>
      ) : (
        data?.alerts.map((alert, i) => (
          <View key={i} style={[s.alertCard, { borderLeftColor: alert.severity === 'critical' ? '#dc2626' : '#ca8a04' }]}>
            <Text style={s.alertMsg}>{alert.message}</Text>
            <Text style={s.alertTime}>{new Date(alert.timestamp).toLocaleTimeString()}</Text>
          </View>
        ))
      )}
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#0f172a' },
  liveIndicator: { flexDirection: 'row', alignItems: 'center', gap: 6, padding: 12, justifyContent: 'center' },
  liveDot: { width: 8, height: 8, borderRadius: 4, backgroundColor: '#dc2626' },
  liveText: { fontSize: 11, color: '#94a3b8', fontWeight: '600', textTransform: 'uppercase' },
  grid: { flexDirection: 'row', flexWrap: 'wrap', padding: 12, gap: 10 },
  metricCard: { width: '47%', backgroundColor: '#1e293b', borderRadius: 12, padding: 14, alignItems: 'center' },
  metricValue: { fontSize: 24, fontWeight: '700', color: '#fff', marginTop: 6 },
  metricLabel: { fontSize: 11, color: '#94a3b8', marginTop: 2 },
  sectionTitle: { fontSize: 16, fontWeight: '700', color: '#e2e8f0', paddingHorizontal: 16, marginTop: 8 },
  emptyAlerts: { flexDirection: 'row', alignItems: 'center', gap: 8, justifyContent: 'center', padding: 24 },
  emptyText: { color: '#64748b', fontSize: 14 },
  alertCard: { backgroundColor: '#1e293b', borderLeftWidth: 3, borderRadius: 8, padding: 12, marginHorizontal: 16, marginTop: 8 },
  alertMsg: { fontSize: 13, color: '#e2e8f0' },
  alertTime: { fontSize: 11, color: '#64748b', marginTop: 4 },
});
