// GOTV Territory View — Shows canvasser's assigned territory boundaries, contacts, and navigation.
// Uses react-native-maps for territory visualization and offline map support.
// Auth: standalone GOTV mobile JWT.

import { useState, useEffect, useCallback } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert, Dimensions } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import MapView, { Marker, Polygon, PROVIDER_GOOGLE } from 'react-native-maps';
import * as Location from 'expo-location';
import { getMobileUser, gotvFetch, type GOTVUser } from '../lib/gotv-auth';
import { router } from 'expo-router';

interface Territory {
  territory_id: string;
  volunteer_id: string;
  volunteer_name: string;
  ward_code: string;
  contact_count: number;
  status: string;
}

interface TerritoryContact {
  contact_id: string;
  full_name: string;
  latitude: number;
  longitude: number;
  voter_status: string;
}

const STATUS_COLORS: Record<string, string> = {
  unknown: '#9ca3af',
  pledged: '#3b82f6',
  confirmed: '#10b981',
  declined: '#ef4444',
  unreachable: '#f59e0b',
};

export default function GOTVTerritoryScreen() {
  const [user, setUser] = useState<GOTVUser | null>(null);
  const [territory, setTerritory] = useState<Territory | null>(null);
  const [contacts, setContacts] = useState<TerritoryContact[]>([]);
  const [location, setLocation] = useState<Location.LocationObject | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    (async () => {
      const mobileUser = await getMobileUser();
      if (!mobileUser) {
        router.replace('/gotv-login');
        return;
      }
      setUser(mobileUser);
      loadTerritory(mobileUser);

      const { status } = await Location.requestForegroundPermissionsAsync();
      if (status === 'granted') {
        const loc = await Location.getCurrentPositionAsync({});
        setLocation(loc);
      }
    })();
  }, []);

  const loadTerritory = async (u: GOTVUser) => {
    try {
      const data = await gotvFetch<{ territory: Territory; contacts: TerritoryContact[] }>('/gotv/mobile/territory');
      if (data && data.territory) {
        setTerritory(data.territory);
        setContacts(data.contacts || []);
      }
    } catch {
      // Territory not assigned yet
    } finally {
      setLoading(false);
    }
  };

  if (loading) {
    return (
      <View style={styles.center}>
        <Text style={styles.loadingText}>Loading territory...</Text>
      </View>
    );
  }

  if (!territory) {
    return (
      <View style={styles.center}>
        <Ionicons name="map-outline" size={64} color="#9ca3af" />
        <Text style={styles.emptyTitle}>No Territory Assigned</Text>
        <Text style={styles.emptySubtitle}>Your team lead will assign you a territory</Text>
        <TouchableOpacity style={styles.backBtn} onPress={() => router.back()}>
          <Text style={styles.backBtnText}>Back to Canvasser</Text>
        </TouchableOpacity>
      </View>
    );
  }

  const region = contacts.length > 0
    ? {
        latitude: contacts.reduce((s, c) => s + c.latitude, 0) / contacts.length,
        longitude: contacts.reduce((s, c) => s + c.longitude, 0) / contacts.length,
        latitudeDelta: 0.02,
        longitudeDelta: 0.02,
      }
    : location
      ? { latitude: location.coords.latitude, longitude: location.coords.longitude, latitudeDelta: 0.05, longitudeDelta: 0.05 }
      : { latitude: 9.0582, longitude: 7.4906, latitudeDelta: 5.0, longitudeDelta: 5.0 };

  return (
    <View style={styles.container}>
      <View style={styles.header}>
        <TouchableOpacity onPress={() => router.back()}>
          <Ionicons name="arrow-back" size={24} color="#1f2937" />
        </TouchableOpacity>
        <View style={styles.headerCenter}>
          <Text style={styles.headerTitle}>My Territory</Text>
          <Text style={styles.headerSubtitle}>{territory.ward_code} — {territory.contact_count} contacts</Text>
        </View>
        <View style={[styles.statusBadge, { backgroundColor: territory.status === 'assigned' ? '#dbeafe' : '#dcfce7' }]}>
          <Text style={{ fontSize: 12 }}>{territory.status}</Text>
        </View>
      </View>

      <MapView
        style={styles.map}
        initialRegion={region}
        showsUserLocation
        showsMyLocationButton
      >
        {contacts.map(c => (
          <Marker
            key={c.contact_id}
            coordinate={{ latitude: c.latitude, longitude: c.longitude }}
            title={c.full_name}
            description={`Status: ${c.voter_status}`}
            pinColor={STATUS_COLORS[c.voter_status] || '#9ca3af'}
          />
        ))}
      </MapView>

      <View style={styles.stats}>
        <View style={styles.statItem}>
          <Text style={styles.statValue}>{contacts.filter(c => c.voter_status === 'pledged').length}</Text>
          <Text style={styles.statLabel}>Pledged</Text>
        </View>
        <View style={styles.statItem}>
          <Text style={styles.statValue}>{contacts.filter(c => c.voter_status === 'unknown').length}</Text>
          <Text style={styles.statLabel}>Unvisited</Text>
        </View>
        <View style={styles.statItem}>
          <Text style={styles.statValue}>{contacts.length}</Text>
          <Text style={styles.statLabel}>Total</Text>
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center', padding: 24 },
  loadingText: { color: '#9ca3af', marginTop: 8 },
  emptyTitle: { fontSize: 18, fontWeight: '600', color: '#374151', marginTop: 16 },
  emptySubtitle: { color: '#9ca3af', marginTop: 4 },
  backBtn: { backgroundColor: '#3b82f6', paddingHorizontal: 24, paddingVertical: 12, borderRadius: 8, marginTop: 24 },
  backBtnText: { color: '#fff', fontWeight: '600' },
  header: { flexDirection: 'row', alignItems: 'center', padding: 16, paddingTop: 56, backgroundColor: '#fff', borderBottomWidth: 1, borderBottomColor: '#e5e7eb' },
  headerCenter: { flex: 1, marginLeft: 12 },
  headerTitle: { fontSize: 18, fontWeight: '700', color: '#1f2937' },
  headerSubtitle: { fontSize: 12, color: '#6b7280' },
  statusBadge: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 12 },
  map: { flex: 1 },
  stats: { flexDirection: 'row', justifyContent: 'space-around', padding: 16, backgroundColor: '#f9fafb', borderTopWidth: 1, borderTopColor: '#e5e7eb' },
  statItem: { alignItems: 'center' },
  statValue: { fontSize: 20, fontWeight: '700', color: '#1f2937' },
  statLabel: { fontSize: 12, color: '#6b7280', marginTop: 2 },
});
