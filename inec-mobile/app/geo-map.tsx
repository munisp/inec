import { useState, useEffect, useCallback } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Alert, Linking, Dimensions, RefreshControl, Platform } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Location from 'expo-location';
import * as Haptics from 'expo-haptics';
import { geoApi } from '../src/lib/api';

interface Landmark {
  id: number;
  name: string;
  category: string;
  latitude: number;
  longitude: number;
  address: string;
  icon: string;
}

interface NearbyPU {
  polling_unit_code: string;
  name: string;
  latitude: number;
  longitude: number;
  distance_m: number;
  ward_name: string;
  lga_name: string;
}

type Tab = 'nearby' | 'landmarks' | 'stats' | 'street_view';

const CATEGORY_ICONS: Record<string, string> = {
  inec_office: 'business',
  collation_center: 'flag',
  police_station: 'shield-checkmark',
  hospital: 'medkit',
  school: 'school',
  transport_hub: 'airplane',
  government_building: 'business',
  church: 'home',
  mosque: 'home',
  market: 'cart',
  bank: 'cash',
  post_office: 'mail',
};

const CATEGORY_COLORS: Record<string, string> = {
  inec_office: '#059669',
  collation_center: '#dc2626',
  police_station: '#1d4ed8',
  hospital: '#ec4899',
  school: '#f59e0b',
  transport_hub: '#6366f1',
  government_building: '#7c3aed',
};

