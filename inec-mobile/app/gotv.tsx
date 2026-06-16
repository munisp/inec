// GOTV Party Portal — dashboard, campaigns, volunteers, rides.
// Uses standalone GOTV mobile auth (phone+OTP, NOT INEC Keycloak).
// Party-scoped: all data isolated per party_id from JWT token.

import { useState, useCallback, useEffect } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  RefreshControl, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect, router } from 'expo-router';
import { EmptyState } from '../src/components/EmptyState';
import { CardSkeleton } from '../src/components/SkeletonLoader';
import { gotvFetch, isAuthenticated, getMobileUser, type GOTVUser } from '../lib/gotv-auth';

// ─── Types ─────────────────────────────────────────────────────────────────

interface DashboardData {
  total_contacts: number;
  total_volunteers: number;
  total_pledges: number;
  active_campaigns: number;
  pending_rides: number;
}

interface Campaign {
  campaign_id: string;
  name: string;
  campaign_type: string;
  status: string;
  total_contacts: number;
  contacts_reached: number;
}

interface Volunteer {
  volunteer_id: string;
  full_name: string;
  role: string;
  is_active: boolean;
  has_vehicle: boolean;
  doors_knocked: number;
  calls_made: number;
  rides_given: number;
}

interface RideRequest {
  request_id: string;
  contact_id: string;
  polling_unit_code: string;
  status: string;
  distance_km: number | null;
}

// ─── Colors ────────────────────────────────────────────────────────────────

const STATUS_COLORS: Record<string, { color: string; bg: string }> = {
  draft: { color: '#6b7280', bg: '#f3f4f6' },
  active: { color: '#22c55e', bg: '#f0fdf4' },
  paused: { color: '#f59e0b', bg: '#fffbeb' },
  completed: { color: '#8b5cf6', bg: '#f5f3ff' },
  pending: { color: '#f59e0b', bg: '#fffbeb' },
  matched: { color: '#3b82f6', bg: '#dbeafe' },
  en_route: { color: '#6366f1', bg: '#eef2ff' },
  dropped_off: { color: '#10b981', bg: '#d1fae5' },
};

type Tab = 'dashboard' | 'campaigns' | 'volunteers' | 'rides';

