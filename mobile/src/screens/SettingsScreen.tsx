import React, { useState, useEffect } from 'react';
import { View, Text, ScrollView, StyleSheet, TouchableOpacity, Switch, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { logout, isBiometricAvailable, enableBiometric, isBiometricEnabled } from '../lib/auth';
import { syncOfflineQueue } from '../lib/api';
import AsyncStorage from '@react-native-async-storage/async-storage';

export default function SettingsScreen({ navigation }: any) {
  const [biometricEnabled, setBiometricEnabled] = useState(false);
  const [biometricAvailable, setBiometricAvailable] = useState(false);
  const [offlineCount, setOfflineCount] = useState(0);
  const [darkMode, setDarkMode] = useState(false);
  const [notifications, setNotifications] = useState(true);

  useEffect(() => {
    (async () => {
      setBiometricAvailable(await isBiometricAvailable());
      setBiometricEnabled(await isBiometricEnabled());
      const queue = JSON.parse(await AsyncStorage.getItem('offline_queue') || '[]');
      setOfflineCount(queue.length);
    })();
  }, []);

  const toggleBiometric = async (value: boolean) => {
    if (value) await enableBiometric();
    setBiometricEnabled(value);
  };

  const handleSync = async () => {
    const synced = await syncOfflineQueue();
    Alert.alert('Sync Complete', `${synced} queued requests synced`);
    setOfflineCount(Math.max(0, offlineCount - synced));
  };

  const handleLogout = () => {
    Alert.alert('Logout', 'Are you sure?', [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Logout', style: 'destructive', onPress: async () => { await logout(); /* App will re-render to login */ } },
    ]);
  };

  const sections = [
    {
      title: 'Account',
      items: [
        { icon: 'person' as const, title: 'Profile', action: () => navigation.navigate('Profile') },
        { icon: 'notifications' as const, title: 'Notifications', action: () => navigation.navigate('Notifications'), badge: null },
      ],
    },
    {
      title: 'Security',
      items: [
        { icon: 'finger-print' as const, title: 'Biometric Login', toggle: biometricEnabled, onToggle: toggleBiometric, disabled: !biometricAvailable },
      ],
    },
    {
      title: 'Data & Sync',
      items: [
        { icon: 'cloud-upload' as const, title: 'Sync Offline Data', action: handleSync, badge: offlineCount > 0 ? offlineCount : null },
        { icon: 'trash' as const, title: 'Clear Cache', action: async () => { await AsyncStorage.clear(); Alert.alert('Cache cleared'); } },
      ],
    },
    {
      title: 'Display',
      items: [
        { icon: 'moon' as const, title: 'Dark Mode', toggle: darkMode, onToggle: setDarkMode },
        { icon: 'notifications-outline' as const, title: 'Push Notifications', toggle: notifications, onToggle: setNotifications },
      ],
    },
  ];

  return (
    <ScrollView style={s.container}>
      {sections.map((section) => (
        <View key={section.title} style={s.section}>
          <Text style={s.sectionTitle}>{section.title}</Text>
          {section.items.map((item) => (
            <TouchableOpacity
              key={item.title}
              style={s.row}
              onPress={'action' in item ? item.action : undefined}
              disabled={'toggle' in item}
            >
              <Ionicons name={item.icon} size={20} color="#475569" />
              <Text style={s.rowText}>{item.title}</Text>
              {'badge' in item && item.badge != null && (
                <View style={s.badge}><Text style={s.badgeText}>{item.badge}</Text></View>
              )}
              {'toggle' in item ? (
                <Switch
                  value={item.toggle}
                  onValueChange={item.onToggle}
                  trackColor={{ true: '#15803d' }}
                  disabled={'disabled' in item ? item.disabled : false}
                />
              ) : (
                <Ionicons name="chevron-forward" size={16} color="#94a3b8" />
              )}
            </TouchableOpacity>
          ))}
        </View>
      ))}

      <TouchableOpacity style={s.logoutBtn} onPress={handleLogout}>
        <Ionicons name="log-out" size={20} color="#dc2626" />
        <Text style={s.logoutText}>Sign Out</Text>
      </TouchableOpacity>

      <Text style={s.version}>INEC Platform v1.0.0</Text>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  section: { marginTop: 24, marginHorizontal: 16 },
  sectionTitle: { fontSize: 12, fontWeight: '600', color: '#94a3b8', textTransform: 'uppercase', marginBottom: 8 },
  row: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#fff', padding: 14, borderRadius: 12, marginBottom: 6, gap: 12, elevation: 1 },
  rowText: { flex: 1, fontSize: 15, color: '#1e293b' },
  badge: { backgroundColor: '#dc2626', borderRadius: 10, paddingHorizontal: 7, paddingVertical: 1 },
  badgeText: { color: '#fff', fontSize: 11, fontWeight: '700' },
  logoutBtn: { flexDirection: 'row', alignItems: 'center', gap: 8, justifyContent: 'center', margin: 24, padding: 14, borderRadius: 12, borderWidth: 1, borderColor: '#fecaca' },
  logoutText: { fontSize: 15, fontWeight: '600', color: '#dc2626' },
  version: { textAlign: 'center', fontSize: 12, color: '#94a3b8', marginBottom: 32 },
});
