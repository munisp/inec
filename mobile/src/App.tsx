/**
 * INEC Election Platform — React Native Mobile App
 *
 * Full feature parity with web UI:
 * - INEC: Dashboard, Elections, Results, Collation, Map, BVAS, Audit, Incidents
 * - GOTV: Portal, Campaigns, Contacts, Volunteers, War Room, Tasks, Scoring
 * - Party Primaries: Convention Dashboard, Aspirants, Delegates, Voting, Remote Voting
 * - Infrastructure: Admin, Settings, Notifications, Profile
 *
 * Capabilities: Biometric auth, push notifications, camera/barcode, offline-first, maps
 */
import React, { useEffect, useState } from 'react';
import { StatusBar, ActivityIndicator, View } from 'react-native';
import { NavigationContainer } from '@react-navigation/native';
import { createBottomTabNavigator } from '@react-navigation/bottom-tabs';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { Ionicons } from '@expo/vector-icons';

import { getStoredUser, User } from './lib/auth';
import { registerForPushNotifications, addNotificationResponseListener } from './lib/notifications';

// Screens
import LoginScreen from './screens/LoginScreen';
import DashboardScreen from './screens/DashboardScreen';
import ElectionsScreen from './screens/ElectionsScreen';
import ResultsScreen from './screens/ResultsScreen';
import MapScreen from './screens/MapScreen';
import BVASScreen from './screens/BVASScreen';
import IncidentsScreen from './screens/IncidentsScreen';
import AuditScreen from './screens/AuditScreen';
import CollationScreen from './screens/CollationScreen';
import GOTVPortalScreen from './screens/GOTVPortalScreen';
import CampaignsScreen from './screens/CampaignsScreen';
import ContactsScreen from './screens/ContactsScreen';
import VolunteersScreen from './screens/VolunteersScreen';
import WarRoomScreen from './screens/WarRoomScreen';
import TasksScreen from './screens/gotv-tasks';
import ScoringScreen from './screens/ScoringScreen';
import PrimariesDashboardScreen from './screens/PrimariesDashboardScreen';
import AspirantsScreen from './screens/AspirantsScreen';
import DelegatesScreen from './screens/DelegatesScreen';
import VotingScreen from './screens/VotingScreen';
import RemoteVotingScreen from './screens/RemoteVotingScreen';
import SettingsScreen from './screens/SettingsScreen';
import NotificationsScreen from './screens/NotificationsScreen';
import ProfileScreen from './screens/ProfileScreen';

const Tab = createBottomTabNavigator();
const Stack = createNativeStackNavigator();

// INEC Stack
function INECStack() {
  return (
    <Stack.Navigator screenOptions={{ headerStyle: { backgroundColor: '#15803d' }, headerTintColor: '#fff' }}>
      <Stack.Screen name="Dashboard" component={DashboardScreen} />
      <Stack.Screen name="Elections" component={ElectionsScreen} />
      <Stack.Screen name="Results" component={ResultsScreen} />
      <Stack.Screen name="Collation" component={CollationScreen} />
      <Stack.Screen name="Map" component={MapScreen} />
      <Stack.Screen name="BVAS" component={BVASScreen} />
      <Stack.Screen name="Incidents" component={IncidentsScreen} />
      <Stack.Screen name="Audit" component={AuditScreen} />
    </Stack.Navigator>
  );
}

// GOTV Stack
function GOTVStack() {
  return (
    <Stack.Navigator screenOptions={{ headerStyle: { backgroundColor: '#2563eb' }, headerTintColor: '#fff' }}>
      <Stack.Screen name="GOTVPortal" component={GOTVPortalScreen} options={{ title: 'GOTV Portal' }} />
      <Stack.Screen name="Campaigns" component={CampaignsScreen} />
      <Stack.Screen name="Contacts" component={ContactsScreen} />
      <Stack.Screen name="Volunteers" component={VolunteersScreen} />
      <Stack.Screen name="WarRoom" component={WarRoomScreen} options={{ title: 'War Room' }} />
      <Stack.Screen name="Tasks" component={TasksScreen} options={{ title: 'Field Tasks' }} />
      <Stack.Screen name="Scoring" component={ScoringScreen} />
    </Stack.Navigator>
  );
}

// Primaries Stack
function PrimariesStack() {
  return (
    <Stack.Navigator screenOptions={{ headerStyle: { backgroundColor: '#7c3aed' }, headerTintColor: '#fff' }}>
      <Stack.Screen name="PrimariesDashboard" component={PrimariesDashboardScreen} options={{ title: 'Convention' }} />
      <Stack.Screen name="Aspirants" component={AspirantsScreen} />
      <Stack.Screen name="Delegates" component={DelegatesScreen} />
      <Stack.Screen name="Voting" component={VotingScreen} options={{ title: 'Voting Rounds' }} />
      <Stack.Screen name="RemoteVoting" component={RemoteVotingScreen} options={{ title: 'Remote Voting' }} />
    </Stack.Navigator>
  );
}

// Settings Stack
function SettingsStack() {
  return (
    <Stack.Navigator screenOptions={{ headerStyle: { backgroundColor: '#1e293b' }, headerTintColor: '#fff' }}>
      <Stack.Screen name="SettingsHome" component={SettingsScreen} options={{ title: 'Settings' }} />
      <Stack.Screen name="Profile" component={ProfileScreen} />
      <Stack.Screen name="Notifications" component={NotificationsScreen} />
    </Stack.Navigator>
  );
}

function MainTabs() {
  return (
    <Tab.Navigator
      screenOptions={({ route }) => ({
        headerShown: false,
        tabBarActiveTintColor: '#15803d',
        tabBarInactiveTintColor: '#94a3b8',
        tabBarStyle: { paddingBottom: 4, height: 60 },
        tabBarIcon: ({ focused, color, size }) => {
          let iconName: keyof typeof Ionicons.glyphMap = 'home';
          switch (route.name) {
            case 'INEC': iconName = focused ? 'shield-checkmark' : 'shield-checkmark-outline'; break;
            case 'GOTV': iconName = focused ? 'megaphone' : 'megaphone-outline'; break;
            case 'Primaries': iconName = focused ? 'people' : 'people-outline'; break;
            case 'More': iconName = focused ? 'settings' : 'settings-outline'; break;
          }
          return <Ionicons name={iconName} size={size} color={color} />;
        },
      })}
    >
      <Tab.Screen name="INEC" component={INECStack} />
      <Tab.Screen name="GOTV" component={GOTVStack} />
      <Tab.Screen name="Primaries" component={PrimariesStack} />
      <Tab.Screen name="More" component={SettingsStack} />
    </Tab.Navigator>
  );
}

export default function App() {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    (async () => {
      const storedUser = await getStoredUser();
      setUser(storedUser);
      setLoading(false);

      // Register push notifications
      await registerForPushNotifications();
    })();

    // Handle notification taps
    const subscription = addNotificationResponseListener((response) => {
      const data = response.notification.request.content.data;
      // Navigate to relevant screen based on notification data
      console.log('Notification tapped:', data);
    });

    return () => subscription.remove();
  }, []);

  if (loading) {
    return (
      <View style={{ flex: 1, justifyContent: 'center', alignItems: 'center', backgroundColor: '#15803d' }}>
        <ActivityIndicator size="large" color="#fff" />
      </View>
    );
  }

  return (
    <SafeAreaProvider>
      <StatusBar barStyle="light-content" backgroundColor="#15803d" />
      <NavigationContainer>
        {user ? <MainTabs /> : <LoginScreen onLogin={setUser} />}
      </NavigationContainer>
    </SafeAreaProvider>
  );
}
