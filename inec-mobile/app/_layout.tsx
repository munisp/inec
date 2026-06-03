import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useEffect } from 'react';
import { View } from 'react-native';
import { getDb } from '../src/lib/offline';
import { NetworkBanner } from '../src/components/NetworkBanner';

export default function RootLayout() {
  useEffect(() => {
    getDb();
  }, []);

  return (
    <View style={{ flex: 1 }}>
      <StatusBar style="light" />
      <NetworkBanner />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: '#166534' },
          headerTintColor: '#fff',
          headerTitleStyle: { fontWeight: 'bold' },
          animation: 'slide_from_right',
        }}
      >
        <Stack.Screen name="index" options={{ headerShown: false }} />
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen name="kyc" options={{ title: 'KYC Verification' }} />
        <Stack.Screen name="disputes" options={{ title: 'Disputes' }} />
        <Stack.Screen name="elections" options={{ title: 'Elections' }} />
        <Stack.Screen name="results" options={{ title: 'Results & Collation' }} />
        <Stack.Screen name="document-ai" options={{ title: 'Document AI' }} />
        <Stack.Screen name="scale-health" options={{ title: 'System Health' }} />
        <Stack.Screen name="geofencing" options={{ title: 'Geofencing' }} />
        <Stack.Screen name="voter-search" options={{ title: 'Voter Search' }} />
        <Stack.Screen name="middleware" options={{ title: 'Middleware' }} />
        <Stack.Screen name="biometrics" options={{ title: 'Biometrics' }} />
      </Stack>
    </View>
  );
}
