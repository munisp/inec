import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, Dimensions } from 'react-native';
import MapView, { Marker, PROVIDER_GOOGLE } from 'react-native-maps';
import { apiGet } from '../lib/api';

interface PollingUnit {
  code: string;
  name: string;
  latitude: number;
  longitude: number;
  state_code: string;
}

export default function MapScreen() {
  const [units, setUnits] = useState<PollingUnit[]>([]);

  useEffect(() => {
    (async () => {
      try {
        const data = await apiGet('/polling-units?page=1&limit=100');
        setUnits((data.polling_units || []).filter((u: PollingUnit) => u.latitude && u.longitude));
      } catch {}
    })();
  }, []);

  return (
    <View style={s.container}>
      <MapView
        style={s.map}
        initialRegion={{ latitude: 9.0820, longitude: 8.6753, latitudeDelta: 8, longitudeDelta: 8 }}
      >
        {units.map((u) => (
          <Marker
            key={u.code}
            coordinate={{ latitude: u.latitude, longitude: u.longitude }}
            title={u.name}
            description={u.code}
            pinColor="#15803d"
          />
        ))}
      </MapView>
      <View style={s.legend}>
        <Text style={s.legendText}>{units.length} polling units loaded</Text>
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  container: { flex: 1 },
  map: { width: Dimensions.get('window').width, height: Dimensions.get('window').height - 180 },
  legend: { position: 'absolute', bottom: 16, left: 16, right: 16, backgroundColor: '#fff', borderRadius: 12, padding: 12, elevation: 4, alignItems: 'center' },
  legendText: { fontSize: 13, color: '#475569' },
});
