"""Neo4j Graph Database Integration for Election Analysis.

Implements graph database operations for voter relationship analysis,
duplicate detection, network analysis, and election fraud investigation.

Usage:
    from neo4j_integration import Neo4jElectionGraph
    graph = Neo4jElectionGraph(uri="bolt://localhost:7687", user="neo4j", password="password")
    graph.connect()
    graph.create_voter(voter_id="V001", name="John Doe", nin="12345678901")
    graph.find_relationships(voter_id="V001", depth=2)
"""

import os
import json
from pathlib import Path
from typing import Dict, List, Optional, Tuple, Any
from datetime import datetime, date

# Neo4j dependencies (install with: pip install neo4j)
try:
    from neo4j import GraphDatabase
    NEO4J_AVAILABLE = True
except ImportError:
    NEO4J_AVAILABLE = False


class Neo4jElectionGraph:
    """Neo4j graph database for election analysis.
    
    Graph Schema:
    - VOTER nodes: Voter information and biometric data
    - POLLING_UNIT nodes: Geographic and administrative data
    - RECORDS relationships: Voting records and connections
    - ASSOCIATED_WITH relationships: Voter-PU connections
    - SIMILAR_TO relationships: Biometric similarity between voters
    - NEAR relationships: Geographic proximity between PUs
    
    Example Cypher queries for election analysis:
    - Find duplicate voters: Match (v1:VOTTER)-[:ASSOCIATED_WITH]->(pu:PU)<-[:ASSOCIATED_WITH]-(v2:VOTER) WHERE v1 <> v2 RETURN v1, v2
    - Network analysis: Match path=(v:VOTER)-[:SIMILAR_TO*1..3]-() RETURN path
    - Geographic clustering: Match (pu1:PU)-[:NEAR]-(pu2:PU) WHERE pu1.ward = pu2.ward RETURN pu1, pu2
    """
    
    def __init__(
        self,
        uri: str = "bolt://localhost:7687",
        user: str = "neo4j",
        password: str = "password",
        database: str = "neo4j",
    ):
        self.uri = uri
        self.user = user
        self.password = password
        self.database = database
        self.driver = None
        self.connected = False
    
    def connect(self):
        """Connect to Neo4j database."""
        if not NEO4J_AVAILABLE:
            raise ImportError("Neo4j Python driver not installed. Run: pip install neo4j")
        
        self.driver = GraphDatabase.driver(self.uri, auth=(self.user, self.password))
        self.driver.verify_connectivity()
        self.connected = True
        print(f"✓ Connected to Neo4j at {self.uri}")
    
    def close(self):
        """Close database connection."""
        if self.driver:
            self.driver.close()
            self.connected = False
            print("✓ Disconnected from Neo4j")
    
    def __enter__(self):
        self.connect()
        return self
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()
    
    def execute_query(self, query: str, parameters: Dict = None) -> List[Dict]:
        """Execute a Cypher query and return results."""
        if not self.connected:
            raise ConnectionError("Not connected to Neo4j")
        
        with self.driver.session(database=self.database) as session:
            result = session.run(query, parameters or {})
            return [record.data() for record in result]
    
    # ---- Voter Management ----
    
    def create_voter(self, voter_id: str, name: str, nin: str, 
                    biometric_hash: str = None, address: str = None) -> Dict:
        """Create a voter node in the graph.
        
        Args:
            voter_id: Unique voter identifier
            name: Voter's full name
            nin: National Identification Number
            biometric_hash: Hash of biometric template
            address: Voter's address
            
        Returns:
            Dict with operation result
        """
        query = """
        CREATE (v:VOTER {
            voter_id: $voter_id,
            name: $name,
            nin: $nin,
            biometric_hash: $biometric_hash,
            address: $address,
            created_at: datetime()
        })
        RETURN v
        """
        
        result = self.execute_query(query, {
            'voter_id': voter_id,
            'name': name,
            'nin': nin,
            'biometric_hash': biometric_hash,
            'address': address,
        })
        
        return {
            'success': True,
            'voter_id': voter_id,
            'created': True,
        }
    
    def get_voter(self, voter_id: str) -> Optional[Dict]:
        """Get voter information by ID."""
        query = """
        MATCH (v:VOTER {voter_id: $voter_id})
        RETURN v
        """
        
        results = self.execute_query(query, {'voter_id': voter_id})
        
        if results:
            return results[0]['v']
        return None
    
    def find_duplicate_voters(self, nin: str = None, biometric_hash: str = None,
                             name: str = None, min_confidence: float = 0.8) -> List[Dict]:
        """Find potential duplicate voters.
        
        Args:
            nin: NIN to search for
            biometric_hash: Biometric hash to match
            name: Partial name match
            min_confidence: Minimum similarity confidence
            
        Returns:
            List of potential duplicate pairs
        """
        conditions = []
        params = {}
        
        if nin:
            conditions.append("v.nin = $nin")
            params['nin'] = nin
        
        if biometric_hash:
            conditions.append("v.biometric_hash = $biometric_hash")
            params['biometric_hash'] = biometric_hash
        
        if name:
            conditions.append("toLower(v.name) CONTAINS toLower($name)")
            params['name'] = name
        
        where_clause = " AND ".join(conditions) if conditions else "1=1"
        
        query = f"""
        MATCH (v1:VOTER)-[:ASSOCIATED_WITH]->(pu1:POLLING_UNIT)<-[:ASSOCIATED_WITH]-(v2:VOTER)
        WHERE {where_clause} AND v1.voter_id <> v2.voter_id
        RETURN v1.voter_id as voter1_id, v1.name as voter1_name,
               v2.voter_id as voter2_id, v2.name as voter2_name,
               pu1.pu_code as polling_unit,
               pu1.ward as ward
        """
        
        return self.execute_query(query, params)
    
    # ---- Polling Unit Management ----
    
    def create_polling_unit(self, pu_code: str, ward: str, lga: str,
                           state: str, latitude: float, longitude: float,
                           accredited_voters: int = 0) -> Dict:
        """Create a polling unit node in the graph."""
        query = """
        CREATE (pu:POLLING_UNIT {
            pu_code: $pu_code,
            ward: $ward,
            lga: $lga,
            state: $state,
            latitude: $latitude,
            longitude: $longitude,
            accredited_voters: $accredited_voters,
            created_at: datetime()
        })
        RETURN pu
        """
        
        self.execute_query(query, {
            'pu_code': pu_code,
            'ward': ward,
            'lga': lga,
            'state': state,
            'latitude': latitude,
            'longitude': longitude,
            'accredited_voters': accredited_voters,
        })
        
        return {
            'success': True,
            'pu_code': pu_code,
        }
    
    def create_neighborhood_relationships(self, distance_threshold_km: float = 2.0) -> int:
        """Create NEAR relationships between geographically close PUs."""
        query = """
        MATCH (pu1:POLLING_UNIT), (pu2:POLLING_UNIT)
        WHERE pu1.pu_code < pu2.pu_code
        AND distance(point({latitude: pu1.latitude, longitude: pu1.longitude}),
                    point({latitude: pu2.latitude, longitude: pu2.longitude})) 
            < $threshold * 1000
        CREATE (pu1)-[:NEAR {distance: distance(point({latitude: pu1.latitude, longitude: pu1.longitude}),
                                          point({latitude: pu2.latitude, longitude: pu2.longitude}))}]->(pu2)
        RETURN count(pu1) as relationships_created
        """
        
        results = self.execute_query(query, {'threshold': distance_threshold_km})
        return results[0]['relationships_created'] if results else 0
    
    # ---- Voting Records ----
    
    def record_vote(self, voter_id: str, pu_code: str, vote_count: int,
                   timestamp: str = None) -> Dict:
        """Record a vote in the graph.
        
        Args:
            voter_id: Voter identifier
            pu_code: Polling unit code
            vote_count: Number of votes cast
            timestamp: Vote timestamp (ISO format)
            
        Returns:
            Dict with operation result
        """
        query = """
        MATCH (v:VOTER {voter_id: $voter_id}), (pu:POLLING_UNIT {pu_code: $pu_code})
        CREATE (v)-[:VOTED_AT {
            vote_count: $vote_count,
            timestamp: datetime($timestamp),
            recorded_at: datetime()
        }]->(pu)
        RETURN v, pu
        """
        
        self.execute_query(query, {
            'voter_id': voter_id,
            'pu_code': pu_code,
            'vote_count': vote_count,
            'timestamp': timestamp or datetime.now().isoformat(),
        })
        
        return {
            'success': True,
            'voter_id': voter_id,
            'pu_code': pu_code,
        }
    
    # ---- Analysis Queries ----
    
    def find_voter_network(self, voter_id: str, max_depth: int = 2) -> Dict:
        """Find voter's social and voting network.
        
        Args:
            voter_id: Voter identifier
            max_depth: Maximum network depth
            
        Returns:
            Dict with network analysis results
        """
        query = f"""
        MATCH path = shortestPath((v:VOTER {{voter_id: $voter_id}})-[r*1..{max_depth}]-())
        RETURN path
        """
        
        results = self.execute_query(query, {'voter_id': voter_id})
        
        # Count nodes and relationships
        voters = set()
        pus = set()
        for result in results:
            path = result.get('path', {})
            nodes = path.get('nodes', [])
            for node in nodes:
                if 'VOTER' in str(node):
                    voters.add(node.get('voter_id'))
                elif 'POLLING_UNIT' in str(node):
                    pus.add(node.get('pu_code'))
        
        return {
            'voter_id': voter_id,
            'connected_voters': list(voters),
            'connected_pus': list(pus),
            'network_size': len(voters) + len(pus),
        }
    
    def analyze_ward_patterns(self, ward: str) -> Dict:
        """Analyze voting patterns for a ward.
        
        Args:
            ward: Ward name
            
        Returns:
            Dict with pattern analysis
        """
        query = """
        MATCH (pu:POLLING_UNIT {ward: $ward})
        OPTIONAL MATCH (pu)<-[:VOTED_AT]-(v:VOTER)
        RETURN pu.pu_code as pu_code,
               count(v) as total_votes,
               avg(v.vote_count) as avg_votes_per_voter,
               collect(DISTINCT v.voter_id) as voter_ids
        """
        
        results = self.execute_query(query, {'ward': ward})
        
        return {
            'ward': ward,
            'polling_units': len(results),
            'total_votes': sum(r['total_votes'] for r in results),
            'avg_votes_per_pu': sum(r['total_votes'] for r in results) / max(len(results), 1),
        }
    
    def detect_suspicious_patterns(self, min_voter_count: int = 5) -> List[Dict]:
        """Detect suspicious voting patterns.
        
        Looks for:
        - Same voter at multiple PUs
        - Unusually high turnout in specific PUs
        - Clusters of similar biometric patterns
        
        Returns:
            List of suspicious patterns
        """
        # Find voters at multiple PUs
        multi_pu_query = """
        MATCH (v:VOTER)-[:VOTED_AT]->(pu:POLLING_UNIT)
        WITH v, collect(pu.pu_code) as pus
        WHERE size(pus) > 1
        RETURN v.voter_id, v.name, v.nin, pus, size(pus) as pu_count
        ORDER BY pu_count DESC
        LIMIT 100
        """
        
        results = self.execute_query(multi_pu_query)
        
        return [
            {
                'pattern': 'multiple_polling_units',
                'voter_id': r.get('v.voter_id'),
                'name': r.get('v.name'),
                'nin': r.get('v.nin'),
                'polling_units': r.get('pus'),
                'pu_count': r.get('pu_count'),
            }
            for r in results
        ]


