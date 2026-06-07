import { useState, useEffect, useCallback, useRef } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Alert, Linking, Dimensions, RefreshControl, Platform, ActivityIndicator } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Location from 'expo-location';
import * as Haptics from 'expo-haptics';
import MapView, { Marker, Circle, Polyline, Callout, PROVIDER_DEFAULT } from 'react-native-maps';
import { geoApi } from '../src/lib/api';

interface Landmark {
  id: number; name: string; category: string; latitude: number; longitude: number; address: string; icon: string;
}
interface NearbyPU {
  polling_unit_code: string; name: string; latitude: number; longitude: number; distance_m: number; ward_name: string; lga_name: string;
}
interface Official {
  staff_id: string; role: string; latitude: number; longitude: number; pu_code: string; activity: string; battery_pct: number; updated_at: string;
}
interface CrowdReport {
  pu_code: string; latitude: number; longitude: number; head_count: number; density_level: string; queue_length: number; wait_time_min: number; pu_name: string;
}
interface GeofenceZone {
  pu_code: string; center_lat: number; center_lng: number; radius_m: number;
}
interface CrowdAlert {
  id: number; pu_code: string; severity: string; message: string; created_at: string;
}

type LayerKey = 'pus' | 'landmarks' | 'officials' | 'crowd' | 'geofences' | 'incidents' | 'weather';

const CATEGORY_COLORS: Record<string, string> = {
  inec_office: '#059669', collation_center: '#dc2626', police_station: '#1d4ed8',
  hospital: '#ec4899', school: '#f59e0b', transport_hub: '#6366f1', government_building: '#7c3aed',
};
const DENSITY_COLORS: Record<string, string> = {
  overcrowded: '#dc2626', high: '#f97316', moderate: '#eab308', low: '#22c55e',
};
const ROLE_COLORS: Record<string, string> = {
  presiding_officer: '#dc2626', assistant_presiding: '#f97316', poll_clerk: '#3b82f6',
  security: '#059669', supervisor: '#7c3aed', inec_official: '#059669',
};

const NIGERIA_CENTER = { latitude: 9.0820, longitude: 7.4951, latitudeDelta: 12, longitudeDelta: 12 };

