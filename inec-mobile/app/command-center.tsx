import { useState, useEffect, useRef } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Platform, ActivityIndicator } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface CommandCenterData {
  total_polling_units: number;
  results_received: number;
  stalled_count: number;
  completion_pct: number;
  alert_count: number;
  load_shedding_level: number;
  state_velocities: Array<{
    state_code: string;
    state_name: string;
    total_pus: number;
    reported: number;
    pct: number;
    status: string;
  }>;
  live_feed: Array<{
    polling_unit_code: string;
    state_name: string;
    total_votes: number;
    submitted_at: string;
  }>;
}

export default function CommandCenterScreen() {
  const [data, setData] = useState<CommandCenterData | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdate, setLastUpdate] = useState<Date | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const loadData = async () => {
    setLoading(true);
    try {
      const d = await api<CommandCenterData>('/command-center/state');
      setData(d);
      setLastUpdate(new Date());
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
    intervalRef.current = setInterval(loadData, 15000);
    return () => { if (intervalRef.current) clearInterval(intervalRef.current); };
  }, []);

  const setLoadShedding = async (level: number) => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      await api('/load-shedding', { method: 'POST', body: JSON.stringify({ level }) });
      loadData();
    } catch { /* ignore */ }
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      <View style={styles.header}>
        <Ionicons name="grid" size={28} color="#166534" />
        <Text style={styles.title}>Command Center</Text>
        {lastUpdate && <Text style={styles.lastUpdate}>{lastUpdate.toLocaleTimeString()}</Text>}
      </View>

      {loading && !data && <ActivityIndicator size="large" color="#166534" style={{ marginTop: 40 }} />}
      {error && !data && (
        <View style={styles.errorCard}>
          <Text style={styles.errorText}>{error}</Text>
          <TouchableOpacity style={styles.retryBtn} onPress={loadData}>
            <Text style={styles.retryText}>Retry</Text>
          </TouchableOpacity>
        </View>
      )}

      {data && (
        <>
          <View style={styles.progressCard}>
            <View style={styles.progressBarBg}>
              <View style={[styles.progressBarFill, { width: `${Math.min(data.completion_pct, 100)}%` }]} />
            </View>
            <Text style={styles.progressText}>{data.completion_pct.toFixed(1)}% Reporting</Text>
          </View>

          <View style={styles.statsRow}>
            {[
              { label: 'Total PUs', value: data.total_polling_units, color: '#3b82f6' },
              { label: 'Reported', value: data.results_received, color: '#22c55e' },
              { label: 'Stalled', value: data.stalled_count, color: '#ef4444' },
              { label: 'Alerts', value: data.alert_count, color: '#f59e0b' },
            ].map((s) => (
              <View key={s.label} style={[styles.statCard, { borderTopColor: s.color, borderTopWidth: 3 }]}>
                <Text style={styles.statValue}>{(s.value || 0).toLocaleString()}</Text>
                <Text style={styles.statLabel}>{s.label}</Text>
              </View>
            ))}
          </View>

          <View style={styles.section}>
            <Text style={styles.sectionTitle}>Load Shedding</Text>
            <View style={styles.lsRow}>
              {[0, 1, 2, 3].map((lvl) => (
                <TouchableOpacity
                  key={lvl}
                  style={[styles.lsBtn, data.load_shedding_level === lvl && styles.lsBtnActive]}
                  onPress={() => setLoadShedding(lvl)}
                >
                  <Text style={[styles.lsBtnText, data.load_shedding_level === lvl && styles.lsBtnTextActive]}>
                    {lvl === 0 ? 'Off' : `L${lvl}`}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>
          </View>

          {data.state_velocities && data.state_velocities.length > 0 && (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>State Progress ({data.state_velocities.length})</Text>
              {data.state_velocities.slice(0, 10).map((sv) => (
                <View key={sv.state_code} style={styles.stateRow}>
                  <Text style={styles.stateName}>{sv.state_name || sv.state_code}</Text>
                  <View style={{ flex: 1, marginHorizontal: 8 }}>
                    <View style={styles.miniBarBg}>
                      <View style={[styles.miniBarFill, { width: `${Math.min(sv.pct, 100)}%` }]} />
                    </View>
                  </View>
                  <Text style={styles.statePct}>{sv.pct.toFixed(0)}%</Text>
                </View>
              ))}
            </View>
          )}

          {data.live_feed && data.live_feed.length > 0 && (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Live Feed</Text>
              {data.live_feed.slice(0, 5).map((f, i) => (
                <View key={i} style={styles.feedItem}>
                  <Ionicons name="radio-button-on" size={10} color="#22c55e" />
                  <View style={{ flex: 1, marginLeft: 8 }}>
                    <Text style={styles.feedPU}>{f.polling_unit_code}</Text>
                    <Text style={styles.feedState}>{f.state_name} — {f.total_votes} votes</Text>
                  </View>
                </View>
              ))}
            </View>
          )}
        </>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#0f172a' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 16, paddingTop: Platform.OS === 'ios' ? 60 : 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#f1f5f9', flex: 1 },
  lastUpdate: { fontSize: 11, color: '#94a3b8' },
  errorCard: { margin: 16, padding: 16, backgroundColor: '#451a1a', borderRadius: 12, alignItems: 'center' },
  errorText: { color: '#fca5a5', marginBottom: 8 },
  retryBtn: { paddingHorizontal: 16, paddingVertical: 8, backgroundColor: '#dc2626', borderRadius: 8 },
  retryText: { color: '#fff', fontWeight: '600' },
  progressCard: { margin: 16, padding: 16, backgroundColor: '#1e293b', borderRadius: 12 },
  progressBarBg: { height: 10, backgroundColor: '#334155', borderRadius: 5, overflow: 'hidden' },
  progressBarFill: { height: '100%', backgroundColor: '#22c55e', borderRadius: 5 },
  progressText: { fontSize: 13, color: '#94a3b8', marginTop: 6, textAlign: 'right' },
  statsRow: { flexDirection: 'row', flexWrap: 'wrap', paddingHorizontal: 12, gap: 8 },
  statCard: { flex: 1, minWidth: '45%', backgroundColor: '#1e293b', padding: 14, borderRadius: 12, alignItems: 'center' },
  statValue: { fontSize: 20, fontWeight: '700', color: '#f1f5f9' },
  statLabel: { fontSize: 11, color: '#94a3b8', marginTop: 2 },
  section: { margin: 16, padding: 16, backgroundColor: '#1e293b', borderRadius: 12 },
  sectionTitle: { fontSize: 16, fontWeight: '600', color: '#f1f5f9', marginBottom: 12 },
  lsRow: { flexDirection: 'row', gap: 8 },
  lsBtn: { flex: 1, paddingVertical: 10, borderRadius: 8, backgroundColor: '#334155', alignItems: 'center' },
  lsBtnActive: { backgroundColor: '#166534' },
  lsBtnText: { color: '#94a3b8', fontWeight: '600' },
  lsBtnTextActive: { color: '#fff' },
  stateRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 6 },
  stateName: { width: 60, fontSize: 12, color: '#94a3b8' },
  statePct: { width: 36, fontSize: 12, color: '#f1f5f9', textAlign: 'right' },
  miniBarBg: { height: 6, backgroundColor: '#334155', borderRadius: 3, overflow: 'hidden' },
  miniBarFill: { height: '100%', backgroundColor: '#22c55e', borderRadius: 3 },
  feedItem: { flexDirection: 'row', alignItems: 'center', paddingVertical: 8, borderBottomWidth: 1, borderBottomColor: '#334155' },
  feedPU: { fontSize: 13, fontWeight: '600', color: '#f1f5f9' },
  feedState: { fontSize: 11, color: '#94a3b8' },
});