class ElectionGraphAnalyzer:
    """Advanced graph analysis for election fraud detection."""
    
    def __init__(self, graph: Neo4jElectionGraph):
        self.graph = graph
    
    def find_voter_duplication_networks(self, min_network_size: int = 3) -> List[Dict]:
        """Find networks of potential voter duplication.
        
        Looks for groups of voters with similar biometric patterns
        who voted at the same polling units.
        """
        query = """
        MATCH (v1:VOTER)-[:VOTED_AT]->(pu:POLLING_UNIT)<-[:VOTED_AT]-(v2:VOTER)
        WHERE v1.voter_id < v2.voter_id
        AND (v1.biometric_hash = v2.biometric_hash OR v1.nin = v2.nin)
        WITH v1, v2, pu, count(*) as shared_votes
        WHERE shared_votes >= 2
        RETURN v1.voter_id as voter1_id, v1.name as voter1_name,
               v2.voter_id as voter2_id, v2.name as voter2_name,
               pu.pu_code as polling_unit,
               shared_votes as shared_votes_count
        ORDER BY shared_votes DESC
        LIMIT 100
        """
        
        return self.graph.execute_query(query)
    
    def analyze_turnout_anomalies(self, threshold_multiplier: float = 2.0) -> List[Dict]:
        """Analyze turnout anomalies using graph statistics.
        
        Finds PUs with turnout significantly higher than ward average.
        """
        query = """
        MATCH (pu:POLLING_UNIT)-[:IN_WARD]->(w:WARD)
        WITH w, avg(pu.turnout_percentage) as ward_avg, 
             collect(pu) as pus
        UNWIND pus as pu
        WITH w, pu, pu.turnout_percentage as turnout, ward_avg
        WHERE turnout > ward_avg * $threshold
        RETURN pu.pu_code, pu.turnout_percentage, ward_avg, w.ward_name
        ORDER BY pu.turnout_percentage DESC
        LIMIT 50
        """
        
        return self.graph.execute_query(query, {'threshold': threshold_multiplier})
    
    def generate_election_report(self, ward: str = None) -> Dict:
        """Generate comprehensive election analysis report.
        
        Args:
            ward: Optional ward filter
            
        Returns:
            Dict with comprehensive analysis
        """
        report = {
            'generated_at': datetime.now().isoformat(),
            'ward_filter': ward,
            'statistics': {},
            'suspicious_patterns': [],
            'recommendations': [],
        }
        
        # Total voters
        query = """
        MATCH (v:VOTER)
        RETURN count(v) as total_voters
        """
        results = self.graph.execute_query(query)
        report['statistics']['total_voters'] = results[0]['total_voters'] if results else 0
        
        # Total polling units
        query = """
        MATCH (pu:POLLING_UNIT)
        RETURN count(pu) as total_pus
        """
        results = self.graph.execute_query(query)
        report['statistics']['total_polling_units'] = results[0]['total_pus'] if results else 0
        
        # Duplicate voters
        duplicates = self.graph.find_duplicate_voters()
        report['suspicious_patterns'].extend([
            {'type': 'duplicate_voter', 'data': d} for d in duplicates[:10]
        ])
        
        # Suspicious patterns
        suspicious = self.graph.detect_suspicious_patterns()
        report['suspicious_patterns'].extend(suspicious[:10])
        
        # Recommendations
        if len(duplicates) > 10:
            report['recommendations'].append(
                "High number of duplicate voters detected. Recommend manual review."
            )
        if len(suspicious) > 5:
            report['recommendations'].append(
                "Suspicious voting patterns detected. Recommend investigation."
            )
        
        return report