export default function GeoMapScreen() {
  const mapRef = useRef<MapView>(null);
  const [loading, setLoading] = useState(false);
  const [location, setLocation] = useState<{ lat: number; lng: number } | null>(null);
  const [nearbyPUs, setNearbyPUs] = useState<NearbyPU[]>([]);
  const [landmarks, setLandmarks] = useState<Landmark[]>([]);
  const [officials, setOfficials] = useState<Official[]>([]);
  const [crowdReports, setCrowdReports] = useState<CrowdReport[]>([]);
  const [geofenceZones, setGeofenceZones] = useState<GeofenceZone[]>([]);
  const [crowdAlerts, setCrowdAlerts] = useState<CrowdAlert[]>([]);
  const [spatialStats, setSpatialStats] = useState<{ total_pus: number; avg_turnout: number; area_km2: number; pu_density_per_km2: number } | null>(null);
  const [weatherInfo, setWeatherInfo] = useState<{ temp_c: number; humidity: number; description: string; wind_kmh: number } | null>(null);
  const [layers, setLayers] = useState<Record<LayerKey, boolean>>({
    pus: true, landmarks: false, officials: false, crowd: false, geofences: false, incidents: false, weather: false,
  });
  const [showPanel, setShowPanel] = useState(true);
  const [selectedMarker, setSelectedMarker] = useState<string | null>(null);
  const trackingTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    (async () => {
      const { status } = await Location.requestForegroundPermissionsAsync();
      if (status === 'granted') {
        const loc = await Location.getCurrentPositionAsync({});
        setLocation({ lat: loc.coords.latitude, lng: loc.coords.longitude });
      }
    })();
    loadInitialData();
  }, []);

  // SSE-like polling for tracking (every 10s when enabled)
  useEffect(() => {
    if (layers.officials) {
      loadOfficials();
      trackingTimer.current = setInterval(loadOfficials, 10000);
    } else {
      if (trackingTimer.current) clearInterval(trackingTimer.current);
    }
    return () => { if (trackingTimer.current) clearInterval(trackingTimer.current); };
  }, [layers.officials]);

  const loadInitialData = async () => {
    setLoading(true);
    try {
      const [statsData] = await Promise.all([
        geoApi.spatialStats(1).catch(() => null),
      ]);
      if (statsData) setSpatialStats(statsData);
    } catch {}
    setLoading(false);
  };

  const findNearby = async () => {
    if (!location) return;
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const data = await geoApi.nearbyPUs(location.lat, location.lng, 10000);
      setNearbyPUs(data.polling_units || []);
      if (data.polling_units?.length > 0) {
        const first = data.polling_units[0];
        mapRef.current?.animateToRegion({ latitude: first.latitude, longitude: first.longitude, latitudeDelta: 0.05, longitudeDelta: 0.05 }, 1000);
      }
    } catch { Alert.alert('Error', 'Failed to load nearby PUs'); }
    setLoading(false);
  };

  const loadLandmarks = async () => {
    try {
      const data = await geoApi.landmarks({});
      setLandmarks(data.landmarks || []);
    } catch {}
  };

  const loadOfficials = async () => {
    try {
      const data = await geoApi.getOfficials({ active_minutes: 60 });
      setOfficials(data.officials || []);
    } catch {}
  };

  const loadCrowdDensity = async () => {
    try {
      const data = await geoApi.getCrowdDensity({ recent_minutes: 120 });
      setCrowdReports(data.reports || []);
    } catch {}
  };

  const loadGeofences = async () => {
    try {
      await geoApi.seedGeofenceZones().catch(() => {});
      const data = await geoApi.getGeofenceZones();
      const zones = data?.zones?.features || data?.zones || [];
      const parsed: GeofenceZone[] = zones.map((z: any) => ({
        pu_code: z.properties?.pu_code || z.pu_code || '',
        center_lat: z.properties?.center_lat || z.center_lat,
        center_lng: z.properties?.center_lng || z.center_lng,
        radius_m: z.properties?.radius_m || z.radius_m || 500,
      })).filter((z: GeofenceZone) => z.center_lat && z.center_lng);
      setGeofenceZones(parsed);
    } catch {}
  };

  const loadCrowdAlerts = async () => {
    try {
      const data = await geoApi.getCrowdAlerts();
      setCrowdAlerts(data.alerts || []);
    } catch {}
  };

  const loadWeather = async () => {
    const lat = location?.lat || 9.0820;
    const lng = location?.lng || 7.4951;
    try {
      const data = await geoApi.getWeatherOverlay(lat, lng);
      if (data?.weather) setWeatherInfo(data.weather);
    } catch {}
  };

  const toggleLayer = (key: LayerKey) => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    const newVal = !layers[key];
    setLayers(prev => ({ ...prev, [key]: newVal }));
    if (newVal) {
      if (key === 'pus') findNearby();
      if (key === 'landmarks') loadLandmarks();
      if (key === 'officials') loadOfficials();
      if (key === 'crowd') loadCrowdDensity();
      if (key === 'geofences') loadGeofences();
      if (key === 'incidents') loadCrowdAlerts();
      if (key === 'weather') loadWeather();
    }
  };

  const openDirections = (lat: number, lng: number) => {
    const url = Platform.select({
      ios: `maps:0,0?daddr=${lat},${lng}`,
      android: `geo:0,0?q=${lat},${lng}`,
      default: `https://www.google.com/maps/dir/?api=1&destination=${lat},${lng}`,
    });
    if (url) Linking.openURL(url);
  };

  const openStreetView = (lat: number, lng: number) => {
    Linking.openURL(`https://www.google.com/maps/@?api=1&map_action=pano&viewpoint=${lat},${lng}`);
  };

  return (
    <View style={styles.container}>
      {/* Map */}
      <MapView
        ref={mapRef}
        style={styles.map}
        provider={PROVIDER_DEFAULT}
        initialRegion={NIGERIA_CENTER}
        showsUserLocation
        showsMyLocationButton
        showsCompass
        showsScale
        mapType="standard"
      >
        {/* Nearby PU markers */}
        {layers.pus && nearbyPUs.map(pu => (
          <Marker
            key={pu.polling_unit_code}
            coordinate={{ latitude: pu.latitude, longitude: pu.longitude }}
            pinColor="#16a34a"
            title={pu.name}
            description={`${pu.ward_name}, ${pu.lga_name} • ${(pu.distance_m / 1000).toFixed(1)}km`}
          />
        ))}

        {/* Landmark markers */}
        {layers.landmarks && landmarks.map(lm => (
          <Marker
            key={`lm-${lm.id}`}
            coordinate={{ latitude: lm.latitude, longitude: lm.longitude }}
            pinColor={CATEGORY_COLORS[lm.category] || '#6b7280'}
            title={lm.name}
            description={`${lm.category.replace(/_/g, ' ')} • ${lm.address}`}
          />
        ))}

        {/* Official tracking markers */}
        {layers.officials && officials.map(off => (
          <Marker
            key={`off-${off.staff_id}`}
            coordinate={{ latitude: off.latitude, longitude: off.longitude }}
            pinColor={ROLE_COLORS[off.role] || '#3b82f6'}
            title={`${off.role.replace(/_/g, ' ')} — ${off.staff_id.slice(0, 8)}`}
            description={`${off.activity} • Battery: ${off.battery_pct}% • PU: ${off.pu_code}`}
          >
            <Callout>
              <View style={{ padding: 4, maxWidth: 200 }}>
                <Text style={{ fontWeight: 'bold', fontSize: 12 }}>{off.role.replace(/_/g, ' ')}</Text>
                <Text style={{ fontSize: 11 }}>Activity: {off.activity}</Text>
                <Text style={{ fontSize: 11 }}>Battery: {off.battery_pct}%</Text>
                <Text style={{ fontSize: 11 }}>PU: {off.pu_code}</Text>
              </View>
            </Callout>
          </Marker>
        ))}

        {/* Crowd density markers */}
        {layers.crowd && crowdReports.map((cr, i) => (
          <Marker
            key={`cr-${cr.pu_code}-${i}`}
            coordinate={{ latitude: cr.latitude, longitude: cr.longitude }}
            pinColor={DENSITY_COLORS[cr.density_level] || '#6b7280'}
            title={cr.pu_name || cr.pu_code}
            description={`${cr.head_count} people • ${cr.density_level} • Wait: ${cr.wait_time_min}min`}
          />
        ))}

        {/* Geofence circles */}
        {layers.geofences && geofenceZones.map((gz, i) => (
          <Circle
            key={`gf-${gz.pu_code}-${i}`}
            center={{ latitude: gz.center_lat, longitude: gz.center_lng }}
            radius={gz.radius_m}
            strokeColor="rgba(59, 130, 246, 0.6)"
            fillColor="rgba(59, 130, 246, 0.1)"
            strokeWidth={2}
          />
        ))}
      </MapView>

      {/* Floating layer panel toggle */}
      <TouchableOpacity style={styles.panelToggle} onPress={() => setShowPanel(!showPanel)}>
        <Ionicons name={showPanel ? 'chevron-down' : 'layers'} size={20} color="#fff" />
      </TouchableOpacity>

      {/* Weather badge */}
      {layers.weather && weatherInfo && (
        <View style={styles.weatherBadge}>
          <Text style={styles.weatherText}>{weatherInfo.temp_c}°C {weatherInfo.description}</Text>
          <Text style={styles.weatherSubtext}>💧 {weatherInfo.humidity}% 💨 {weatherInfo.wind_kmh}km/h</Text>
        </View>
      )}

      {/* Crowd alerts banner */}
      {layers.incidents && crowdAlerts.length > 0 && (
        <View style={styles.alertBanner}>
          <Ionicons name="warning" size={14} color="#f59e0b" />
          <Text style={styles.alertText} numberOfLines={1}>
            {crowdAlerts[0].severity.toUpperCase()}: {crowdAlerts[0].message || crowdAlerts[0].pu_code}
          </Text>
        </View>
      )}

      {/* Stats bar */}
      {spatialStats && (
        <View style={styles.statsBar}>
          <Text style={styles.statItem}>{spatialStats.total_pus.toLocaleString()} PUs</Text>
          <Text style={styles.statItem}>{(spatialStats.area_km2 || 0).toLocaleString()} km²</Text>
          <Text style={styles.statItem}>Turnout: {((spatialStats.avg_turnout || 0) * 100).toFixed(0)}%</Text>
        </View>
      )}

      {/* Loading indicator */}
      {loading && (
        <View style={styles.loadingOverlay}>
          <ActivityIndicator size="small" color="#16a34a" />
        </View>
      )}

      {/* Layer control panel */}
      {showPanel && (
        <View style={styles.layerPanel}>
          <Text style={styles.panelTitle}>Map Layers</Text>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.layerChips}>
            {([
              { key: 'pus' as LayerKey, label: 'Polling Units', icon: 'location' as const },
              { key: 'landmarks' as LayerKey, label: 'Landmarks', icon: 'business' as const },
              { key: 'officials' as LayerKey, label: 'Officials', icon: 'radio' as const },
              { key: 'crowd' as LayerKey, label: 'Crowd', icon: 'people' as const },
              { key: 'geofences' as LayerKey, label: 'Geofences', icon: 'shield-checkmark' as const },
              { key: 'incidents' as LayerKey, label: 'Alerts', icon: 'warning' as const },
              { key: 'weather' as LayerKey, label: 'Weather', icon: 'cloud' as const },
            ]).map(({ key, label, icon }) => (
              <TouchableOpacity
                key={key}
                style={[styles.chip, layers[key] && styles.chipActive]}
                onPress={() => toggleLayer(key)}
              >
                <Ionicons name={icon} size={14} color={layers[key] ? '#fff' : '#374151'} />
                <Text style={[styles.chipText, layers[key] && styles.chipTextActive]}>{label}</Text>
                {key === 'officials' && layers[key] && officials.length > 0 && (
                  <View style={styles.chipBadge}><Text style={styles.chipBadgeText}>{officials.length}</Text></View>
                )}
              </TouchableOpacity>
            ))}
          </ScrollView>

          {/* Quick actions */}
          <View style={styles.quickActions}>
            <TouchableOpacity style={styles.actionBtn} onPress={findNearby}>
              <Ionicons name="navigate" size={16} color="#16a34a" />
              <Text style={styles.actionText}>Find Nearby</Text>
            </TouchableOpacity>
            <TouchableOpacity style={styles.actionBtn} onPress={() => {
              if (location) openStreetView(location.lat, location.lng);
              else if (mapRef.current) {
                // Use Nigeria center as fallback
                openStreetView(9.0820, 7.4951);
              }
            }}>
              <Ionicons name="eye" size={16} color="#3b82f6" />
              <Text style={styles.actionText}>Street View</Text>
            </TouchableOpacity>
            <TouchableOpacity style={styles.actionBtn} onPress={() => {
              mapRef.current?.animateToRegion(NIGERIA_CENTER, 1000);
            }}>
              <Ionicons name="globe" size={16} color="#7c3aed" />
              <Text style={styles.actionText}>Nigeria</Text>
            </TouchableOpacity>
            {location && (
              <TouchableOpacity style={styles.actionBtn} onPress={() => openDirections(location.lat, location.lng)}>
                <Ionicons name="navigate-circle" size={16} color="#f59e0b" />
                <Text style={styles.actionText}>Directions</Text>
              </TouchableOpacity>
            )}
          </View>

          {/* Nearby PU list */}
          {layers.pus && nearbyPUs.length > 0 && (
            <View style={styles.listSection}>
              <Text style={styles.listTitle}>Nearby Polling Units ({nearbyPUs.length})</Text>
              <ScrollView style={{ maxHeight: 120 }}>
                {nearbyPUs.slice(0, 5).map(pu => (
                  <TouchableOpacity key={pu.polling_unit_code} style={styles.listItem}
                    onPress={() => {
                      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                      mapRef.current?.animateToRegion({ latitude: pu.latitude, longitude: pu.longitude, latitudeDelta: 0.01, longitudeDelta: 0.01 }, 500);
                    }}>
                    <Ionicons name="location" size={14} color="#16a34a" />
                    <View style={{ flex: 1, marginLeft: 6 }}>
                      <Text style={styles.listItemTitle} numberOfLines={1}>{pu.name}</Text>
                      <Text style={styles.listItemSub}>{pu.ward_name} • {(pu.distance_m / 1000).toFixed(1)}km</Text>
                    </View>
                    <TouchableOpacity onPress={() => openDirections(pu.latitude, pu.longitude)}>
                      <Ionicons name="navigate" size={16} color="#3b82f6" />
                    </TouchableOpacity>
                  </TouchableOpacity>
                ))}
              </ScrollView>
            </View>
          )}
        </View>
      )}
    </View>
  );
}

