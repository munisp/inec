import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity, Platform, Alert,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { router } from 'expo-router';
import { clearToken } from '../../src/lib/api';

interface MenuItem {
  id: string;
  title: string;
  subtitle: string;
  icon: keyof typeof Ionicons.glyphMap;
  color: string;
  bg: string;
  route: string;
}

const MENU_SECTIONS: { title: string; items: MenuItem[] }[] = [
  {
    title: 'Election',
    items: [
      { id: 'elections', title: 'Elections', subtitle: 'View active elections', icon: 'podium-outline', color: '#166534', bg: '#dcfce7', route: '/elections' },
      { id: 'results', title: 'Results & Collation', subtitle: 'Live results by state and LGA', icon: 'bar-chart-outline', color: '#2563eb', bg: '#dbeafe', route: '/results' },
    ],
  },
  {
    title: 'Verification',
    items: [
      { id: 'kyc', title: 'KYC Verification', subtitle: 'Identity & liveness check', icon: 'person-circle-outline', color: '#7c3aed', bg: '#ede9fe', route: '/kyc' },
      { id: 'docai', title: 'Document AI', subtitle: 'Analyze EC8A result sheets', icon: 'scan-outline', color: '#0891b2', bg: '#cffafe', route: '/document-ai' },
    ],
  },
  {
    title: 'Monitoring',
    items: [
      { id: 'disputes', title: 'Disputes', subtitle: 'File & track election disputes', icon: 'shield-outline', color: '#dc2626', bg: '#fef2f2', route: '/disputes' },
      { id: 'scale', title: 'System Health', subtitle: 'Platform scale & performance', icon: 'pulse-outline', color: '#059669', bg: '#d1fae5', route: '/scale-health' },
    ],
  },
];

export default function MoreScreen() {
  const handleLogout = useCallback(() => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    Alert.alert('Logout', 'Are you sure you want to logout?', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Logout',
        style: 'destructive',
        onPress: async () => {
          await clearToken();
          router.replace('/');
        },
      },
    ]);
  }, []);

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      {MENU_SECTIONS.map((section) => (
        <View key={section.title} style={styles.section}>
          <Text style={styles.sectionTitle}>{section.title}</Text>
          <View style={styles.card}>
            {section.items.map((item, index) => (
              <TouchableOpacity
                key={item.id}
                style={[styles.menuItem, index < section.items.length - 1 && styles.menuItemBorder]}
                onPress={() => {
                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  router.push(item.route as never);
                }}
                activeOpacity={0.7}
              >
                <View style={[styles.iconCircle, { backgroundColor: item.bg }]}>
                  <Ionicons name={item.icon} size={20} color={item.color} />
                </View>
                <View style={styles.menuText}>
                  <Text style={styles.menuTitle}>{item.title}</Text>
                  <Text style={styles.menuSubtitle}>{item.subtitle}</Text>
                </View>
                <Ionicons name="chevron-forward" size={16} color="#d1d5db" />
              </TouchableOpacity>
            ))}
          </View>
        </View>
      ))}

      <View style={styles.section}>
        <TouchableOpacity style={styles.logoutButton} onPress={handleLogout} activeOpacity={0.8}>
          <Ionicons name="log-out-outline" size={20} color="#dc2626" />
          <Text style={styles.logoutText}>Logout</Text>
        </TouchableOpacity>
      </View>

      <Text style={styles.version}>INEC Observer v1.0.0</Text>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  section: { marginTop: 20, paddingHorizontal: 16 },
  sectionTitle: { fontSize: 13, fontWeight: '700', color: '#6b7280', textTransform: 'uppercase', letterSpacing: 0.5, marginBottom: 8, marginLeft: 4 },
  card: { backgroundColor: '#fff', borderRadius: 16, overflow: 'hidden', shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  menuItem: { flexDirection: 'row', alignItems: 'center', padding: 16, gap: 12 },
  menuItemBorder: { borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
  iconCircle: { width: 40, height: 40, borderRadius: 12, justifyContent: 'center', alignItems: 'center' },
  menuText: { flex: 1 },
  menuTitle: { fontSize: 15, fontWeight: '600', color: '#111827' },
  menuSubtitle: { fontSize: 12, color: '#9ca3af', marginTop: 2 },
  logoutButton: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, backgroundColor: '#fff', borderRadius: 16, padding: 16, borderWidth: 1, borderColor: '#fecaca' },
  logoutText: { fontSize: 15, fontWeight: '600', color: '#dc2626' },
  version: { textAlign: 'center', fontSize: 12, color: '#d1d5db', marginTop: 24, marginBottom: 40 },
});