export default function GeoMapScreen() {
  const [tab, setTab] = useState<Tab>('nearby');
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [location, setLocation] = useState<{ lat: number; lng: number } | null>(null);
  const [nearbyPUs, setNearbyPUs] = useState<NearbyPU[]>([]);
  const [landmarks, setLandmarks] = useState<Landmark[]>([]);
  const [spatialStats, setSpatialStats] = useState<{ total_pus: number; avg_turnout: number; area_km2: number; pu_density_per_km2: number } | null>(null);
  const [searchRadius, setSearchRadius] = useState('5000');
  const [selectedCategory, setSelectedCategory] = useState<string>('');

  useEffect(() => {
    (async () => {
      const { status } = await Location.requestForegroundPermissionsAsync();
      if (status === 'granted') {
        const loc = await Location.getCurrentPositionAsync({});
        setLocation({ lat: loc.coords.latitude, lng: loc.coords.longitude });
      }
    })();
    loadStats();
  }, []);

  useEffect(() => {
    if (location && tab === 'nearby') findNearby();
    if (location && tab === 'landmarks') loadLandmarks();
  }, [location, tab]);

  const findNearby = async () => {
    if (!location) return;
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const data = await geoApi.nearbyPUs(location.lat, location.lng, Number(searchRadius));
      setNearbyPUs(data.polling_units || []);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to find nearby PUs');
    }
    setLoading(false);
  };

  const loadLandmarks = async () => {
    setLoading(true);
    try {
      const params: { lat?: number; lng?: number; radius?: number; category?: string } = {};
      if (location) { params.lat = location.lat; params.lng = location.lng; params.radius = 50000; }
      if (selectedCategory) params.category = selectedCategory;
      const data = await geoApi.landmarks(params);
      setLandmarks(data.landmarks || []);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed');
    }
    setLoading(false);
  };

  const loadStats = async () => {
    try {
      const data = await geoApi.spatialStats(1);
      setSpatialStats(data);
    } catch {}
  };

  const openStreetView = async (lat: number, lng: number) => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const data = await geoApi.streetView(lat, lng);
      const url = data.street_view?.mapillary?.viewer_url || data.street_view?.google?.viewer_url;
      if (url) {
        await Linking.openURL(url);
      } else {
        Alert.alert('Info', 'No street view available for this location');
      }
    } catch {
      // Fallback to Google Maps
      const url = `https://www.google.com/maps/@${lat},${lng},3a,75y,0h,90t/data=!3m4!1e1!3m2!1s!2e0`;
      await Linking.openURL(url);
    }
  };

  const openInMaps = (lat: number, lng: number, name: string) => {
    const url = Platform.OS === 'ios'
      ? `maps://app?daddr=${lat},${lng}&q=${encodeURIComponent(name)}`
      : `geo:${lat},${lng}?q=${lat},${lng}(${encodeURIComponent(name)})`;
    Linking.openURL(url);
  };

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    if (tab === 'nearby') await findNearby();
    else if (tab === 'landmarks') await loadLandmarks();
    else await loadStats();
    setRefreshing(false);
  }, [tab, location, searchRadius, selectedCategory]);

  const formatDistance = (m: number) => m < 1000 ? `${Math.round(m)}m` : `${(m / 1000).toFixed(1)}km`;

  const tabs: { key: Tab; label: string; icon: string }[] = [
    { key: 'nearby', label: 'Nearby PUs', icon: 'location' },
    { key: 'landmarks', label: 'Landmarks', icon: 'business' },
    { key: 'stats', label: 'Stats', icon: 'analytics' },
    { key: 'street_view', label: 'Street View', icon: 'eye' },
  ];

  return (
    <ScrollView style={s.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}>
      <Text style={s.title}>Geospatial Map</Text>
      <Text style={s.subtitle}>
        {location ? `${location.lat.toFixed(4)}, ${location.lng.toFixed(4)}` : 'Acquiring location...'}
      </Text>

      {/* Tabs */}
      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={s.tabBar}>
        {tabs.map(t => (
          <TouchableOpacity key={t.key} style={[s.tab, tab === t.key && s.tabActive]}
            onPress={() => { setTab(t.key); Haptics.selectionAsync(); }}>
            <Ionicons name={t.icon as keyof typeof Ionicons.glyphMap} size={14} color={tab === t.key ? '#fff' : '#666'} />
            <Text style={[s.tabText, tab === t.key && s.tabTextActive]}>{t.label}</Text>
          </TouchableOpacity>
        ))}
      </ScrollView>

      {/* Summary Stats Cards */}
      {spatialStats && (
        <View style={s.statsRow}>
          <View style={s.statCard}>
            <Text style={s.statValue}>{spatialStats.total_pus?.toLocaleString() || 0}</Text>
            <Text style={s.statLabel}>Total PUs</Text>
          </View>
          <View style={s.statCard}>
            <Text style={s.statValue}>{((spatialStats.avg_turnout || 0) * 100).toFixed(1)}%</Text>
            <Text style={s.statLabel}>Avg Turnout</Text>
          </View>
          <View style={s.statCard}>
            <Text style={s.statValue}>{Math.round(spatialStats.area_km2 || 0).toLocaleString()}</Text>
            <Text style={s.statLabel}>Area km²</Text>
          </View>
          <View style={s.statCard}>
            <Text style={s.statValue}>{(spatialStats.pu_density_per_km2 || 0).toFixed(1)}</Text>
            <Text style={s.statLabel}>PU/km²</Text>
          </View>
        </View>
      )}

      {/* Tab Content */}
      {tab === 'nearby' && (
        <View style={s.section}>
          <View style={s.searchRow}>
            <TextInput style={s.input} value={searchRadius} onChangeText={setSearchRadius}
              placeholder="Radius (m)" keyboardType="numeric" />
            <TouchableOpacity style={s.searchBtn} onPress={findNearby}>
              <Ionicons name="search" size={16} color="#fff" />
              <Text style={s.searchBtnText}>Search</Text>
            </TouchableOpacity>
          </View>

          {loading && <Text style={s.loadingText}>Searching...</Text>}

          {nearbyPUs.map(pu => (
            <View key={pu.polling_unit_code} style={s.card}>
              <View style={s.cardHeader}>
                <View style={{ flex: 1 }}>
                  <Text style={s.cardTitle}>{pu.name}</Text>
                  <Text style={s.cardSub}>{pu.ward_name} · {pu.lga_name}</Text>
                </View>
                <View style={s.badge}>
                  <Text style={s.badgeText}>{formatDistance(pu.distance_m)}</Text>
                </View>
              </View>
              <View style={s.cardActions}>
                <TouchableOpacity style={s.actionBtn} onPress={() => openInMaps(pu.latitude, pu.longitude, pu.name)}>
                  <Ionicons name="navigate" size={14} color="#2563eb" />
                  <Text style={s.actionText}>Directions</Text>
                </TouchableOpacity>
                <TouchableOpacity style={s.actionBtn} onPress={() => openStreetView(pu.latitude, pu.longitude)}>
                  <Ionicons name="eye" size={14} color="#059669" />
                  <Text style={[s.actionText, { color: '#059669' }]}>Street View</Text>
                </TouchableOpacity>
              </View>
            </View>
          ))}
          {!loading && nearbyPUs.length === 0 && <Text style={s.emptyText}>No polling units found nearby</Text>}
        </View>
      )}

      {tab === 'landmarks' && (
        <View style={s.section}>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ marginBottom: 8 }}>
            <TouchableOpacity style={[s.filterChip, !selectedCategory && s.filterActive]}
              onPress={() => { setSelectedCategory(''); loadLandmarks(); }}>
              <Text style={[s.filterText, !selectedCategory && s.filterTextActive]}>All</Text>
            </TouchableOpacity>
            {['inec_office', 'collation_center', 'police_station', 'hospital', 'school', 'government_building'].map(cat => (
              <TouchableOpacity key={cat} style={[s.filterChip, selectedCategory === cat && s.filterActive]}
                onPress={() => { setSelectedCategory(cat); }}>
                <Text style={[s.filterText, selectedCategory === cat && s.filterTextActive]}>
                  {cat.replace(/_/g, ' ')}
                </Text>
              </TouchableOpacity>
            ))}
          </ScrollView>

          {loading && <Text style={s.loadingText}>Loading landmarks...</Text>}

          {landmarks.map(lm => (
            <View key={lm.id} style={s.card}>
              <View style={s.cardHeader}>
                <View style={[s.iconCircle, { backgroundColor: CATEGORY_COLORS[lm.category] || '#6b7280' }]}>
                  <Ionicons name={(CATEGORY_ICONS[lm.category] || 'location') as keyof typeof Ionicons.glyphMap} size={14} color="#fff" />
                </View>
                <View style={{ flex: 1, marginLeft: 10 }}>
                  <Text style={s.cardTitle}>{lm.name}</Text>
                  <Text style={s.cardSub}>{lm.category.replace(/_/g, ' ')}</Text>
                  {lm.address ? <Text style={s.cardAddress}>{lm.address}</Text> : null}
                </View>
              </View>
              <View style={s.cardActions}>
                <TouchableOpacity style={s.actionBtn} onPress={() => openInMaps(lm.latitude, lm.longitude, lm.name)}>
                  <Ionicons name="navigate" size={14} color="#2563eb" />
                  <Text style={s.actionText}>Directions</Text>
                </TouchableOpacity>
                <TouchableOpacity style={s.actionBtn} onPress={() => openStreetView(lm.latitude, lm.longitude)}>
                  <Ionicons name="eye" size={14} color="#059669" />
                  <Text style={[s.actionText, { color: '#059669' }]}>Street View</Text>
                </TouchableOpacity>
              </View>
            </View>
          ))}
          {!loading && landmarks.length === 0 && <Text style={s.emptyText}>No landmarks found</Text>}
        </View>
      )}

      {tab === 'stats' && (
        <View style={s.section}>
          <View style={s.card}>
            <Text style={s.sectionTitle}>Spatial Coverage</Text>
            {spatialStats ? (
              <>
                <View style={s.statRow}><Text style={s.statRowLabel}>Total Polling Units</Text><Text style={s.statRowValue}>{spatialStats.total_pus.toLocaleString()}</Text></View>
                <View style={s.statRow}><Text style={s.statRowLabel}>Average Turnout</Text><Text style={s.statRowValue}>{(spatialStats.avg_turnout * 100).toFixed(1)}%</Text></View>
                <View style={s.statRow}><Text style={s.statRowLabel}>Coverage Area</Text><Text style={s.statRowValue}>{Math.round(spatialStats.area_km2).toLocaleString()} km²</Text></View>
                <View style={s.statRow}><Text style={s.statRowLabel}>PU Density</Text><Text style={s.statRowValue}>{spatialStats.pu_density_per_km2.toFixed(2)} per km²</Text></View>
              </>
            ) : (
              <Text style={s.loadingText}>Loading spatial stats...</Text>
            )}
          </View>

          <View style={s.card}>
            <Text style={s.sectionTitle}>PostGIS Integration</Text>
            <View style={s.statRow}><Text style={s.statRowLabel}>Spatial Indexing</Text><Text style={[s.statRowValue, { color: '#059669' }]}>GIST Enabled</Text></View>
            <View style={s.statRow}><Text style={s.statRowLabel}>Coordinate System</Text><Text style={s.statRowValue}>SRID 4326 (WGS84)</Text></View>
            <View style={s.statRow}><Text style={s.statRowLabel}>Proximity Search</Text><Text style={s.statRowValue}>ST_DWithin</Text></View>
            <View style={s.statRow}><Text style={s.statRowLabel}>Boundary Analysis</Text><Text style={s.statRowValue}>Convex Hull</Text></View>
          </View>

          <View style={s.card}>
            <Text style={s.sectionTitle}>Apache Sedona Analytics</Text>
            <View style={s.statRow}><Text style={s.statRowLabel}>Hotspot Detection</Text><Text style={s.statRowValue}>Grid-based Clustering</Text></View>
            <View style={s.statRow}><Text style={s.statRowLabel}>Coverage Gap</Text><Text style={s.statRowValue}>Density Analysis</Text></View>
            <View style={s.statRow}><Text style={s.statRowLabel}>Autocorrelation</Text><Text style={s.statRowValue}>Moran's I</Text></View>
            <View style={s.statRow}><Text style={s.statRowLabel}>Lakehouse Layer</Text><Text style={s.statRowValue}>Gold Tier Parquet</Text></View>
          </View>
        </View>
      )}

      {tab === 'street_view' && (
        <View style={s.section}>
          <View style={s.card}>
            <Text style={s.sectionTitle}>Street View</Text>
            <Text style={s.cardSub}>Select a polling unit or landmark to view street-level imagery via Mapillary (open-source) or Google Maps.</Text>

            {location && (
              <TouchableOpacity style={s.svBtn}
                onPress={() => openStreetView(location.lat, location.lng)}>
                <Ionicons name="eye" size={20} color="#fff" />
                <Text style={s.svBtnText}>View Current Location</Text>
              </TouchableOpacity>
            )}

            <Text style={[s.sectionTitle, { marginTop: 16 }]}>Quick Access</Text>
            {[
              { name: 'INEC National HQ, Abuja', lat: 9.0579, lng: 7.4951 },
              { name: 'National Assembly, Abuja', lat: 9.0642, lng: 7.5063 },
              { name: 'Tafawa Balewa Square, Lagos', lat: 6.4328, lng: 3.4218 },
            ].map(loc => (
              <TouchableOpacity key={loc.name} style={s.svItem}
                onPress={() => openStreetView(loc.lat, loc.lng)}>
                <Ionicons name="location" size={16} color="#2563eb" />
                <Text style={s.svItemText}>{loc.name}</Text>
                <Ionicons name="open-outline" size={14} color="#9ca3af" />
              </TouchableOpacity>
            ))}
          </View>
        </View>
      )}

      <View style={{ height: 40 }} />
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f5f5f5', padding: 16 },
  title: { fontSize: 20, fontWeight: '700', color: '#111' },
  subtitle: { fontSize: 12, color: '#666', marginBottom: 12 },
  tabBar: { marginBottom: 12 },
  tab: { flexDirection: 'row', alignItems: 'center', gap: 4, paddingHorizontal: 12, paddingVertical: 6, borderRadius: 20, backgroundColor: '#e5e7eb', marginRight: 8 },
  tabActive: { backgroundColor: '#1d4ed8' },
  tabText: { fontSize: 12, color: '#666' },
  tabTextActive: { color: '#fff', fontWeight: '600' },
  statsRow: { flexDirection: 'row', gap: 8, marginBottom: 12 },
  statCard: { flex: 1, backgroundColor: '#fff', borderRadius: 8, padding: 8, alignItems: 'center' },
  statValue: { fontSize: 14, fontWeight: '700', color: '#111' },
  statLabel: { fontSize: 9, color: '#666', marginTop: 2 },
  section: {},
  sectionTitle: { fontSize: 14, fontWeight: '600', color: '#111', marginBottom: 8 },
  searchRow: { flexDirection: 'row', gap: 8, marginBottom: 12 },
  input: { flex: 1, backgroundColor: '#fff', borderRadius: 8, paddingHorizontal: 12, paddingVertical: 8, fontSize: 14, borderWidth: 1, borderColor: '#e5e7eb' },
  searchBtn: { flexDirection: 'row', alignItems: 'center', gap: 4, backgroundColor: '#1d4ed8', borderRadius: 8, paddingHorizontal: 16, paddingVertical: 8 },
  searchBtnText: { color: '#fff', fontSize: 13, fontWeight: '600' },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 12, marginBottom: 10, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 2, elevation: 1 },
  cardHeader: { flexDirection: 'row', alignItems: 'center' },
  cardTitle: { fontSize: 13, fontWeight: '600', color: '#111' },
  cardSub: { fontSize: 11, color: '#666', marginTop: 2 },
  cardAddress: { fontSize: 10, color: '#9ca3af', marginTop: 1 },
  cardActions: { flexDirection: 'row', gap: 12, marginTop: 8, paddingTop: 8, borderTopWidth: 1, borderTopColor: '#f3f4f6' },
  actionBtn: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  actionText: { fontSize: 12, color: '#2563eb', fontWeight: '500' },
  badge: { backgroundColor: '#eff6ff', borderRadius: 12, paddingHorizontal: 8, paddingVertical: 2 },
  badgeText: { fontSize: 11, color: '#1d4ed8', fontWeight: '600' },
  iconCircle: { width: 28, height: 28, borderRadius: 14, alignItems: 'center', justifyContent: 'center' },
  filterChip: { paddingHorizontal: 12, paddingVertical: 4, borderRadius: 16, backgroundColor: '#e5e7eb', marginRight: 6 },
  filterActive: { backgroundColor: '#1d4ed8' },
  filterText: { fontSize: 11, color: '#374151', textTransform: 'capitalize' },
  filterTextActive: { color: '#fff' },
  loadingText: { textAlign: 'center', color: '#9ca3af', fontSize: 13, paddingVertical: 20 },
  emptyText: { textAlign: 'center', color: '#9ca3af', fontSize: 13, paddingVertical: 40 },
  statRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 6, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
  statRowLabel: { fontSize: 12, color: '#666' },
  statRowValue: { fontSize: 12, fontWeight: '600', color: '#111' },
  svBtn: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, backgroundColor: '#059669', borderRadius: 8, paddingVertical: 12, marginTop: 12 },
  svBtnText: { color: '#fff', fontSize: 14, fontWeight: '600' },
  svItem: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
  svItemText: { flex: 1, fontSize: 13, color: '#374151' },
});
