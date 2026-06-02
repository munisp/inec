import { useEffect, useState, useRef, useCallback } from 'react';
import {
  View, Text, FlatList, StyleSheet, TouchableOpacity, RefreshControl, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { observerApi, getToken, API_URL, ObserverStats } from '../../src/lib/api';
import { syncPendingData, getPendingReportCount } from '../../src/lib/offline';
import { EmptyState } from '../../src/components/EmptyState';
import { StatsSkeleton } from '../../src/components/SkeletonLoader';

interface SSEEvent {
  id: string;
  type: string;
  data: Record<string, unknown>;
  time: string;
}

const EVENT_ICONS: Record<string, { name: keyof typeof Ionicons.glyphMap; color: string; bg: string }> = {
  result_submitted: { name: 'document-text', color: '#166534', bg: '#dcfce7' },
  observer_checkin: { name: 'location', color: '#2563eb', bg: '#dbeafe' },
  connected: { name: 'radio', color: '#7c3aed', bg: '#ede9fe' },
  anomaly_detected: { name: 'warning', color: '#dc2626', bg: '#fef2f2' },
};

export default function FeedScreen() {
  const [events, setEvents] = useState<SSEEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [stats, setStats] = useState<ObserverStats | null>(null);
  const [pendingCount, setPendingCount] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingStats, setLoadingStats] = useState(true);
  const eventSourceRef = useRef<EventSource | null>(null);

  const connectSSE = useCallback(async () => {
    const token = await getToken();
    if (!token) return;

    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const url = `${API_URL}/observer/stream?token=${token}`;
    const es = new EventSource(url);

    es.onopen = () => {
      setConnected(true);
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    };

    es.addEventListener('connected', (e: MessageEvent) => {
      setConnected(true);
      const data = JSON.parse(e.data);
      setEvents((prev) => [{
        id: data.subscriber_id,
        type: 'connected',
        data,
        time: new Date().toLocaleTimeString(),
      }, ...prev].slice(0, 50));
    });

    es.addEventListener('result_submitted', (e: MessageEvent) => {
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
      const data = JSON.parse(e.data);
      setEvents((prev) => [{
        id: `result-${Date.now()}`,
        type: 'result_submitted',
        data,
        time: new Date().toLocaleTimeString(),
      }, ...prev].slice(0, 50));
    });

    es.addEventListener('observer_checkin', (e: MessageEvent) => {
      Haptics.selectionAsync();
      const data = JSON.parse(e.data);
      setEvents((prev) => [{
        id: `checkin-${Date.now()}`,
        type: 'observer_checkin',
        data,
        time: new Date().toLocaleTimeString(),
      }, ...prev].slice(0, 50));
    });

    es.onerror = () => {
      setConnected(false);
      es.close();
      setTimeout(connectSSE, 5000);
    };

    eventSourceRef.current = es;
  }, []);

  const loadStats = useCallback(async () => {
    try {
      const data = await observerApi.stats();
      setStats(data);
    } catch { /* ignore */ }
    setLoadingStats(false);
  }, []);

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await Promise.all([
      loadStats(),
      syncPendingData(),
      getPendingReportCount().then(setPendingCount),
    ]);
    setRefreshing(false);
  }, [loadStats]);

  useEffect(() => {
    connectSSE();
    loadStats();
    getPendingReportCount().then(setPendingCount);
    return () => { eventSourceRef.current?.close(); };
  }, [connectSSE, loadStats]);

  const renderEvent = ({ item }: { item: SSEEvent }) => {
    const cfg = EVENT_ICONS[item.type] || EVENT_ICONS.connected;
    return (
      <View style={styles.eventCard}>
        <View style={[styles.eventIconCircle, { backgroundColor: cfg.bg }]}>
          <Ionicons name={cfg.name} size={16} color={cfg.color} />
        </View>
        <View style={styles.eventContent}>
          <View style={styles.eventHeader}>
            <Text style={styles.eventType}>{item.type.replace(/_/g, ' ')}</Text>
            <Text style={styles.eventTime}>{item.time}</Text>
          </View>
          <Text style={styles.eventData} numberOfLines={2}>
            {JSON.stringify(item.data).slice(0, 120)}
          </Text>
        </View>
      </View>
    );
  };

  return (
    <View style={styles.container}>
      <View style={styles.statusBar}>
        <View style={styles.statusLeft}>
          <View style={[styles.dot, { backgroundColor: connected ? '#22c55e' : '#ef4444' }]} />
          <Text style={styles.statusText}>{connected ? 'Live' : 'Offline'}</Text>
        </View>
        {pendingCount > 0 && (
          <View style={styles.pendingBadge}>
            <Ionicons name="cloud-upload-outline" size={12} color="#92400e" />
            <Text style={styles.pendingText}>{pendingCount} pending</Text>
          </View>
        )}
        <TouchableOpacity onPress={onRefresh} style={styles.syncButton} activeOpacity={0.7}>
          <Ionicons name="sync-outline" size={20} color="#166534" />
        </TouchableOpacity>
      </View>

      {loadingStats ? <StatsSkeleton /> : stats && (
        <View style={styles.statsRow}>
          {([
            { value: stats.total_observers, label: 'Observers', icon: 'people' as const },
            { value: stats.active_check_ins, label: 'Check-ins', icon: 'location' as const },
            { value: stats.reports_today, label: 'Reports', icon: 'document-text' as const },
            { value: stats.active_sse_streams, label: 'Streams', icon: 'radio' as const },
          ]).map((s) => (
            <View key={s.label} style={styles.statCard}>
              <Ionicons name={s.icon} size={16} color="#166534" style={{ marginBottom: 4 }} />
              <Text style={styles.statValue}>{s.value}</Text>
              <Text style={styles.statLabel}>{s.label}</Text>
            </View>
          ))}
        </View>
      )}

      <FlatList
        data={events}
        keyExtractor={(item) => item.id}
        renderItem={renderEvent}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={onRefresh}
            colors={['#166534']}
            tintColor="#166534"
          />
        }
        ListEmptyComponent={
          <EmptyState
            icon="radio-outline"
            title="No events yet"
            description="Live election results and observer check-ins will appear here in real time"
          />
        }
        contentContainerStyle={events.length === 0 ? styles.emptyContainer : styles.listContent}
        showsVerticalScrollIndicator={false}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  statusBar: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 16,
    paddingVertical: 10,
    backgroundColor: '#fff',
    borderBottomWidth: 1,
    borderBottomColor: '#f3f4f6',
  },
  statusLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    flex: 1,
  },
  dot: {
    width: 8,
    height: 8,
    borderRadius: 4,
    marginRight: 8,
  },
  statusText: { fontSize: 14, fontWeight: '600', color: '#374151' },
  pendingBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    backgroundColor: '#fef3c7',
    paddingHorizontal: 10,
    paddingVertical: 4,
    borderRadius: 12,
    marginRight: 8,
  },
  pendingText: { fontSize: 12, color: '#92400e', fontWeight: '500' },
  syncButton: {
    width: 36,
    height: 36,
    borderRadius: 18,
    backgroundColor: '#f0fdf4',
    alignItems: 'center',
    justifyContent: 'center',
  },
  statsRow: {
    flexDirection: 'row',
    paddingHorizontal: 12,
    paddingVertical: 12,
    gap: 8,
  },
  statCard: {
    flex: 1,
    backgroundColor: '#fff',
    paddingVertical: 14,
    paddingHorizontal: 8,
    borderRadius: 12,
    alignItems: 'center',
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.04,
    shadowRadius: 3,
    elevation: 1,
  },
  statValue: { fontSize: 20, fontWeight: '700', color: '#111827' },
  statLabel: { fontSize: 10, color: '#6b7280', marginTop: 2, fontWeight: '500' },
  listContent: {
    paddingHorizontal: 12,
    paddingTop: 8,
    paddingBottom: Platform.OS === 'ios' ? 100 : 80,
  },
  eventCard: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: 12,
    backgroundColor: '#fff',
    marginBottom: 8,
    padding: 14,
    borderRadius: 12,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.04,
    shadowRadius: 3,
    elevation: 1,
  },
  eventIconCircle: {
    width: 36,
    height: 36,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 2,
  },
  eventContent: { flex: 1 },
  eventHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: 4,
  },
  eventType: { fontSize: 14, fontWeight: '600', color: '#111827', textTransform: 'capitalize' },
  eventTime: { fontSize: 11, color: '#9ca3af' },
  eventData: { fontSize: 12, color: '#6b7280', lineHeight: 18 },
  emptyContainer: { flex: 1 },
});
