# Open-Source Geospatial Components — INEC Election Platform

## Overview

The INEC platform is built on a rich, multi-layer geospatial stack spanning five languages (Go, Rust, Python, TypeScript, SQL) and covering the full pipeline from raw coordinate ingestion to interactive map visualisation. Every component listed below is open-source. The stack is deliberately polyglot: each language is used where it excels — Rust for high-throughput spatial indexing, Go for API serving, Python for analytics, and TypeScript for interactive visualisation.

---

## Layer 1 — Database: PostGIS (PostgreSQL Extension)

PostGIS is the foundational geospatial layer. It extends PostgreSQL with native geometry and geography column types and a library of over 300 spatial functions. The platform uses it for all persistent spatial data.

| Function Used | Purpose |
|---|---|
| `ST_SetSRID(ST_MakePoint(lon, lat), 4326)` | Store point geometries in WGS-84 |
| `ST_AsMVT` / `ST_AsMVTGeom` | Generate Mapbox Vector Tiles (MVT) directly from the database |
| `ST_AsGeoJSON` | Serialize geometries to GeoJSON RFC 7946 |
| `ST_ClusterDBSCAN` | DBSCAN spatial clustering of polling units |
| `ST_Buffer` / `ST_DWithin` | Proximity and geofence checks |
| `ST_Voronoi` | Voronoi polygon generation for ward coverage analysis |
| `geometry(Point,4326)` / `geometry(Polygon,4326)` | Typed geometry columns with SRID constraint |

**License:** GPL-2.0 | **Version:** bundled with PostgreSQL 16-alpine

---

## Layer 2 — Go Backend: Native Spatial Processing

The Go backend implements its own lightweight MVT (Mapbox Vector Tile) encoder and H3-like hexagonal grid logic without external Go geospatial libraries, relying instead on PostGIS for the heavy lifting. The spatial API surface is extensive.

| API Endpoint | Spatial Operation |
|---|---|
| `GET /geo/tiles/mvt/{z}/{x}/{y}.mvt` | PostGIS MVT tile streaming |
| `GET /geo/h3/grid` | H3-like hexagonal aggregation grid |
| `GET /geo/clusters` | DBSCAN clustering via PostGIS |
| `GET /geo/heatmap` | Kernel density heatmap data |
| `GET /geo/nearby-pus` | Nearest-neighbour polling unit search |
| `GET /geo/boundary` | Administrative boundary polygons |
| `GET /geo/spatial-stats` | Spatial statistical summaries |
| `GET /geo/sedona/analysis` | Proxy to Apache Sedona distributed analytics |
| `POST /geo/spoof-check` | GPS spoofing detection |
| `POST /geo/submission/check` | Geofenced result submission validation |
| `POST /geofence/check` | Real-time geofence containment check |
| `GET /geo/tracking/officials` | Live official location tracking |
| `GET /geo/live-stream` | SSE stream of live geo events |

**MVT Encoder:** Custom Go implementation in `mvt.go` — encodes protobuf-encoded vector tile layers directly, compatible with the Mapbox Vector Tile Specification 2.1.

---

## Layer 3 — Rust Services: High-Performance Spatial Indexing

Two dedicated Rust microservices handle the most computationally intensive spatial workloads.

### 3a. `inec-geolibre-spatial` (GeoLibre Spatial Analysis Engine)

| Crate | Version | Purpose |
|---|---|---|
| `geo` | 0.28 | Core geometry types: Point, Polygon, LineString, MultiPolygon |
| `geo-types` | 0.7 | Shared geometry type definitions |
| `geojson` | 0.24 | GeoJSON serialisation / deserialisation (RFC 7946) |
| `h3o` | 0.6 | Pure-Rust H3 hierarchical hexagonal grid system |
| `actix-web` | 4 | HTTP server for the spatial API |
| `serde_json` | 1 | JSON serialisation |

**H3 (`h3o`)** is the pure-Rust implementation of Uber's H3 geospatial indexing system. It enables the platform to aggregate election data into hexagonal cells at multiple resolutions (0–15), supporting the choropleth and heatmap visualisations on the GeoLibre map.

### 3b. `inec-gotv-engine` (Get-Out-The-Vote Geo Engine)

| Crate | Version | Purpose |
|---|---|---|
| `geo` | 0.28 | Geometry operations for volunteer matching |
| `rstar` | 0.12 | R*-tree spatial index for nearest-neighbour queries |
| `ordered-float` | 4 | Float ordering for spatial comparisons |
| `axum` | 0.7 | HTTP server |

**`rstar`** is a Rust implementation of the R*-tree spatial index, enabling sub-millisecond nearest-neighbour lookups for matching voters to volunteer drivers and polling units.

---

## Layer 4 — Python: Distributed Geospatial Analytics

The Python lakehouse and ML pipeline layer handles batch geospatial analytics at scale.

