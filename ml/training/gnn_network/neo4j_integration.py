"""INEC Neo4j Graph Database Integration.

Stores and queries the election graph structure:
- Polling unit adjacency (geographic + administrative)
- Observer networks (who monitors what)
- Result flow (PU → Ward → LGA → State → National)
- Fraud ring detection (connected components of anomalous PUs)

Neo4j is used as the graph storage backend that feeds the GNN model.
"""

import os
import json
from datetime import datetime, timezone
from typing import Optional

NEO4J_URI = os.environ.get("NEO4J_URI", "bolt://localhost:7687")
NEO4J_USER = os.environ.get("NEO4J_USER", "neo4j")
NEO4J_PASSWORD = os.environ.get("NEO4J_PASSWORD", "")

try:
    from neo4j import GraphDatabase, Driver
    NEO4J_AVAILABLE = True
except ImportError:
    NEO4J_AVAILABLE = False


class ElectionGraphDB:
    """Neo4j interface for election graph operations."""

    def __init__(self, uri: str = NEO4J_URI, user: str = NEO4J_USER, password: str = NEO4J_PASSWORD):
        if not NEO4J_AVAILABLE:
            raise RuntimeError("Install neo4j driver: pip install neo4j>=5.17.0")
        self.driver: Driver = GraphDatabase.driver(uri, auth=(user, password))

    def close(self):
        self.driver.close()

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()

    def initialize_schema(self):
        """Create indexes and constraints for election graph."""
        with self.driver.session() as session:
            # Constraints
            session.run("""
                CREATE CONSTRAINT pu_code_unique IF NOT EXISTS
                FOR (p:PollingUnit) REQUIRE p.code IS UNIQUE
            """)
            session.run("""
                CREATE CONSTRAINT ward_code_unique IF NOT EXISTS
                FOR (w:Ward) REQUIRE w.code IS UNIQUE
            """)
            session.run("""
                CREATE CONSTRAINT lga_code_unique IF NOT EXISTS
                FOR (l:LGA) REQUIRE l.code IS UNIQUE
            """)
            session.run("""
                CREATE CONSTRAINT state_code_unique IF NOT EXISTS
                FOR (s:State) REQUIRE s.code IS UNIQUE
            """)
            session.run("""
                CREATE CONSTRAINT observer_id_unique IF NOT EXISTS
                FOR (o:Observer) REQUIRE o.user_id IS UNIQUE
            """)

            # Spatial index for geographic queries
            session.run("""
                CREATE POINT INDEX pu_location IF NOT EXISTS
                FOR (p:PollingUnit) ON (p.location)
            """)

            # Full-text index for search
            session.run("""
                CREATE FULLTEXT INDEX pu_name_search IF NOT EXISTS
                FOR (p:PollingUnit) ON EACH [p.name, p.code]
            """)

    def ingest_polling_units(self, polling_units: list[dict]):
        """Bulk load polling units into Neo4j.

        Args:
            polling_units: List of dicts with code, name, lat, lon, ward, lga, state
        """
        with self.driver.session() as session:
            session.run("""
                UNWIND $pus AS pu
                MERGE (p:PollingUnit {code: pu.code})
                SET p.name = pu.name,
                    p.location = point({latitude: pu.lat, longitude: pu.lon}),
                    p.registered_voters = pu.registered_voters,
                    p.updated_at = datetime()

                MERGE (w:Ward {code: pu.ward_code})
                SET w.name = pu.ward_name

                MERGE (l:LGA {code: pu.lga_code})
                SET l.name = pu.lga_name

                MERGE (s:State {code: pu.state_code})
                SET s.name = pu.state_name

                MERGE (p)-[:IN_WARD]->(w)
                MERGE (w)-[:IN_LGA]->(l)
                MERGE (l)-[:IN_STATE]->(s)
            """, pus=polling_units)

    def build_adjacency_graph(self, max_distance_km: float = 5.0):
        """Create ADJACENT_TO edges between nearby polling units.

        Uses Neo4j spatial functions for efficient distance computation.
        """
        with self.driver.session() as session:
            # First, build same-ward adjacency (always connected)
            session.run("""
                MATCH (a:PollingUnit)-[:IN_WARD]->(w)<-[:IN_WARD]-(b:PollingUnit)
                WHERE a <> b AND NOT exists((a)-[:ADJACENT_TO]-(b))
                CREATE (a)-[:ADJACENT_TO {type: 'same_ward', distance_km: 0}]->(b)
            """)

            # Then, geographic proximity (using spatial index)
            session.run("""
                MATCH (a:PollingUnit), (b:PollingUnit)
                WHERE a <> b
                  AND point.distance(a.location, b.location) < $max_dist_m
                  AND NOT exists((a)-[:ADJACENT_TO]-(b))
                CREATE (a)-[:ADJACENT_TO {
                    type: 'geographic',
                    distance_km: point.distance(a.location, b.location) / 1000.0
                }]->(b)
            """, max_dist_m=max_distance_km * 1000)

    def store_results(self, results: list[dict]):
        """Store election results on polling unit nodes."""
        with self.driver.session() as session:
            session.run("""
                UNWIND $results AS r
                MATCH (p:PollingUnit {code: r.pu_code})
                SET p.total_valid_votes = r.total_valid_votes,
                    p.total_rejected_votes = r.total_rejected_votes,
                    p.accredited_voters = r.accredited_voters,
                    p.turnout_rate = r.accredited_voters * 1.0 / CASE WHEN p.registered_voters > 0 THEN p.registered_voters ELSE 1 END,
                    p.party_results = r.party_results,
                    p.result_submitted_at = datetime(r.submitted_at)
            """, results=results)

    def flag_anomalies(self, anomaly_scores: dict[str, float], threshold: float = 0.7):
        """Flag polling units with high anomaly scores from GNN inference.

        Args:
            anomaly_scores: {pu_code: anomaly_probability}
            threshold: Score above which to flag
        """
        flagged = [
            {"code": code, "score": score}
            for code, score in anomaly_scores.items()
            if score >= threshold
        ]

        with self.driver.session() as session:
            session.run("""
                UNWIND $flagged AS f
                MATCH (p:PollingUnit {code: f.code})
                SET p.anomaly_score = f.score,
                    p.flagged = true,
                    p.flagged_at = datetime()
            """, flagged=flagged)

    def detect_fraud_rings(self, min_cluster_size: int = 3) -> list[dict]:
        """Detect clusters of adjacent anomalous polling units (fraud rings).

        A fraud ring is a connected component of flagged PUs — suggests
        coordinated manipulation across multiple locations.
        """
        with self.driver.session() as session:
            result = session.run("""
                MATCH (p:PollingUnit {flagged: true})
                CALL {
                    WITH p
                    MATCH path = (p)-[:ADJACENT_TO*1..3]-(neighbor:PollingUnit {flagged: true})
                    RETURN collect(DISTINCT neighbor.code) + [p.code] AS cluster_codes
                }
                WITH cluster_codes
                WHERE size(cluster_codes) >= $min_size
                RETURN DISTINCT cluster_codes, size(cluster_codes) AS cluster_size
                ORDER BY cluster_size DESC
            """, min_size=min_cluster_size)

            return [
                {"codes": record["cluster_codes"], "size": record["cluster_size"]}
                for record in result
            ]

    def get_neighborhood_stats(self, pu_code: str, hops: int = 2) -> dict:
        """Get aggregate statistics for a PU's neighborhood.

        Used to compute "deviation from neighborhood" features for GNN.
        """
        with self.driver.session() as session:
            result = session.run("""
                MATCH (center:PollingUnit {code: $code})
                MATCH path = (center)-[:ADJACENT_TO*1..$hops]-(neighbor:PollingUnit)
                WITH center, collect(DISTINCT neighbor) AS neighbors
                RETURN center.code AS code,
                       center.turnout_rate AS center_turnout,
                       avg([n IN neighbors | n.turnout_rate]) AS avg_neighbor_turnout,
                       stDev([n IN neighbors | n.turnout_rate]) AS std_neighbor_turnout,
                       size(neighbors) AS n_neighbors,
                       size([n IN neighbors WHERE n.flagged = true]) AS n_flagged_neighbors
            """, code=pu_code, hops=hops)

            record = result.single()
            if record:
                return dict(record)
            return {}

    def export_graph_for_gnn(self) -> dict:
        """Export the full graph structure for GNN inference.

        Returns node features and edge_index in PyTorch Geometric format.
        """
        import numpy as np

        with self.driver.session() as session:
            # Get all nodes with features
            nodes_result = session.run("""
                MATCH (p:PollingUnit)
                RETURN p.code AS code,
                       p.registered_voters AS registered,
                       p.accredited_voters AS accredited,
                       p.turnout_rate AS turnout,
                       p.total_valid_votes AS valid,
                       p.total_rejected_votes AS rejected,
                       p.anomaly_score AS anomaly_score
                ORDER BY p.code
            """)
            nodes = [dict(r) for r in nodes_result]

            # Get all edges
            edges_result = session.run("""
                MATCH (a:PollingUnit)-[:ADJACENT_TO]->(b:PollingUnit)
                RETURN a.code AS src, b.code AS dst
            """)
            edges = [(r["src"], r["dst"]) for r in edges_result]

        # Map codes to indices
        code_to_idx = {node["code"]: i for i, node in enumerate(nodes)}

        # Build edge_index
        edge_index = np.array([
            [code_to_idx[src], code_to_idx[dst]]
            for src, dst in edges
            if src in code_to_idx and dst in code_to_idx
        ], dtype=np.int64).T

        # Build feature matrix
        features = np.array([
            [
                n.get("registered", 0) or 0,
                n.get("accredited", 0) or 0,
                n.get("turnout", 0) or 0,
                n.get("valid", 0) or 0,
                n.get("rejected", 0) or 0,
                0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,  # Placeholder for full 17 features
            ]
            for n in nodes
        ], dtype=np.float32)

        return {
            "node_features": features,
            "edge_index": edge_index,
            "code_to_idx": code_to_idx,
            "idx_to_code": {v: k for k, v in code_to_idx.items()},
            "n_nodes": len(nodes),
            "n_edges": edge_index.shape[1] if len(edges) > 0 else 0,
        }

    def assign_observer(self, observer_id: int, pu_codes: list[str]):
        """Assign an observer to monitor specific polling units."""
        with self.driver.session() as session:
            session.run("""
                MERGE (o:Observer {user_id: $observer_id})
                WITH o
                UNWIND $codes AS code
                MATCH (p:PollingUnit {code: code})
                MERGE (o)-[:MONITORS]->(p)
            """, observer_id=observer_id, codes=pu_codes)

    def get_observer_network(self, observer_id: int) -> dict:
        """Get the network of PUs an observer monitors and their relationships."""
        with self.driver.session() as session:
            result = session.run("""
                MATCH (o:Observer {user_id: $id})-[:MONITORS]->(p:PollingUnit)
                OPTIONAL MATCH (p)-[:ADJACENT_TO]-(neighbor:PollingUnit)
                RETURN o.user_id AS observer,
                       collect(DISTINCT p.code) AS monitored_pus,
                       count(DISTINCT neighbor) AS total_neighbors,
                       size([n IN collect(DISTINCT neighbor) WHERE n.flagged = true]) AS flagged_neighbors
            """, id=observer_id)

            record = result.single()
            return dict(record) if record else {}


if __name__ == "__main__":
    print("Neo4j Election Graph Database")
    print(f"  URI: {NEO4J_URI}")
    print(f"  Available: {NEO4J_AVAILABLE}")

    if NEO4J_AVAILABLE and NEO4J_PASSWORD:
        with ElectionGraphDB() as db:
            db.initialize_schema()
            print("Schema initialized successfully")
    else:
        print("Set NEO4J_PASSWORD env var to connect")
        print("\nExample usage:")
        print("  export NEO4J_URI=bolt://localhost:7687")
        print("  export NEO4J_USER=neo4j")
        print("  export NEO4J_PASSWORD=your_password")
        print("  python neo4j_integration.py")
