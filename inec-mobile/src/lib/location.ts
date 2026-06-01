import * as Location from 'expo-location';
import { Alert } from 'react-native';

export async function requestLocationPermission(): Promise<boolean> {
  const { status: foreground } = await Location.requestForegroundPermissionsAsync();
  if (foreground !== 'granted') {
    Alert.alert(
      'Location Required',
      'INEC Observer needs location access to verify you are at the correct polling unit.'
    );
    return false;
  }
  return true;
}

export async function requestBackgroundLocationPermission(): Promise<boolean> {
  const foreground = await requestLocationPermission();
  if (!foreground) return false;

  const { status } = await Location.requestBackgroundPermissionsAsync();
  if (status !== 'granted') {
    Alert.alert(
      'Background Location',
      'Enable background location for continuous geofence validation while at your polling unit.'
    );
    return false;
  }
  return true;
}

export async function getCurrentLocation(): Promise<{ latitude: number; longitude: number } | null> {
  const hasPermission = await requestLocationPermission();
  if (!hasPermission) return null;

  const location = await Location.getCurrentPositionAsync({
    accuracy: Location.Accuracy.High,
  });

  return {
    latitude: location.coords.latitude,
    longitude: location.coords.longitude,
  };
}

/**
 * Calculate Haversine distance between two GPS coordinates (meters)
 */
export function haversineDistance(
  lat1: number, lon1: number,
  lat2: number, lon2: number
): number {
  const R = 6371000; // Earth radius in meters
  const toRad = (deg: number) => (deg * Math.PI) / 180;

  const dLat = toRad(lat2 - lat1);
  const dLon = toRad(lon2 - lon1);
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2;
  const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
  return R * c;
}

const GEOFENCE_RADIUS_M = 500;

export function isWithinGeofence(
  observerLat: number, observerLon: number,
  puLat: number, puLon: number,
  radiusM: number = GEOFENCE_RADIUS_M
): { within: boolean; distance: number } {
  const distance = haversineDistance(observerLat, observerLon, puLat, puLon);
  return { within: distance <= radiusM, distance };
}
