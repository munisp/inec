import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { getStoredUser, User } from '../lib/auth';

export default function ProfileScreen() {
  const [user, setUser] = useState<User | null>(null);

  useEffect(() => { getStoredUser().then(setUser); }, []);

  if (!user) return <View style={s.container}><Text style={s.empty}>Not logged in</Text></View>;

  const fields = [
    { icon: 'person' as const, label: 'Name', value: user.name },
    { icon: 'mail' as const, label: 'Email', value: user.email },
    { icon: 'shield' as const, label: 'Role', value: user.role },
    { icon: 'flag' as const, label: 'Party', value: user.partyCode },
    { icon: 'key' as const, label: 'User ID', value: user.id },
  ];

  return (
    <ScrollView style={s.container}>
      <View style={s.avatarSection}>
        <View style={s.avatar}>
          <Text style={s.avatarText}>{user.name[0]?.toUpperCase()}</Text>
        </View>
        <Text style={s.name}>{user.name}</Text>
        <Text style={s.email}>{user.email}</Text>
      </View>

      <View style={s.fields}>
        {fields.map((f) => (
          <View key={f.label} style={s.field}>
            <Ionicons name={f.icon} size={18} color="#64748b" />
            <View style={s.fieldInfo}>
              <Text style={s.fieldLabel}>{f.label}</Text>
              <Text style={s.fieldValue}>{f.value}</Text>
            </View>
          </View>
        ))}
      </View>

      {user.permissions.length > 0 && (
        <View style={s.permissions}>
          <Text style={s.permTitle}>Permissions</Text>
          <View style={s.permGrid}>
            {user.permissions.map((p) => (
              <View key={p} style={s.permBadge}>
                <Text style={s.permText}>{p}</Text>
              </View>
            ))}
          </View>
        </View>
      )}
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  avatarSection: { alignItems: 'center', padding: 24 },
  avatar: { width: 80, height: 80, borderRadius: 40, backgroundColor: '#15803d', justifyContent: 'center', alignItems: 'center' },
  avatarText: { fontSize: 32, fontWeight: '700', color: '#fff' },
  name: { fontSize: 20, fontWeight: '700', color: '#1e293b', marginTop: 12 },
  email: { fontSize: 14, color: '#64748b', marginTop: 4 },
  fields: { margin: 16, backgroundColor: '#fff', borderRadius: 14, padding: 4, elevation: 1 },
  field: { flexDirection: 'row', alignItems: 'center', gap: 12, padding: 14, borderBottomWidth: 1, borderBottomColor: '#f1f5f9' },
  fieldInfo: { flex: 1 },
  fieldLabel: { fontSize: 11, color: '#94a3b8' },
  fieldValue: { fontSize: 15, color: '#1e293b', marginTop: 2 },
  permissions: { margin: 16 },
  permTitle: { fontSize: 14, fontWeight: '600', color: '#1e293b', marginBottom: 8 },
  permGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 6 },
  permBadge: { backgroundColor: '#f0fdf4', paddingHorizontal: 10, paddingVertical: 4, borderRadius: 8 },
  permText: { fontSize: 12, color: '#15803d', fontWeight: '500' },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