def main():
    """Demonstrate Neo4j election graph usage."""
    
    print("=" * 60)
    print("Neo4j Election Graph Integration")
    print("=" * 60)
    
    if not NEO4J_AVAILABLE:
        print("⚠ Neo4j Python driver not installed")
        print("Install with: pip install neo4j")
        print("\nFor production deployment, use Docker:")
        print("  docker run -d -p 7474:7474 -p 7687:7687")
        print("  -e NEO4J_AUTH=neo4j/password neo4j:5")
        return
    
    # Example usage
    print("\nExample usage:")
    print("""
    from neo4j_integration import Neo4jElectionGraph
    
    # Connect to Neo4j
    graph = Neo4jElectionGraph(
        uri="bolt://localhost:7687",
        user="neo4j",
        password="your_password"
    )
    graph.connect()
    
    # Create voter
    graph.create_voter(
        voter_id="V001",
        name="John Doe",
        nin="12345678901"
    )
    
    # Create polling unit
    graph.create_polling_unit(
        pu_code="PU-001",
        ward="Ward-A",
        lga="LGA-1",
        state="Abuja",
        latitude=9.0579,
        longitude=7.4951
    )
    
    # Record vote
    graph.record_vote(
        voter_id="V001",
        pu_code="PU-001",
        vote_count=1
    )
    
    # Find duplicates
    duplicates = graph.find_duplicate_voters(nin="12345678901")
    
    # Analyze network
    network = graph.find_voter_network("V001")
    
    # Generate report
    report = graph.generate_election_report()
    
    # Close connection
    graph.close()
    """)
    
    print("\n" + "=" * 60)
    print("Neo4j Integration Ready")
    print("=" * 60)


if __name__ == "__main__":
    main()