const { width: W, height: H } = Dimensions.get('window');

const styles = StyleSheet.create({
  container: { flex: 1 },
  map: { flex: 1 },
  panelToggle: {
    position: 'absolute', top: 50, right: 12, width: 40, height: 40, borderRadius: 20,
    backgroundColor: '#16a34a', justifyContent: 'center', alignItems: 'center',
    shadowColor: '#000', shadowOffset: { width: 0, height: 2 }, shadowOpacity: 0.25, shadowRadius: 4, elevation: 5,
  },
  weatherBadge: {
    position: 'absolute', top: 50, left: 12, backgroundColor: 'rgba(255,255,255,0.95)',
    borderRadius: 8, padding: 8, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.15, shadowRadius: 2, elevation: 3,
  },
  weatherText: { fontSize: 13, fontWeight: '600', color: '#1f2937' },
  weatherSubtext: { fontSize: 11, color: '#6b7280' },
  alertBanner: {
    position: 'absolute', top: 100, left: 12, right: 12, flexDirection: 'row', alignItems: 'center', gap: 6,
    backgroundColor: 'rgba(254, 243, 199, 0.95)', borderRadius: 8, padding: 8,
    borderWidth: 1, borderColor: '#fbbf24',
  },
  alertText: { fontSize: 12, color: '#92400e', flex: 1 },
  statsBar: {
    position: 'absolute', bottom: 0, left: 0, right: 0, flexDirection: 'row', justifyContent: 'space-around',
    backgroundColor: 'rgba(0,0,0,0.75)', paddingVertical: 6, paddingHorizontal: 12,
  },
  statItem: { fontSize: 11, color: '#fff', fontWeight: '500' },
  loadingOverlay: {
    position: 'absolute', top: 50, alignSelf: 'center', backgroundColor: 'rgba(255,255,255,0.9)',
    borderRadius: 20, padding: 8,
  },
  layerPanel: {
    position: 'absolute', bottom: 30, left: 8, right: 8, backgroundColor: 'rgba(255,255,255,0.97)',
    borderRadius: 12, padding: 12, shadowColor: '#000', shadowOffset: { width: 0, height: -2 }, shadowOpacity: 0.15, shadowRadius: 8, elevation: 8,
    maxHeight: H * 0.45,
  },
  panelTitle: { fontSize: 14, fontWeight: '700', color: '#111827', marginBottom: 8 },
  layerChips: { flexDirection: 'row', gap: 6, paddingBottom: 8 },
  chip: {
    flexDirection: 'row', alignItems: 'center', gap: 4, paddingHorizontal: 10, paddingVertical: 6,
    borderRadius: 16, backgroundColor: '#f3f4f6', borderWidth: 1, borderColor: '#e5e7eb',
  },
  chipActive: { backgroundColor: '#16a34a', borderColor: '#16a34a' },
  chipText: { fontSize: 12, color: '#374151' },
  chipTextActive: { color: '#fff' },
  chipBadge: { backgroundColor: '#fff', borderRadius: 8, paddingHorizontal: 4, marginLeft: 2 },
  chipBadgeText: { fontSize: 10, fontWeight: '700', color: '#16a34a' },
  quickActions: { flexDirection: 'row', gap: 8, marginBottom: 8 },
  actionBtn: { flexDirection: 'row', alignItems: 'center', gap: 4, paddingHorizontal: 8, paddingVertical: 4, borderRadius: 6, backgroundColor: '#f9fafb', borderWidth: 1, borderColor: '#e5e7eb' },
  actionText: { fontSize: 11, color: '#374151' },
  listSection: { marginTop: 4 },
  listTitle: { fontSize: 12, fontWeight: '600', color: '#374151', marginBottom: 4 },
  listItem: { flexDirection: 'row', alignItems: 'center', paddingVertical: 6, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: '#e5e7eb' },
  listItemTitle: { fontSize: 12, fontWeight: '500', color: '#111827' },
  listItemSub: { fontSize: 10, color: '#6b7280' },
});
