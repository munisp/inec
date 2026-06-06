import { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Platform, ActivityIndicator, TextInput } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface User {
  id: number;
  username: string;
  full_name: string;
  role: string;
  staff_id: string;
  state_code: string;
  kyc_status: string;
  created_at: string;
}

export default function UserManagementScreen() {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState('');

  const loadUsers = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const data = await api<{ users: User[] }>('/users');
      setUsers(data.users || []);
    } catch { /* ignore */ }
    setLoading(false);
  };

  useEffect(() => { loadUsers(); }, []);

  const filtered = users.filter(u =>
    u.full_name?.toLowerCase().includes(search.toLowerCase()) ||
    u.username?.toLowerCase().includes(search.toLowerCase()) ||
    u.role?.toLowerCase().includes(search.toLowerCase())
  );

  const roleColor = (r: string) => {
    switch (r) {
      case 'admin': return '#dc2626';
      case 'presiding_officer': return '#166534';
      case 'observer': return '#3b82f6';
      default: return '#64748b';
    }
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      <View style={styles.header}>
        <Ionicons name="people" size={28} color="#166534" />
        <Text style={styles.title}>Users</Text>
      </View>

      <View style={styles.searchBox}>
        <Ionicons name="search" size={18} color="#94a3b8" />
        <TextInput style={styles.searchInput} placeholder="Search users..." value={search} onChangeText={setSearch} />
      </View>

      {loading && <ActivityIndicator size="large" color="#166534" style={{ marginTop: 24 }} />}

      <Text style={styles.countText}>{filtered.length} users</Text>

      {filtered.map(u => (
        <View key={u.id} style={styles.userCard}>
          <View style={styles.avatar}>
            <Text style={styles.avatarText}>{(u.full_name || u.username || '?').charAt(0).toUpperCase()}</Text>
          </View>
          <View style={{ flex: 1 }}>
            <Text style={styles.userName}>{u.full_name || u.username}</Text>
            <Text style={styles.userMeta}>{u.staff_id} — {u.state_code || 'N/A'}</Text>
          </View>
          <View style={[styles.roleBadge, { backgroundColor: roleColor(u.role) + '15' }]}>
            <Text style={[styles.roleText, { color: roleColor(u.role) }]}>{u.role}</Text>
          </View>
        </View>
      ))}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 16, paddingTop: Platform.OS === 'ios' ? 60 : 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b' },
  searchBox: { flexDirection: 'row', alignItems: 'center', marginHorizontal: 16, padding: 10, backgroundColor: '#fff', borderRadius: 10, borderWidth: 1, borderColor: '#e2e8f0', gap: 8 },
  searchInput: { flex: 1, fontSize: 14 },
  countText: { fontSize: 13, color: '#64748b', marginHorizontal: 16, marginVertical: 8 },
  userCard: { flexDirection: 'row', alignItems: 'center', marginHorizontal: 16, marginBottom: 8, padding: 14, backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#e2e8f0' },
  avatar: { width: 40, height: 40, borderRadius: 20, backgroundColor: '#166534', alignItems: 'center', justifyContent: 'center', marginRight: 12 },
  avatarText: { color: '#fff', fontWeight: '700', fontSize: 16 },
  userName: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  userMeta: { fontSize: 12, color: '#64748b', marginTop: 2 },
  roleBadge: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 6 },
  roleText: { fontSize: 11, fontWeight: '600', textTransform: 'capitalize' },
});