| Library | Version | Purpose |
|---|---|---|
| `duckdb` | ≥0.10.0 | In-process OLAP with DuckDB Spatial extension for GeoJSON/WKB queries |
| `pyarrow` | ≥15.0.0 | Apache Arrow columnar format for spatial data exchange |
| `numpy` | ≥1.26.0 | Numerical operations for coordinate arrays |
| `scipy` | ≥1.12.0 | Statistical spatial analysis (kernel density, clustering) |
| `scikit-learn` | ≥1.4.0 | IsolationForest for spatial anomaly detection |

**Apache Sedona** is referenced as an optional distributed engine (`SEDONA_URL` env var). When connected, the platform routes spatial queries to a Sedona cluster for distributed processing of large datasets. When unavailable, it falls back to DuckDB Spatial.

---

## Layer 5 — TypeScript Frontend: Interactive Map Visualisation

The frontend map stack is built on three complementary open-source libraries.

### 5a. MapLibre GL JS

**License:** BSD-3-Clause | **Version:** ^5.18.0

MapLibre GL is the open-source fork of Mapbox GL JS. It is the primary map renderer, handling WebGL-accelerated tile rendering, camera controls, and layer management. The platform uses it to render:
- Administrative boundary layers (states, LGAs, wards)
- Polling unit point clusters
- Live official tracking overlays

### 5b. deck.gl

**License:** MIT | **Version:** ^9.3.x

deck.gl is a WebGL-powered large-scale data visualisation framework from vis.gl (formerly Uber). The platform uses the following deck.gl layers:

| Layer | Module | Purpose |
|---|---|---|
| `ScatterplotLayer` | `@deck.gl/layers` | Polling unit scatter plot |
| `ArcLayer` | `@deck.gl/layers` | Vote flow arcs between wards |
| `GeoJsonLayer` | `@deck.gl/layers` | Administrative boundary polygons |
| `TextLayer` | `@deck.gl/layers` | Polling unit labels |
| `H3HexagonLayer` | `@deck.gl/geo-layers` | H3 hexagonal aggregation choropleth |
| `HeatmapLayer` | `@deck.gl/aggregation-layers` | Voter density and incident heatmaps |
| `MapboxOverlay` | `@deck.gl/mapbox` | deck.gl integration with MapLibre GL |

### 5c. H3-JS

**License:** Apache-2.0 | **Version:** ^4.4.0

H3-JS is the JavaScript port of Uber's H3 geospatial indexing system. The frontend uses `latLngToCell` to compute H3 cell indices client-side for interactive hexagonal aggregation before sending queries to the backend.

### 5d. Turf.js

**License:** MIT | **Version:** ^7.3.5 (`@turf/turf`)

Turf.js is a modular geospatial analysis library for JavaScript. It is used for client-side geometry operations such as bounding box computation, point-in-polygon checks, and distance calculations.

---

## Layer 6 — Coordinate Reference Systems and Data Standards

| Standard | Usage |
|---|---|
| **EPSG:4326** (WGS-84) | All coordinates stored and served in WGS-84 geographic coordinates |
| **GeoJSON RFC 7946** | All API responses use RFC 7946 GeoJSON with `"crs": "EPSG:4326"` |
| **Mapbox Vector Tiles 2.1** | Binary tile format for the `/geo/tiles/mvt/{z}/{x}/{y}.mvt` endpoint |
| **H3 Hierarchical Grid** | Hexagonal spatial indexing at resolutions 5–9 for aggregation |
| **DBSCAN** | Density-Based Spatial Clustering of Applications with Noise |

---

## Summary Table

| Component | Language | License | Role |
|---|---|---|---|
| **PostGIS** | SQL/C | GPL-2.0 | Persistent geometry storage, MVT generation, spatial queries |
| **geo** (Rust crate) | Rust | MIT/Apache-2.0 | Core geometry types and algorithms |
| **geo-types** (Rust crate) | Rust | MIT/Apache-2.0 | Shared geometry type definitions |
| **geojson** (Rust crate) | Rust | MIT | GeoJSON serialisation |
| **h3o** (Rust crate) | Rust | Apache-2.0 | H3 hexagonal grid (pure Rust) |
| **rstar** (Rust crate) | Rust | MIT/Apache-2.0 | R*-tree spatial index |
| **ordered-float** (Rust crate) | Rust | MIT/Apache-2.0 | Float ordering for spatial comparisons |
| **DuckDB Spatial** | Python | MIT | In-process OLAP geospatial analytics |
| **Apache Sedona** (optional) | Python | Apache-2.0 | Distributed spatial analytics |
| **PyArrow** | Python | Apache-2.0 | Columnar spatial data exchange |
| **MapLibre GL JS** | TypeScript | BSD-3-Clause | WebGL map rendering |
| **deck.gl** | TypeScript | MIT | Large-scale geospatial data visualisation |
| **H3-JS** | TypeScript | Apache-2.0 | Client-side H3 hexagonal indexing |
| **Turf.js** | TypeScript | MIT | Client-side geometry operations |
| **MVT Encoder** (custom) | Go | MIT (project) | Protobuf-encoded vector tile generation |
