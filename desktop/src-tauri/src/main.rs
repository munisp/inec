//! INEC Field Kit — Tauri Desktop Application
//!
//! GeoLibre-powered desktop app for election day field operations.
//! Provides offline GIS, GPS tracking, result submission, and spatial analysis.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use serde::{Deserialize, Serialize};
use std::sync::Mutex;

/// Cached polling unit data for offline access
struct OfflineCache {
    polling_units: Vec<PollingUnit>,
    last_sync: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PollingUnit {
    code: String,
    name: String,
    latitude: f64,
    longitude: f64,
    state_code: String,
    lga_name: String,
    ward_name: String,
    registered_voters: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct GpsPosition {
    latitude: f64,
    longitude: f64,
    accuracy: f64,
    timestamp: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct SyncQueueItem {
    id: String,
    item_type: String, // "result", "incident", "tracking"
    payload: serde_json::Value,
    created_at: String,
    synced: bool,
}

// ─── Tauri Commands ─────────────────────────────────────────────────────

#[tauri::command]
fn get_offline_pu_count(cache: tauri::State<'_, Mutex<OfflineCache>>) -> usize {
    cache.lock().unwrap().polling_units.len()
}

#[tauri::command]
fn get_last_sync(cache: tauri::State<'_, Mutex<OfflineCache>>) -> Option<String> {
    cache.lock().unwrap().last_sync.clone()
}

#[tauri::command]
fn search_polling_units(
    query: String,
    cache: tauri::State<'_, Mutex<OfflineCache>>,
) -> Vec<PollingUnit> {
    let cache = cache.lock().unwrap();
    let q = query.to_lowercase();
    cache.polling_units
        .iter()
        .filter(|pu| {
            pu.name.to_lowercase().contains(&q)
                || pu.code.to_lowercase().contains(&q)
                || pu.lga_name.to_lowercase().contains(&q)
                || pu.state_code.to_lowercase().contains(&q)
        })
        .take(50)
        .cloned()
        .collect()
}

#[tauri::command]
fn find_nearest_pu(
    lat: f64,
    lng: f64,
    cache: tauri::State<'_, Mutex<OfflineCache>>,
) -> Option<PollingUnit> {
    let cache = cache.lock().unwrap();
    cache.polling_units
        .iter()
        .min_by(|a, b| {
            let da = haversine(lat, lng, a.latitude, a.longitude);
            let db = haversine(lat, lng, b.latitude, b.longitude);
            da.partial_cmp(&db).unwrap()
        })
        .cloned()
}

#[tauri::command]
fn import_offline_data(
    data: Vec<PollingUnit>,
    cache: tauri::State<'_, Mutex<OfflineCache>>,
) -> String {
    let count = data.len();
    let mut cache = cache.lock().unwrap();
    cache.polling_units = data;
    cache.last_sync = Some(chrono_now());
    format!("Imported {} polling units for offline use", count)
}

fn haversine(lat1: f64, lon1: f64, lat2: f64, lon2: f64) -> f64 {
    let r = 6371.0;
    let dlat = (lat2 - lat1).to_radians();
    let dlon = (lon2 - lon1).to_radians();
    let a = (dlat / 2.0).sin().powi(2)
        + lat1.to_radians().cos() * lat2.to_radians().cos() * (dlon / 2.0).sin().powi(2);
    r * 2.0 * a.sqrt().asin()
}

fn chrono_now() -> String {
    // Simple ISO 8601 timestamp without chrono dependency
    use std::time::{SystemTime, UNIX_EPOCH};
    let duration = SystemTime::now().duration_since(UNIX_EPOCH).unwrap();
    let secs = duration.as_secs();
    let hours = (secs / 3600) % 24;
    let minutes = (secs / 60) % 60;
    let seconds = secs % 60;
    format!("{}:{}:{} UTC", hours, minutes, seconds)
}

// ─── Main ───────────────────────────────────────────────────────────────

fn main() {
    tauri::Builder::default()
        .manage(Mutex::new(OfflineCache {
            polling_units: Vec::new(),
            last_sync: None,
        }))
        .invoke_handler(tauri::generate_handler![
            get_offline_pu_count,
            get_last_sync,
            search_polling_units,
            find_nearest_pu,
            import_offline_data,
        ])
        .run(tauri::generate_context!())
        .expect("error while running INEC Field Kit");
}
