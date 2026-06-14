import React, { useState, useEffect } from 'react';
import { View, Text, FlatList, StyleSheet, Switch, TouchableOpacity } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import AsyncStorage from '@react-native-async-storage/async-storage';

interface NotificationItem {
  id: string;
  title: string;
  body: string;
  type: string;
  timestamp: string;
  read: boolean;
}

interface NotificationPrefs {
  electionAlerts: boolean;
  gotvUpdates: boolean;
  primariesAlerts: boolean;
  incidentAlerts: boolean;
  systemUpdates: boolean;
}

export default function NotificationsScreen() {
  const [notifications, setNotifications] = useState<NotificationItem[]>([]);
  const [prefs, setPrefs] = useState<NotificationPrefs>({
    electionAlerts: true, gotvUpdates: true, primariesAlerts: true,
    incidentAlerts: true, systemUpdates: false,
  });
  const [showPrefs, setShowPrefs] = useState(false);

  useEffect(() => {
    (async () => {
      const stored = await AsyncStorage.getItem('notification_history');
      if (stored) setNotifications(JSON.parse(stored));
      const storedPrefs = await AsyncStorage.getItem('notification_prefs');
      if (storedPrefs) setPrefs(JSON.parse(storedPrefs));
    })();
  }, []);

  const updatePref = async (key: keyof NotificationPrefs, value: boolean) => {
    const newPrefs = { ...prefs, [key]: value };
    setPrefs(newPrefs);
    await AsyncStorage.setItem('notification_prefs', JSON.stringify(newPrefs));
  };

  const prefItems = [
    { key: 'electionAlerts' as const, label: 'Election Alerts', desc: 'Results, collation, BVAS updates' },
    { key: 'gotvUpdates' as const, label: 'GOTV Updates', desc: 'Campaign progress, task assignments' },
    { key: 'primariesAlerts' as const, label: 'Primaries Alerts', desc: 'Voting rounds, delegate accreditation' },
    { key: 'incidentAlerts' as const, label: 'Incident Alerts', desc: 'Field incidents, security alerts' },
    { key: 'systemUpdates' as const, label: 'System Updates', desc: 'Platform maintenance, new features' },
  ];

  return (
    <View style={s.container}>
      <TouchableOpacity style={s.prefToggle} onPress={() => setShowPrefs(!showPrefs)}>
        <Ionicons name="settings-outline" size={18} color="#64748b" />
        <Text style={s.prefToggleText}>Notification Preferences</Text>
        <Ionicons name={showPrefs ? 'chevron-up' : 'chevron-down'} size={16} color="#94a3b8" />
      </TouchableOpacity>

      {showPrefs && (
        <View style={s.prefsSection}>
          {prefItems.map((item) => (
            <View key={item.key} style={s.prefRow}>
              <View style={s.prefInfo}>
                <Text style={s.prefLabel}>{item.label}</Text>
                <Text style={s.prefDesc}>{item.desc}</Text>
              </View>
              <Switch value={prefs[item.key]} onValueChange={(v) => updatePref(item.key, v)} trackColor={{ true: '#15803d' }} />
            </View>
          ))}
        </View>
      )}

      <FlatList
        data={notifications}
        keyExtractor={(item) => item.id}
        renderItem={({ item }) => (
          <View style={[s.card, !item.read && s.unread]}>
            <Ionicons
              name={item.type === 'election' ? 'shield' : item.type === 'incident' ? 'alert-circle' : 'notifications'}
              size={20}
              color={item.type === 'incident' ? '#dc2626' : '#2563eb'}
            />
            <View style={s.cardInfo}>
              <Text style={s.cardTitle}>{item.title}</Text>
              <Text style={s.cardBody}>{item.body}</Text>
              <Text style={s.cardTime}>{new Date(item.timestamp).toLocaleString()}</Text>
            </View>
          </View>
        )}
        contentContainerStyle={s.list}
        ListEmptyComponent={
          <View style={s.emptyContainer}>
            <Ionicons name="notifications-off" size={48} color="#e2e8f0" />
            <Text style={s.empty}>No notifications yet</Text>
          </View>
        }
      />
    </View>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  prefToggle: { flexDirection: 'row', alignItems: 'center', gap: 8, padding: 16, backgroundColor: '#fff', borderBottomWidth: 1, borderBottomColor: '#f1f5f9' },
  prefToggleText: { flex: 1, fontSize: 14, color: '#475569', fontWeight: '500' },
  prefsSection: { backgroundColor: '#fff', paddingHorizontal: 16, paddingBottom: 12 },
  prefRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f8fafc' },
  prefInfo: { flex: 1 },
  prefLabel: { fontSize: 14, fontWeight: '500', color: '#1e293b' },
  prefDesc: { fontSize: 11, color: '#94a3b8', marginTop: 2 },
  list: { padding: 16, gap: 8 },
  card: { flexDirection: 'row', backgroundColor: '#fff', borderRadius: 12, padding: 12, gap: 12, elevation: 1 },
  unread: { borderLeftWidth: 3, borderLeftColor: '#2563eb' },
  cardInfo: { flex: 1 },
  cardTitle: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  cardBody: { fontSize: 13, color: '#64748b', marginTop: 2 },
  cardTime: { fontSize: 11, color: '#94a3b8', marginTop: 4 },
  emptyContainer: { alignItems: 'center', marginTop: 64, gap: 12 },
  empty: { color: '#94a3b8', fontSize: 14 },
});
