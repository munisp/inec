import { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Platform, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Location from 'expo-location';
import * as Haptics from 'expo-haptics';
import { geofenceApi } from '../src/lib/api';

export default function GeofencingScreen() {
  const [puCode, setPuCode] = useState('');
  const [result, setResult] = useState<Record<string, unknown> | null>(null);
  const [spoofResult, setSpoofResult] = useState<Record<string, unknown> | null>(null);
  const [loading, setLoading] = useState(false);
  const [location, setLocation] = useState<{ lat: number; lng: number } | null>(null);

  useEffect(() => {
    (async () => {
      const { status } = await Location.requestForegroundPermissionsAsync();
      if (status === 'granted') {
        const loc = await Location.getCurrentPositionAsync({});
        setLocation({ lat: loc.coords.latitude, lng: loc.coords.longitude });
      }
    })();
  }, []);

  const handleGeofenceCheck = async () => {
    if (!location || !puCode) { Alert.alert('Error', 'Enter PU code and enable location'); return; }
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const res = await geofenceApi.check(location.lat, location.lng, puCode);
      setResult(res as unknown as Record<string, unknown>);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed');
    }
    setLoading(false);
  };

  const handleSpoofCheck = async () => {
    if (!location) { Alert.alert('Error', 'Location not available'); return; }
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const res = await geofenceApi.spoofCheck(location.lat, location.lng, `device-${Platform.OS}`);
      setSpoofResult(res as unknown as Record<string, unknown>);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed');
    }
    setLoading(false);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <Text style={styles.cardTitle}>Current Location</Text>
        {location ? (
          <View style={styles.row}>
            <View style={styles.stat}>
              <Text style={styles.statLabel}>Latitude</Text>
              <Text style={styles.statValue}>{location.lat.toFixed(6)}</Text>
            </View>
            <View style={styles.stat}>
              <Text style={styles.statLabel}>Longitude</Text>
              <Text style={styles.statValue}>{location.lng.toFixed(6)}</Text>
            </View>
          </View>
        ) : (
          <Text style={styles.muted}>Acquiring GPS location...</Text>
        )}
      </View>

      <View style={styles.card}>
        <Text style={styles.cardTitle}>Geofence Check</Text>
        <TextInput style={styles.input} placeholder="Polling Unit Code (e.g. KN/01/01/001)" value={puCode} onChangeText={setPuCode} placeholderTextColor="#9ca3af" />
        <TouchableOpacity style={styles.button} onPress={handleGeofenceCheck} disabled={loading} activeOpacity={0.8}>
          <Ionicons name="location-outline" size={18} color="#fff" />
          <Text style={styles.buttonText}>{loading ? 'Checking...' : 'Verify Location'}</Text>
        </TouchableOpacity>
        {result && (
          <View style={[styles.resultBadge, { backgroundColor: result.within_geofence ? '#dcfce7' : '#fef2f2' }]}>
            <Ionicons name={result.within_geofence ? 'checkmark-circle' : 'close-circle'} size={24} color={result.within_geofence ? '#166534' : '#dc2626'} />
            <View style={{ flex: 1, marginLeft: 8 }}>
              <Text style={{ fontWeight: '700', color: result.within_geofence ? '#166534' : '#dc2626' }}>
                {result.within_geofence ? 'Within Geofence' : 'Outside Geofence'}
              </Text>
              <Text style={styles.muted}>Distance: {String(result.distance_km || '?')} km</Text>
            </View>
          </View>
        )}
      </View>

      <View style={styles.card}>
        <Text style={styles.cardTitle}>GPS Spoof Detection</Text>
        <Text style={styles.muted}>Verify device location authenticity using multi-signal analysis</Text>
        <TouchableOpacity style={[styles.button, { backgroundColor: '#7c3aed' }]} onPress={handleSpoofCheck} disabled={loading} activeOpacity={0.8}>
          <Ionicons name="shield-checkmark-outline" size={18} color="#fff" />
          <Text style={styles.buttonText}>Run Spoof Check</Text>
        </TouchableOpacity>
        {spoofResult && (
          <View style={[styles.resultBadge, { backgroundColor: spoofResult.is_spoofed ? '#fef2f2' : '#dcfce7' }]}>
            <Ionicons name={spoofResult.is_spoofed ? 'warning' : 'shield-checkmark'} size={24} color={spoofResult.is_spoofed ? '#dc2626' : '#166534'} />
            <View style={{ flex: 1, marginLeft: 8 }}>
              <Text style={{ fontWeight: '700', color: spoofResult.is_spoofed ? '#dc2626' : '#166534' }}>
                {spoofResult.is_spoofed ? 'Spoofing Detected!' : 'Location Authentic'}
              </Text>
              <Text style={styles.muted}>Risk Score: {String(spoofResult.risk_score || 0)}</Text>
            </View>
          </View>
        )}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  card: { backgroundColor: '#fff', borderRadius: 16, padding: 16, marginBottom: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  cardTitle: { fontSize: 16, fontWeight: '700', color: '#111827', marginBottom: 12 },
  row: { flexDirection: 'row', gap: 16 },
  stat: { flex: 1 },
  statLabel: { fontSize: 12, color: '#6b7280' },
  statValue: { fontSize: 16, fontWeight: '600', color: '#111827', marginTop: 2 },
  muted: { fontSize: 13, color: '#9ca3af', marginBottom: 8 },
  input: { borderWidth: 1, borderColor: '#e5e7eb', borderRadius: 12, padding: 12, fontSize: 15, color: '#111827', marginBottom: 12 },
  button: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6, backgroundColor: '#166534', borderRadius: 12, padding: 14 },
  buttonText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  resultBadge: { flexDirection: 'row', alignItems: 'center', borderRadius: 12, padding: 14, marginTop: 12 },
});
