//! Neo4j client for election graph queries.
//!
//! Provides:
//! - Polling unit neighborhood queries (for GNN feature extraction)
//! - Fraud ring detection (connected components of flagged nodes)
//! - Result flow validation (hierarchical aggregation checks)
//! - Observer network analysis

use anyhow::{Context, Result};
use neo4rs::{query, Graph, ConfigBuilder};
use serde::Serialize;
use tracing::info;

/// Neo4j client for election graph operations.
pub struct Neo4jClient {
    graph: Graph,
}

#[derive(Serialize)]
pub struct NeighborInfo {
    pub code: String,
    pub turnout: f64,
    pub distance_km: f64,
    pub flagged: bool,
}

#[derive(Serialize)]
pub struct GraphQueryResponse {
    pub pu_code: String,
    pub neighbors: Vec<NeighborInfo>,
    pub avg_neighbor_turnout: f64,
    pub deviation_from_neighbors: f64,
    pub flagged_neighbors: u32,
}

#[derive(Serialize)]
pub struct FraudRing {
    pub cluster_id: String,
    pub polling_units: Vec<String>,
    pub size: usize,
    pub avg_anomaly_score: f64,
    pub geographic_spread_km: f64,
}

impl Neo4jClient {
    /// Connect to Neo4j using environment variables.
    pub async fn connect() -> Result<Self> {
        let uri = std::env::var("NEO4J_URI").unwrap_or_else(|_| "bolt://localhost:7687".into());
        let user = std::env::var("NEO4J_USER").unwrap_or_else(|_| "neo4j".into());
        let password = std::env::var("NEO4J_PASSWORD").unwrap_or_else(|_| String::new());

        if password.is_empty() {
            anyhow::bail!("NEO4J_PASSWORD not set");
        }

        let config = ConfigBuilder::default()
            .uri(&uri)
            .user(&user)
            .password(&password)
            .db("neo4j")
            .max_connections(10)
            .build()?;

        let graph = Graph::connect(config).await
            .context("Failed to connect to Neo4j")?;

        info!(uri, "Connected to Neo4j");
        Ok(Self { graph })
    }

    /// Get neighborhood statistics for a polling unit.
    /// Used by GNN to compute "deviation from neighbors" features.
    pub async fn get_neighborhood(&self, pu_code: &str, hops: u32) -> Result<GraphQueryResponse> {
        let cypher = format!(
            r#"
            MATCH (center:PollingUnit {{code: $code}})
            MATCH path = (center)-[:ADJACENT_TO*1..{}]-(neighbor:PollingUnit)
            WITH center, collect(DISTINCT neighbor) AS neighbors
            UNWIND neighbors AS n
            RETURN center.code AS center_code,
                   center.turnout_rate AS center_turnout,
                   n.code AS neighbor_code,
                   n.turnout_rate AS neighbor_turnout,
                   COALESCE(n.flagged, false) AS neighbor_flagged,
                   point.distance(center.location, n.location) / 1000.0 AS distance_km
            "#,
            hops
        );

        let mut result = self.graph
            .execute(query(&cypher).param("code", pu_code))
            .await
            .context("Neo4j query failed")?;

        let mut neighbors = Vec::new();
        let mut turnouts = Vec::new();
        let mut flagged_count = 0u32;
        let mut center_turnout = 0.0f64;

        while let Ok(Some(row)) = result.next().await {
            if center_turnout == 0.0 {
                center_turnout = row.get::<f64>("center_turnout").unwrap_or(0.0);
            }

            let turnout = row.get::<f64>("neighbor_turnout").unwrap_or(0.0);
            let flagged = row.get::<bool>("neighbor_flagged").unwrap_or(false);
            if flagged { flagged_count += 1; }
            turnouts.push(turnout);

            neighbors.push(NeighborInfo {
                code: row.get::<String>("neighbor_code").unwrap_or_default(),
                turnout,
                distance_km: row.get::<f64>("distance_km").unwrap_or(0.0),
                flagged,
            });
        }

        let avg_turnout = if turnouts.is_empty() {
            0.0
        } else {
            turnouts.iter().sum::<f64>() / turnouts.len() as f64
        };

        Ok(GraphQueryResponse {
            pu_code: pu_code.to_string(),
            neighbors,
            avg_neighbor_turnout: avg_turnout,
            deviation_from_neighbors: center_turnout - avg_turnout,
            flagged_neighbors: flagged_count,
        })
    }

