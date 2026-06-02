import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, RefreshControl, TouchableOpacity, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { observerApi, PartyDashboard } from '../../src/lib/api';
import { getCurrentLocation } from '../../src/lib/location';
import { EmptyState } from '../../src/components/EmptyState';
import { CardSkeleton } from '../../src/components/SkeletonLoader';

const PARTIES = ['APC', 'PDP', 'LP', 'NNPP', 'ADC', 'SDP', 'APGA', 'YPP'];

const PARTY_COLORS: Record<string, string> = {
  APC: '#0074D9', PDP: '#FF4136', LP: '#2ECC40', NNPP: '#B10DC9',
  ADC: '#FF851B', SDP: '#FFDC00', APGA: '#7FDBFF', YPP: '#01FF70',
};

export default function DashboardScreen() {
  const [selectedParty, setSelectedParty] = useState('APC');
  const [data, setData] = useState<PartyDashboard | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [loading, setLoading] = useState(true);
  const [checkedIn, setCheckedIn] = useState(false);
  const [checkInStatus, setCheckInStatus] = useState('');
  const [checkingIn, setCheckingIn] = useState(false);

  const loadDashboard = useCallback(async () => {
    try {
      const result = await observerApi.partyDashboard(selectedParty);
      setData(result);
    } catch { /* ignore */ }
    setLoading(false);
  }, [selectedParty]);

  useFocusEffect(useCallback(() => { loadDashboard(); }, [loadDashboard]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await loadDashboard();
    setRefreshing(false);
  }, [loadDashboard]);

  const handleCheckIn = async () => {
    setCheckingIn(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    const location = await getCurrentLocation();
    if (!location) {
      setCheckInStatus('Location unavailable');
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setCheckingIn(false);
      return;
    }

    try {
      const result = await observerApi.checkIn('PU-001', location.latitude, location.longitude);
      setCheckedIn(result.within_geofence);
      setCheckInStatus(
        result.within_geofence
          ? `Checked in (${Math.round(result.distance_m)}m from PU)`
          : `Outside geofence (${Math.round(result.distance_m)}m away)`
      );
      Haptics.notificationAsync(
        result.within_geofence
          ? Haptics.NotificationFeedbackType.Success
          : Haptics.NotificationFeedbackType.Warning
      );
    } catch (e: unknown) {
      setCheckInStatus(e instanceof Error ? e.message : 'Check-in failed');
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
    } finally {
      setCheckingIn(false);
    }
  };

  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} colors={['#166534']} tintColor="#166534" />}
      showsVerticalScrollIndicator={false}
    >
      <View style={styles.checkInCard}>
        <View style={styles.checkInHeader}>
          <View style={[styles.checkInIcon, { backgroundColor: checkedIn ? '#dcfce7' : '#f3f4f6' }]}>
            <Ionicons name={checkedIn ? 'checkmark-circle' : 'location'} size={20} color={checkedIn ? '#166534' : '#6b7280'} />
          </View>
          <View style={{ flex: 1 }}>
            <Text style={styles.checkInTitle}>Polling Unit Check-In</Text>
            <Text style={styles.checkInSubtitle}>Verify your location at the assigned PU</Text>
          </View>
        </View>
        <TouchableOpacity
          style={[styles.checkInButton, checkedIn && styles.checkInButtonChecked]}
          onPress={handleCheckIn}
          disabled={checkingIn}
          activeOpacity={0.8}
        >
          <Ionicons name={checkedIn ? 'checkmark-circle' : 'navigate'} size={18} color="#fff" />
          <Text style={styles.checkInButtonText}>
            {checkingIn ? 'Checking in...' : checkedIn ? 'Checked In' : 'Check In Now'}
          </Text>
        </TouchableOpacity>
        {checkInStatus ? (
          <View style={[styles.checkInStatusBox, { backgroundColor: checkedIn ? '#f0fdf4' : '#fef2f2' }]}>
            <Ionicons name={checkedIn ? 'checkmark-circle' : 'alert-circle'} size={14} color={checkedIn ? '#166534' : '#dc2626'} />
            <Text style={[styles.checkInStatusText, { color: checkedIn ? '#166534' : '#dc2626' }]}>
              {checkInStatus}
            </Text>
          </View>
        ) : null}
      </View>

      <View style={styles.section}>
        <Text style={styles.sectionTitle}>Select Party</Text>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.partyRow}>
          {PARTIES.map((party) => {
            const isActive = selectedParty === party;
            const color = PARTY_COLORS[party] || '#166534';
            return (
              <TouchableOpacity
                key={party}
                style={[styles.partyChip, isActive && { backgroundColor: color, borderColor: color }]}
                onPress={() => {
                  Haptics.selectionAsync();
                  setSelectedParty(party);
                  setLoading(true);
                }}
                activeOpacity={0.7}
              >
                <Text style={[styles.partyChipText, isActive && { color: '#fff' }]}>{party}</Text>
              </TouchableOpacity>
            );
          })}
        </ScrollView>
      </View>

      {loading ? (
        <View style={{ padding: 12, gap: 8 }}>
          <CardSkeleton />
          <CardSkeleton />
        </View>
      ) : data ? (
        <>
          <View style={styles.statsGrid}>
            {([
              { value: data.total_votes.toLocaleString(), label: 'Total Votes', icon: 'trending-up' as const, color: '#166534' },
              { value: data.polling_units_with_results.toLocaleString(), label: 'PUs Reported', icon: 'checkmark-done' as const, color: '#2563eb' },
              { value: data.total_polling_units.toLocaleString(), label: 'Total PUs', icon: 'business' as const, color: '#7c3aed' },
              { value: `${data.coverage_pct}%`, label: 'Coverage', icon: 'pie-chart' as const, color: '#166534' },
            ]).map((s) => (
              <View key={s.label} style={styles.statCard}>
                <Ionicons name={s.icon} size={18} color={s.color} style={{ marginBottom: 4 }} />
                <Text style={styles.statValue}>{s.value}</Text>
                <Text style={styles.statLabel}>{s.label}</Text>
              </View>
            ))}
          </View>

          {data.state_breakdown.length > 0 && (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>State Breakdown</Text>
              {data.state_breakdown.slice(0, 10).map((state, i) => (
                <View key={i} style={styles.stateRow}>
                  <View style={styles.stateRank}>
                    <Text style={styles.stateRankText}>{i + 1}</Text>
                  </View>
                  <Text style={styles.stateName}>{state.state}</Text>
                  <Text style={styles.stateVotes}>{state.votes.toLocaleString()}</Text>
                </View>
              ))}
            </View>
          )}

          {data.recent_results.length > 0 && (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Recent Results</Text>
              {data.recent_results.slice(0, 10).map((result, i) => (
                <View key={i} style={styles.resultRow}>
                  <Ionicons name="document-text-outline" size={16} color="#9ca3af" />
                  <Text style={styles.resultPU} numberOfLines={1}>{result.polling_unit}</Text>
                  <Text style={styles.resultVotes}>{result.votes}</Text>
                </View>
              ))}
            </View>
          )}
        </>
      ) : (
        <EmptyState
          icon="stats-chart-outline"
          title="No data available"
          description="Select a party to view dashboard statistics"
        />
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  checkInCard: {
    backgroundColor: '#fff',
    margin: 12,
    padding: 16,
    borderRadius: 16,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.06,
    shadowRadius: 8,
    elevation: 2,
  },
  checkInHeader: { flexDirection: 'row', alignItems: 'center', gap: 12, marginBottom: 14 },
  checkInIcon: {
    width: 44,
    height: 44,
    borderRadius: 14,
    alignItems: 'center',
    justifyContent: 'center',
  },
  checkInTitle: { fontSize: 16, fontWeight: '700', color: '#111827' },
  checkInSubtitle: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  checkInButton: {
    backgroundColor: '#166534',
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    paddingVertical: 14,
    borderRadius: 12,
    shadowColor: '#166534',
    shadowOffset: { width: 0, height: 3 },
    shadowOpacity: 0.2,
    shadowRadius: 6,
    elevation: 3,
  },
  checkInButtonChecked: { backgroundColor: '#059669' },
  checkInButtonText: { color: '#fff', fontWeight: '700', fontSize: 15 },
  checkInStatusBox: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginTop: 10,
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 8,
  },
  checkInStatusText: { fontSize: 13, fontWeight: '500' },
  section: { paddingHorizontal: 12, marginBottom: 8 },
  sectionTitle: { fontSize: 16, fontWeight: '700', color: '#111827', marginBottom: 10 },
  partyRow: { gap: 8, paddingVertical: 4 },
  partyChip: {
    paddingHorizontal: 20,
    paddingVertical: 10,
    borderRadius: 24,
    backgroundColor: '#fff',
    borderWidth: 1.5,
    borderColor: '#e5e7eb',
  },
  partyChipText: { fontSize: 14, fontWeight: '600', color: '#374151' },
  statsGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    paddingHorizontal: 12,
    paddingBottom: 8,
    gap: 8,
  },
  statCard: {
    flex: 1,
    minWidth: '45%',
    backgroundColor: '#fff',
    paddingVertical: 16,
    paddingHorizontal: 12,
    borderRadius: 14,
    alignItems: 'center',
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.04,
    shadowRadius: 3,
    elevation: 1,
  },
  statValue: { fontSize: 22, fontWeight: '800', color: '#111827' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 4, fontWeight: '500' },
  stateRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    backgroundColor: '#fff',
    padding: 14,
    borderRadius: 10,
    marginBottom: 6,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.03,
    shadowRadius: 2,
    elevation: 1,
  },
  stateRank: {
    width: 24,
    height: 24,
    borderRadius: 12,
    backgroundColor: '#f3f4f6',
    alignItems: 'center',
    justifyContent: 'center',
  },
  stateRankText: { fontSize: 11, fontWeight: '700', color: '#6b7280' },
  stateName: { fontSize: 14, color: '#374151', flex: 1 },
  stateVotes: { fontSize: 14, fontWeight: '700', color: '#166534' },
  resultRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    backgroundColor: '#fff',
    padding: 14,
    borderRadius: 10,
    marginBottom: 6,
  },
  resultPU: { fontSize: 13, color: '#374151', flex: 1 },
  resultVotes: { fontSize: 14, fontWeight: '700', color: '#111827' },
});