export default function GOTVScreen() {
  const [tab, setTab] = useState<Tab>('dashboard');
  const [dashboard, setDashboard] = useState<DashboardData | null>(null);
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [volunteers, setVolunteers] = useState<Volunteer[]>([]);
  const [rides, setRides] = useState<RideRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [user, setUser] = useState<GOTVUser | null>(null);

  // Auth check — redirect to GOTV login if not authenticated
  useEffect(() => {
    (async () => {
      const authed = await isAuthenticated();
      if (!authed) {
        router.replace('/gotv-login');
        return;
      }
      const mobileUser = await getMobileUser();
      setUser(mobileUser);
    })();
  }, []);

  const loadAll = useCallback(async () => {
    try {
      setError(null);
      const [dash, camps, vols, rds] = await Promise.all([
        gotvFetch<DashboardData>('/gotv/dashboard'),
        gotvFetch<{ campaigns: Campaign[] }>('/gotv/campaigns'),
        gotvFetch<{ volunteers: Volunteer[] }>('/gotv/volunteers'),
        gotvFetch<{ rides: RideRequest[] }>('/gotv/rides'),
      ]);
      setDashboard(dash);
      setCampaigns(camps.campaigns || []);
      setVolunteers(vols.volunteers || []);
      setRides(rds.rides || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load data. Pull down to retry.');
    }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { loadAll(); }, [loadAll]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    if (Platform.OS !== 'web') Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await loadAll();
    setRefreshing(false);
  }, [loadAll]);

  // ─── Dashboard Tab ───────────────────────────────────────────────────

  const renderDashboard = () => {
    if (!dashboard) return <EmptyState icon="megaphone-outline" title="No GOTV data" description="Connect to your party portal" />;

    const stats = [
      { label: 'Contacts', value: dashboard.total_contacts, icon: 'people' as const, color: '#3b82f6' },
      { label: 'Volunteers', value: dashboard.total_volunteers, icon: 'hand-left' as const, color: '#22c55e' },
      { label: 'Pledges', value: dashboard.total_pledges, icon: 'checkmark-circle' as const, color: '#8b5cf6' },
      { label: 'Campaigns', value: dashboard.active_campaigns, icon: 'megaphone' as const, color: '#f59e0b' },
      { label: 'Pending Rides', value: dashboard.pending_rides, icon: 'car' as const, color: '#6366f1' },
    ];

    const quickLinks = [
      { route: '/gotv-campaigns', label: 'Campaigns', icon: 'megaphone' as const, color: '#f59e0b' },
      { route: '/gotv-contacts', label: 'Contacts', icon: 'people' as const, color: '#3b82f6' },
      { route: '/gotv-pledges', label: 'Pledges', icon: 'thumbs-up' as const, color: '#8b5cf6' },
      { route: '/gotv-rides', label: 'Rides', icon: 'car' as const, color: '#6366f1' },
      { route: '/gotv-segments', label: 'Segments', icon: 'funnel' as const, color: '#06b6d4' },
      { route: '/gotv-warroom', label: 'War Room', icon: 'radio' as const, color: '#ef4444' },
      { route: '/gotv-analytics', label: 'Analytics', icon: 'bar-chart' as const, color: '#22c55e' },
      { route: '/gotv-indicators', label: 'KOH Indicators', icon: 'speedometer' as const, color: '#006b3f' },
      { route: '/gotv-territory', label: 'Territory', icon: 'map' as const, color: '#0ea5e9' },
      { route: '/gotv-leaderboard', label: 'Leaderboard', icon: 'trophy' as const, color: '#eab308' },
    ];

    return (
      <View>
        <View style={styles.dashGrid}>
          {stats.map(s => (
            <View key={s.label} style={[styles.statCard, { borderLeftColor: s.color }]}>
              <Ionicons name={s.icon} size={24} color={s.color} />
              <Text style={styles.statValue}>{s.value.toLocaleString()}</Text>
              <Text style={styles.statLabel}>{s.label}</Text>
            </View>
          ))}
        </View>
        <Text style={{ fontSize: 14, fontWeight: '600', color: '#374151', marginTop: 16, marginBottom: 8 }}>Quick Access</Text>
        <View style={styles.dashGrid}>
          {quickLinks.map(ql => (
            <TouchableOpacity key={ql.route} style={styles.quickLink} onPress={() => router.push(ql.route as any)}>
              <Ionicons name={ql.icon} size={22} color={ql.color} />
              <Text style={styles.quickLinkText}>{ql.label}</Text>
            </TouchableOpacity>
          ))}
        </View>
      </View>
    );
  };

  // ─── Campaigns Tab ───────────────────────────────────────────────────

  const renderCampaigns = () => {
    if (campaigns.length === 0) return <EmptyState icon="megaphone-outline" title="No campaigns" description="Create your first campaign" />;
    return (
      <View>
        {campaigns.map(c => {
          const sc = STATUS_COLORS[c.status] || STATUS_COLORS.draft;
          return (
            <TouchableOpacity key={c.campaign_id} style={styles.listCard}>
              <View style={styles.listCardHeader}>
                <Text style={styles.listCardTitle}>{c.name}</Text>
                <View style={[styles.badge, { backgroundColor: sc.bg }]}>
                  <Text style={[styles.badgeText, { color: sc.color }]}>{c.status}</Text>
                </View>
              </View>
              <View style={styles.listCardMeta}>
                <Text style={styles.metaText}>Type: {c.campaign_type}</Text>
                <Text style={styles.metaText}>
                  {c.contacts_reached}/{c.total_contacts} reached
                </Text>
              </View>
            </TouchableOpacity>
          );
        })}
      </View>
    );
  };

  // ─── Volunteers Tab ──────────────────────────────────────────────────

  const renderVolunteers = () => {
    if (volunteers.length === 0) return <EmptyState icon="people-outline" title="No volunteers" description="Register volunteers for your party" />;
    return (
      <View>
        {volunteers.map(v => (
          <TouchableOpacity key={v.volunteer_id} style={styles.listCard}>
            <View style={styles.listCardHeader}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                <Ionicons name="person-circle" size={28} color="#6b7280" />
                <View>
                  <Text style={styles.listCardTitle}>{v.full_name}</Text>
                  <Text style={styles.metaText}>{v.role}</Text>
                </View>
              </View>
              {v.has_vehicle && (
                <View style={[styles.badge, { backgroundColor: '#d1fae5' }]}>
                  <Ionicons name="car" size={12} color="#059669" />
                  <Text style={[styles.badgeText, { color: '#059669', marginLeft: 4 }]}>Vehicle</Text>
                </View>
              )}
            </View>
            <View style={styles.statsRow}>
              <View style={styles.miniStat}>
                <Text style={styles.miniStatValue}>{v.doors_knocked}</Text>
                <Text style={styles.miniStatLabel}>Doors</Text>
              </View>
              <View style={styles.miniStat}>
                <Text style={styles.miniStatValue}>{v.calls_made}</Text>
                <Text style={styles.miniStatLabel}>Calls</Text>
              </View>
              <View style={styles.miniStat}>
                <Text style={styles.miniStatValue}>{v.rides_given}</Text>
                <Text style={styles.miniStatLabel}>Rides</Text>
              </View>
            </View>
          </TouchableOpacity>
        ))}
      </View>
    );
  };

  // ─── Rides Tab ───────────────────────────────────────────────────────

  const renderRides = () => {
    if (rides.length === 0) return <EmptyState icon="car-outline" title="No ride requests" description="Ride-to-polls requests appear here" />;
    return (
      <View>
        {rides.map(r => {
          const sc = STATUS_COLORS[r.status] || STATUS_COLORS.pending;
          return (
            <TouchableOpacity key={r.request_id} style={styles.listCard}>
              <View style={styles.listCardHeader}>
                <View>
                  <Text style={styles.listCardTitle}>PU: {r.polling_unit_code}</Text>
                  <Text style={styles.metaText}>Contact: {r.contact_id}</Text>
                </View>
                <View style={[styles.badge, { backgroundColor: sc.bg }]}>
                  <Text style={[styles.badgeText, { color: sc.color }]}>{r.status}</Text>
                </View>
              </View>
              {r.distance_km !== null && (
                <Text style={styles.metaText}>{r.distance_km} km away</Text>
              )}
            </TouchableOpacity>
          );
        })}
      </View>
    );
  };

  // ─── Tabs ────────────────────────────────────────────────────────────

  const tabs: { key: Tab; label: string; icon: string }[] = [
    { key: 'dashboard', label: 'Dashboard', icon: 'grid' },
    { key: 'campaigns', label: 'Campaigns', icon: 'megaphone' },
    { key: 'volunteers', label: 'Volunteers', icon: 'people' },
    { key: 'rides', label: 'Rides', icon: 'car' },
  ];

  return (
    <View style={styles.container}>
      <View style={styles.header}>
        <View style={styles.headerTop}>
          <View>
            <Text style={styles.headerTitle}>GOTV Portal</Text>
            <Text style={styles.headerSubtitle}>
              {user ? `${user.party_code} — ${user.display_name}` : 'Get Out The Vote'}
            </Text>
          </View>
          <TouchableOpacity onPress={() => router.push('/gotv-canvasser')} style={styles.canvassButton}>
            <Ionicons name="walk" size={16} color="#006b3f" />
            <Text style={styles.canvassButtonText}>Canvass</Text>
          </TouchableOpacity>
        </View>
      </View>

      <View style={styles.tabBar}>
        {tabs.map(t => (
          <TouchableOpacity
            key={t.key}
            style={[styles.tabItem, tab === t.key && styles.tabItemActive]}
            onPress={() => {
              setTab(t.key);
              if (Platform.OS !== 'web') Haptics.selectionAsync();
            }}
          >
            <Ionicons name={t.icon as any} size={18} color={tab === t.key ? '#006b3f' : '#6b7280'} />
            <Text style={[styles.tabLabel, tab === t.key && styles.tabLabelActive]}>{t.label}</Text>
          </TouchableOpacity>
        ))}
      </View>

      <ScrollView
        style={styles.content}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#006b3f" />}
      >
        {loading ? (
          <>
            <CardSkeleton />
            <CardSkeleton />
            <CardSkeleton />
          </>
        ) : error ? (
          <View style={{ padding: 24, alignItems: 'center' }}>
            <Ionicons name="cloud-offline" size={48} color="#ef4444" />
            <Text style={{ fontSize: 16, fontWeight: '600', color: '#111827', marginTop: 12 }}>Connection Error</Text>
            <Text style={{ fontSize: 14, color: '#6b7280', textAlign: 'center', marginTop: 4 }}>{error}</Text>
          </View>
        ) : (
          <>
            {tab === 'dashboard' && renderDashboard()}
            {tab === 'campaigns' && renderCampaigns()}
            {tab === 'volunteers' && renderVolunteers()}
            {tab === 'rides' && renderRides()}
          </>
        )}
        <View style={{ height: 40 }} />
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  header: { paddingTop: 60, paddingBottom: 16, paddingHorizontal: 20, backgroundColor: '#006b3f' },
  headerTop: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  headerTitle: { fontSize: 24, fontWeight: '700', color: '#fff' },
  headerSubtitle: { fontSize: 14, color: '#bbf7d0', marginTop: 2 },
  canvassButton: {
    flexDirection: 'row', alignItems: 'center', gap: 4,
    backgroundColor: '#fff', paddingHorizontal: 12, paddingVertical: 8, borderRadius: 8,
  },
  canvassButtonText: { fontSize: 13, fontWeight: '600', color: '#006b3f' },
  tabBar: { flexDirection: 'row', backgroundColor: '#fff', borderBottomWidth: 1, borderBottomColor: '#e5e7eb', paddingHorizontal: 4 },
  tabItem: { flex: 1, alignItems: 'center', paddingVertical: 10, borderBottomWidth: 2, borderBottomColor: 'transparent' },
  tabItemActive: { borderBottomColor: '#006b3f' },
  tabLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
  tabLabelActive: { color: '#006b3f', fontWeight: '600' },
  content: { flex: 1, padding: 16 },
  dashGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 12 },
  statCard: {
    width: '47%', backgroundColor: '#fff', borderRadius: 12, padding: 16,
    borderLeftWidth: 4, ...Platform.select({
      ios: { shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.08, shadowRadius: 4 },
      android: { elevation: 2 },
      default: {},
    }),
  },
  statValue: { fontSize: 24, fontWeight: '700', color: '#111827', marginTop: 8 },
  statLabel: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  listCard: {
    backgroundColor: '#fff', borderRadius: 12, padding: 16, marginBottom: 12,
    ...Platform.select({
      ios: { shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.08, shadowRadius: 4 },
      android: { elevation: 2 },
      default: {},
    }),
  },
  listCardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  listCardTitle: { fontSize: 16, fontWeight: '600', color: '#111827' },
  listCardMeta: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 8 },
  metaText: { fontSize: 13, color: '#6b7280' },
  badge: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: 8, paddingVertical: 3, borderRadius: 12 },
  badgeText: { fontSize: 11, fontWeight: '600' },
  statsRow: { flexDirection: 'row', marginTop: 12, gap: 16 },
  miniStat: { alignItems: 'center' },
  miniStatValue: { fontSize: 16, fontWeight: '700', color: '#111827' },
  miniStatLabel: { fontSize: 11, color: '#6b7280' },
  quickLink: { width: '47%', backgroundColor: '#fff', borderRadius: 10, padding: 12, flexDirection: 'row', alignItems: 'center', gap: 8, borderWidth: 1, borderColor: '#e5e7eb' },
  quickLinkText: { fontSize: 12, fontWeight: '600', color: '#374151' },
});