    /// Detect fraud rings — connected components of anomalous polling units.
    pub async fn detect_fraud_rings(&self, min_size: u32) -> Result<Vec<FraudRing>> {
        let cypher = r#"
            MATCH (p:PollingUnit {flagged: true})
            CALL {
                WITH p
                MATCH (p)-[:ADJACENT_TO*1..3]-(neighbor:PollingUnit {flagged: true})
                RETURN collect(DISTINCT neighbor.code) + [p.code] AS cluster_codes,
                       avg(neighbor.anomaly_score) AS avg_score
            }
            WITH cluster_codes, avg_score
            WHERE size(cluster_codes) >= $min_size
            RETURN DISTINCT cluster_codes, size(cluster_codes) AS cluster_size, avg_score
            ORDER BY cluster_size DESC
            LIMIT 50
        "#;

        let mut result = self.graph
            .execute(query(cypher).param("min_size", min_size as i64))
            .await
            .context("Fraud ring detection query failed")?;

        let mut rings = Vec::new();
        let mut ring_idx = 0;

        while let Ok(Some(row)) = result.next().await {
            let codes: Vec<String> = row.get("cluster_codes").unwrap_or_default();
            let size = codes.len();
            let avg_score: f64 = row.get("avg_score").unwrap_or(0.0);

            rings.push(FraudRing {
                cluster_id: format!("ring-{:04}", ring_idx),
                polling_units: codes,
                size,
                avg_anomaly_score: avg_score,
                geographic_spread_km: 0.0, // Computed separately if needed
            });
            ring_idx += 1;
        }

        Ok(rings)
    }

    /// Store anomaly scores from GNN inference back to Neo4j.
    pub async fn store_anomaly_scores(&self, scores: &[(String, f64)]) -> Result<()> {
        let cypher = r#"
            UNWIND $scores AS s
            MATCH (p:PollingUnit {code: s.code})
            SET p.anomaly_score = s.score,
                p.flagged = CASE WHEN s.score >= 0.7 THEN true ELSE false END,
                p.scored_at = datetime()
        "#;

        let scores_param: Vec<serde_json::Value> = scores.iter()
            .map(|(code, score)| serde_json::json!({"code": code, "score": score}))
            .collect();

        self.graph
            .run(query(cypher).param("scores", scores_param))
            .await
            .context("Failed to store anomaly scores")?;

        info!(n_scores = scores.len(), "Stored GNN anomaly scores in Neo4j");
        Ok(())
    }

    /// Export graph structure for GNN inference.
    /// Returns (node_features, edge_index, code_map).
    pub async fn export_for_gnn(&self) -> Result<(Vec<Vec<f64>>, Vec<[usize; 2]>, Vec<String>)> {
        // Get all nodes
        let node_query = r#"
            MATCH (p:PollingUnit)
            RETURN p.code AS code,
                   COALESCE(p.registered_voters, 0) AS registered,
                   COALESCE(p.accredited_voters, 0) AS accredited,
                   COALESCE(p.turnout_rate, 0) AS turnout,
                   COALESCE(p.total_valid_votes, 0) AS valid,
                   COALESCE(p.total_rejected_votes, 0) AS rejected
            ORDER BY p.code
        "#;

        let mut node_result = self.graph.execute(query(node_query)).await?;
        let mut codes = Vec::new();
        let mut features = Vec::new();

        while let Ok(Some(row)) = node_result.next().await {
            let code: String = row.get("code").unwrap_or_default();
            let registered: f64 = row.get("registered").unwrap_or(0.0);
            let accredited: f64 = row.get("accredited").unwrap_or(0.0);
            let turnout: f64 = row.get("turnout").unwrap_or(0.0);
            let valid: f64 = row.get("valid").unwrap_or(0.0);
            let rejected: f64 = row.get("rejected").unwrap_or(0.0);

            // 17-dim feature vector (pad remaining with 0)
            let feat = vec![
                registered, accredited, turnout, valid, rejected,
                0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0,
            ];
            features.push(feat);
            codes.push(code);
        }

        // Build code-to-index map
        let code_to_idx: std::collections::HashMap<&str, usize> = codes.iter()
            .enumerate()
            .map(|(i, c)| (c.as_str(), i))
            .collect();

        // Get all edges
        let edge_query = r#"
            MATCH (a:PollingUnit)-[:ADJACENT_TO]->(b:PollingUnit)
            RETURN a.code AS src, b.code AS dst
        "#;

        let mut edge_result = self.graph.execute(query(edge_query)).await?;
        let mut edges = Vec::new();

        while let Ok(Some(row)) = edge_result.next().await {
            let src: String = row.get("src").unwrap_or_default();
            let dst: String = row.get("dst").unwrap_or_default();
            if let (Some(&si), Some(&di)) = (code_to_idx.get(src.as_str()), code_to_idx.get(dst.as_str())) {
                edges.push([si, di]);
            }
        }

        info!(nodes = codes.len(), edges = edges.len(), "Graph exported for GNN");
        Ok((features, edges, codes))
    }
}
