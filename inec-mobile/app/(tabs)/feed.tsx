import { useEffect, useState, useRef, useCallback } from 'react';
import {
  View, Text, FlatList, StyleSheet, TouchableOpacity, RefreshControl,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { observerApi, getToken, API_URL, ObserverStats } from '../../src/lib/api';
import { syncPendingData, getPendingReportCount } from '../../src/lib/offline';

interface SSEEvent {
  id: string;
  type: string;
  data: Record<string, unknown>;
  time: string;
}

export default function FeedScreen() {
  const [events, setEvents] = useState<SSEEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [stats, setStats] = useState<ObserverStats | null>(null);
  const [pendingCount, setPendingCount] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  const eventSourceRef = useRef<EventSource | null>(null);

  const connectSSE = useCallback(async () => {
    const token = await getToken();
    if (!token) return;

    // Close existing connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const url = `${API_URL}/observer/stream?token=${token}`;
    const es = new EventSource(url);

    es.onopen = () => setConnected(true);

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
      const data = JSON.parse(e.data);
      setEvents((prev) => [{
        id: `result-${Date.now()}`,
        type: 'result_submitted',
        data,
        time: new Date().toLocaleTimeString(),
      }, ...prev].slice(0, 50));
    });

    es.addEventListener('observer_checkin', (e: MessageEvent) => {
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
      // Reconnect after 5 seconds
      setTimeout(connectSSE, 5000);
    };

    eventSourceRef.current = es;
  }, []);

  const loadStats = useCallback(async () => {
    try {
      const data = await observerApi.stats();
      setStats(data);
    } catch { /* ignore */ }
  }, []);

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
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

    return () => {
      eventSourceRef.current?.close();
    };
  }, [connectSSE, loadStats]);

  const renderEvent = ({ item }: { item: SSEEvent }) => (
    <View style={styles.eventCard}>
      <View style={styles.eventHeader}>
        <Ionicons
          name={item.type === 'result_submitted' ? 'document-text' : item.type === 'observer_checkin' ? 'location' : 'radio'}
          size={18}
          color="#166534"
        />
        <Text style={styles.eventType}>{item.type.replace('_', ' ')}</Text>
        <Text style={styles.eventTime}>{item.time}</Text>
      </View>
      <Text style={styles.eventData} numberOfLines={2}>
        {JSON.stringify(item.data).slice(0, 120)}
      </Text>
    </View>
  );

  return (
    <View style={styles.container}>
      {/* Status Bar */}
      <View style={styles.statusBar}>
        <View style={[styles.dot, { backgroundColor: connected ? '#22c55e' : '#ef4444' }]} />
        <Text style={styles.statusText}>{connected ? 'Live' : 'Disconnected'}</Text>
        {pendingCount > 0 && (
          <View style={styles.pendingBadge}>
            <Text style={styles.pendingText}>{pendingCount} pending</Text>
          </View>
        )}
        <TouchableOpacity onPress={onRefresh} style={styles.syncButton}>
          <Ionicons name="sync-outline" size={18} color="#166534" />
        </TouchableOpacity>
      </View>

      {/* Stats Cards */}
      {stats && (
        <View style={styles.statsRow}>
          <View style={styles.statCard}>
            <Text style={styles.statValue}>{stats.total_observers}</Text>
            <Text style={styles.statLabel}>Observers</Text>
          </View>
          <View style={styles.statCard}>
            <Text style={styles.statValue}>{stats.active_check_ins}</Text>
            <Text style={styles.statLabel}>Check-ins</Text>
          </View>
          <View style={styles.statCard}>
            <Text style={styles.statValue}>{stats.reports_today}</Text>
            <Text style={styles.statLabel}>Reports</Text>
          </View>
          <View style={styles.statCard}>
            <Text style={styles.statValue}>{stats.active_sse_streams}</Text>
            <Text style={styles.statLabel}>Streams</Text>
          </View>
        </View>
      )}

      {/* Events List */}
      <FlatList
        data={events}
        keyExtractor={(item) => item.id}
        renderItem={renderEvent}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} colors={['#166534']} />}
        ListEmptyComponent={
          <Text style={styles.emptyText}>Waiting for live results...</Text>
        }
        contentContainerStyle={events.length === 0 ? styles.emptyContainer : undefined}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  statusBar: {
    flexDirection: 'row', alignItems: 'center', padding: 12,
    backgroundColor: '#fff', borderBottomWidth: 1, borderBottomColor: '#e5e7eb',
  },
  dot: { width: 8, height: 8, borderRadius: 4, marginRight: 6 },
  statusText: { fontSize: 13, fontWeight: '600', color: '#374151' },
  pendingBadge: {
    marginLeft: 12, backgroundColor: '#fef3c7', paddingHorizontal: 8, paddingVertical: 2, borderRadius: 10,
  },
  pendingText: { fontSize: 11, color: '#92400e' },
  syncButton: { marginLeft: 'auto', padding: 4 },
  statsRow: { flexDirection: 'row', padding: 12, gap: 8 },
  statCard: {
    flex: 1, backgroundColor: '#fff', padding: 12, borderRadius: 8,
    alignItems: 'center', borderWidth: 1, borderColor: '#e5e7eb',
  },
  statValue: { fontSize: 20, fontWeight: 'bold', color: '#166534' },
  statLabel: { fontSize: 10, color: '#6b7280', marginTop: 2 },
  eventCard: {
    backgroundColor: '#fff', marginHorizontal: 12, marginVertical: 4,
    padding: 12, borderRadius: 8, borderWidth: 1, borderColor: '#e5e7eb',
  },
  eventHeader: { flexDirection: 'row', alignItems: 'center', gap: 6, marginBottom: 4 },
  eventType: { fontSize: 13, fontWeight: '600', color: '#166534', textTransform: 'capitalize' },
  eventTime: { marginLeft: 'auto', fontSize: 11, color: '#9ca3af' },
  eventData: { fontSize: 12, color: '#6b7280' },
  emptyText: { fontSize: 14, color: '#9ca3af', textAlign: 'center' },
  emptyContainer: { flex: 1, justifyContent: 'center', alignItems: 'center' },
});
