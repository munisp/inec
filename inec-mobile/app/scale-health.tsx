import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, RefreshControl, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { DeviceGatewayHealth, DeviceGatewayOperationalStatus, externalElectionApi, IReVPortalStatus, scaleApi } from '../src/lib/api';
import { CardSkeleton } from '../src/components/SkeletonLoader';

interface MiddlewareMode {
  Name: string;
  IsReal: boolean;
  Connection: string;
}

export default function ScaleHealthScreen() {
  const [health, setHealth] = useState<Record<string, unknown> | null>(null);
  const [middlewareModes, setMiddlewareModes] = useState<MiddlewareMode[]>([]);
  const [deviceHealth, setDeviceHealth] = useState<DeviceGatewayHealth | null>(null);
  const [deviceTrust, setDeviceTrust] = useState<DeviceGatewayOperationalStatus | null>(null);
  const [irevStatus, setIReVStatus] = useState<IReVPortalStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const loadData = useCallback(async () => {
    try {
      const [h, m, gateway, trust, portal] = await Promise.all([
        scaleApi.health().catch(() => null),
        scaleApi.middlewareModes().catch(() => []),
        externalElectionApi.deviceGatewayHealth().catch(() => ({ status: 'unavailable' } as DeviceGatewayHealth)),
        externalElectionApi.deviceGatewayStatus().catch(() => null),
        externalElectionApi.irevStatus().catch(() => null),
      ]);
      if (h) setHealth(h);
      setMiddlewareModes(m);
      setDeviceHealth(gateway);
      setDeviceTrust(trust);
      setIReVStatus(portal);
    } catch { /* */ }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { loadData(); }, [loadData]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await loadData();
    setRefreshing(false);
  }, [loadData]);

  if (loading) return <View style={styles.container}><CardSkeleton /><CardSkeleton /><CardSkeleton /></View>;

  const renderValue = (val: unknown): string => {
    if (val === null || val === undefined) return '-';
    if (typeof val === 'object') return JSON.stringify(val);
    return String(val);
  };

  const renderSection = (title: string, data: Record<string, unknown>, icon: keyof typeof Ionicons.glyphMap, color: string) => (
    <View style={styles.sectionCard}>
      <View style={styles.sectionHeader}>
        <View style={[styles.sectionIcon, { backgroundColor: `${color}15` }]}>
          <Ionicons name={icon} size={18} color={color} />
        </View>
        <Text style={styles.sectionTitle}>{title}</Text>
      </View>
      {Object.entries(data).map(([key, value]) => {
        if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
          return (
            <View key={key} style={styles.nestedSection}>
              <Text style={styles.nestedTitle}>{key.replace(/_/g, ' ')}</Text>
              {Object.entries(value as Record<string, unknown>).map(([k, v]) => (
                <View key={k} style={styles.kvRow}>
                  <Text style={styles.kvKey}>{k.replace(/_/g, ' ')}</Text>
                  <Text style={styles.kvValue}>{renderValue(v)}</Text>
                </View>
              ))}
            </View>
          );
        }
        return (
          <View key={key} style={styles.kvRow}>
            <Text style={styles.kvKey}>{key.replace(/_/g, ' ')}</Text>
            <Text style={styles.kvValue}>{renderValue(value)}</Text>
          </View>
        );
      })}
    </View>
  );

  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} colors={['#166534']} tintColor="#166534" />}
    >
      <View style={styles.headerCard}>
        <Ionicons name="pulse" size={28} color="#059669" />
        <Text style={styles.headerTitle}>System Health</Text>
        <Text style={styles.headerSubtitle}>Real-time platform scaling and middleware status</Text>
      </View>

      {health && (
        <>
          {health.database && renderSection('Database', health.database as Record<string, unknown>, 'server-outline', '#2563eb')}
          {health.ingestion_queue && renderSection('Ingestion Queue', health.ingestion_queue as Record<string, unknown>, 'layers-outline', '#f59e0b')}
          {health.websocket && renderSection('WebSocket', health.websocket as Record<string, unknown>, 'wifi-outline', '#7c3aed')}
          {health.rate_limiter && renderSection('Rate Limiter', health.rate_limiter as Record<string, unknown>, 'shield-outline', '#dc2626')}
        </>
      )}

      <View style={[styles.sectionCard, deviceHealth?.status === 'ready' ? styles.gatewayReady : styles.gatewayReview]}>
        <View style={styles.sectionHeader}>
          <View style={[styles.sectionIcon, { backgroundColor: deviceHealth?.status === 'ready' ? '#dcfce7' : '#fef3c7' }]}>
            <Ionicons name="shield-checkmark-outline" size={18} color={deviceHealth?.status === 'ready' ? '#166534' : '#b45309'} />
          </View>
          <View style={{ flex: 1 }}><Text style={styles.sectionTitle}>BVAS gateway and IReV boundary</Text><Text style={styles.gatewayNote}>mTLS device ingress, redacted outbox delivery, and authorised receipt state</Text></View>
        </View>
        <View style={styles.kvRow}><Text style={styles.kvKey}>Gateway status</Text><Text style={styles.kvValue}>{deviceHealth?.status || 'unavailable'}</Text></View>
        <View style={styles.kvRow}><Text style={styles.kvKey}>Active enrollments</Text><Text style={styles.kvValue}>{String(deviceTrust?.enrollments?.active ?? '—')}</Text></View>
        <View style={styles.kvRow}><Text style={styles.kvKey}>Delivered events</Text><Text style={styles.kvValue}>{String(deviceTrust?.delivery?.delivered ?? '—')}</Text></View>
        <View style={styles.kvRow}><Text style={styles.kvKey}>IReV portal state</Text><Text style={styles.kvValue}>{irevStatus?.status || 'unavailable'}</Text></View>
        <View style={styles.kvRow}><Text style={styles.kvKey}>Acknowledged receipts</Text><Text style={styles.kvValue}>{String(irevStatus?.submissions?.acknowledged ?? '—')}</Text></View>
        {deviceHealth?.status !== 'ready' ? <Text style={styles.gatewayWarning}>Device events remain fail closed until every required gateway component is ready. Unavailable data is not a readiness confirmation.</Text> : null}
      </View>

      {middlewareModes.length > 0 && (
        <View style={styles.sectionCard}>
          <View style={styles.sectionHeader}>
            <View style={[styles.sectionIcon, { backgroundColor: '#dcfce715' }]}>
              <Ionicons name="git-network-outline" size={18} color="#166534" />
            </View>
            <Text style={styles.sectionTitle}>Middleware Status</Text>
          </View>
          {middlewareModes.map((m, i) => (
            <View key={i} style={styles.mwRow}>
              <View style={[styles.mwDot, { backgroundColor: m.IsReal ? '#22c55e' : '#f59e0b' }]} />
              <View style={{ flex: 1 }}>
                <Text style={styles.mwName}>{m.Name}</Text>
                <Text style={styles.mwConnection} numberOfLines={1}>{m.Connection}</Text>
              </View>
              <View style={[styles.mwBadge, { backgroundColor: m.IsReal ? '#f0fdf4' : '#fffbeb' }]}>
                <Text style={[styles.mwBadgeText, { color: m.IsReal ? '#22c55e' : '#f59e0b' }]}>
                  {m.IsReal ? 'REAL' : 'EMBEDDED'}
                </Text>
              </View>
            </View>
          ))}
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  headerCard: { margin: 16, marginBottom: 8, backgroundColor: '#fff', borderRadius: 16, padding: 20, alignItems: 'center', gap: 4, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  headerTitle: { fontSize: 18, fontWeight: '700', color: '#111827' },
  headerSubtitle: { fontSize: 13, color: '#6b7280', textAlign: 'center' },
  sectionCard: { margin: 16, marginTop: 8, backgroundColor: '#fff', borderRadius: 14, padding: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  sectionHeader: { flexDirection: 'row', alignItems: 'center', gap: 10, marginBottom: 12 },
  sectionIcon: { width: 32, height: 32, borderRadius: 10, justifyContent: 'center', alignItems: 'center' },
  sectionTitle: { fontSize: 15, fontWeight: '700', color: '#111827' },
  kvRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 6, borderBottomWidth: 1, borderBottomColor: '#f9fafb' },
  kvKey: { fontSize: 13, color: '#6b7280', textTransform: 'capitalize', flex: 1 },
  kvValue: { fontSize: 13, fontWeight: '600', color: '#111827', maxWidth: '50%', textAlign: 'right' },
  nestedSection: { marginTop: 8, marginBottom: 4, backgroundColor: '#f9fafb', borderRadius: 8, padding: 10 },
  nestedTitle: { fontSize: 12, fontWeight: '700', color: '#374151', textTransform: 'capitalize', marginBottom: 4 },
  mwRow: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f9fafb' },
  mwDot: { width: 8, height: 8, borderRadius: 4 },
  mwName: { fontSize: 13, fontWeight: '600', color: '#111827' },
  mwConnection: { fontSize: 11, color: '#9ca3af', marginTop: 2 },
  mwBadge: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 6 },
  mwBadgeText: { fontSize: 10, fontWeight: '700' },
  gatewayReady: { borderWidth: 1, borderColor: '#bbf7d0' },
  gatewayReview: { borderWidth: 1, borderColor: '#fde68a', backgroundColor: '#fffbeb' },
  gatewayNote: { color: '#6b7280', fontSize: 10, marginTop: 2 },
  gatewayWarning: { color: '#92400e', fontSize: 11, lineHeight: 16, marginTop: 10 },
});
