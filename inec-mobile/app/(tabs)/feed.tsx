import { useEffect, useState, useRef, useCallback } from 'react';
import {
  View, Text, FlatList, StyleSheet, TouchableOpacity, RefreshControl, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { fetch as expoFetch } from 'expo/fetch';
import { observerApi, getToken, API_URL, ObserverStats } from '../../src/lib/api';
import { syncPendingData, syncPendingResults, getPendingReportCount } from '../../src/lib/offline';
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
  const abortRef = useRef<AbortController | null>(null);
  const retryRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mountedRef = useRef(true);

  const handleSSEEvent = useCallback((type: string, raw: string) => {
    let data: Record<string, unknown>;
    try {
      data = JSON.parse(raw);
    } catch {
      return; // malformed frame — drop it
    }
    if (!mountedRef.current) return;
    if (type === 'connected') {
      setConnected(true);
      setEvents((prev) => [{
        id: String(data.subscriber_id ?? `conn-${Date.now()}`),
        type: 'connected',
        data,
        time: new Date().toLocaleTimeString(),
      }, ...prev].slice(0, 50));
    } else if (type === 'result_submitted') {
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
      setEvents((prev) => [{
        id: `result-${Date.now()}`,
        type: 'result_submitted',
        data,
        time: new Date().toLocaleTimeString(),
      }, ...prev].slice(0, 50));
    } else if (type === 'observer_checkin') {
      Haptics.selectionAsync();
      setEvents((prev) => [{
        id: `checkin-${Date.now()}`,
        type: 'observer_checkin',
        data,
        time: new Date().toLocaleTimeString(),
      }, ...prev].slice(0, 50));
    }
  }, []);

  // Header-authenticated SSE: the JWT travels in the Authorization header —
  // NEVER as a ?token= query parameter, which leaks into server logs, proxies
  // and crash reports. expo/fetch supports streaming response bodies (the
  // stock RN fetch/EventSource cannot send Authorization on SSE).
  const connectSSE = useCallback(async () => {
    const token = await getToken();
    if (!token) return;

    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;

    try {
      const res = await expoFetch(`${API_URL}/observer/stream`, {
        headers: {
          Accept: 'text/event-stream',
          Authorization: `Bearer ${token}`,
        },
        signal: controller.signal,
      });
      if (!res.ok || !res.body) throw new Error(`stream ${res.status}`);
      if (controller.signal.aborted || !mountedRef.current) return;

      setConnected(true);
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let eventType = 'message';
      let dataLines: string[] = [];
      const dispatchFrame = () => {
        if (dataLines.length > 0) handleSSEEvent(eventType, dataLines.join('\n'));
        eventType = 'message';
        dataLines = [];
      };

      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let nl: number;
        while ((nl = buffer.indexOf('\n')) >= 0) {
          const line = buffer.slice(0, nl).replace(/\r$/, '');
          buffer = buffer.slice(nl + 1);
          if (line === '') dispatchFrame();
          else if (line.startsWith(':')) { /* comment/keep-alive */ }
          else if (line.startsWith('event:')) eventType = line.slice(6).trim();
          else if (line.startsWith('data:')) dataLines.push(line.slice(5).replace(/^ /, ''));
        }
      }
      throw new Error('stream ended');
    } catch {
      if (controller.signal.aborted || !mountedRef.current) return;
      setConnected(false);
      retryRef.current = setTimeout(connectSSE, 5000);
    }
  }, [handleSSEEvent]);

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
      syncPendingResults(),
      getPendingReportCount().then(setPendingCount),
    ]);
    setRefreshing(false);
  }, [loadStats]);

  useEffect(() => {
    mountedRef.current = true;
    connectSSE();
    loadStats();
    getPendingReportCount().then(setPendingCount);
    return () => {
      mountedRef.current = false;
      abortRef.current?.abort();
      if (retryRef.current) clearTimeout(retryRef.current);
    };
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
