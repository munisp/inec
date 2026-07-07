"""GNN for Cross-PU Election Validation using PyTorch Geometric.

Implements a Graph Neural Network for validating election results across
polling units (PUs) by learning geographic and statistical patterns.

Usage:
    python train_gnn.py --data-dir datasets/election-results --epochs 100
    python train_gnn.py --pretrained --fine-tune

Model saved to: services/ml-models/gnn/gnn_election_validation.onnx
"""

import os
import argparse
from pathlib import Path
from typing import Dict, List, Tuple, Optional

import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F
from torch_geometric.data import Data, DataLoader
from torch_geometric.nn import GCNConv, GATConv, MessagePassing
from torch_geometric.utils import add_self_loops, degree

# Configuration
MODEL_DIR = Path(__file__).parent
MODEL_DIR.mkdir(parents=True, exist_ok=True)
GNN_MODEL_PATH = MODEL_DIR / "gnn_election_validation.pth"
GNN_ONNX_PATH = MODEL_DIR / "gnn_election_validation.onnx"


class ElectionNode:
    """Represents a polling unit node in the election graph."""
    
    def __init__(
        self,
        pu_code: str,
        latitude: float,
        longitude: float,
        ward: str,
        lga: str,
        vote_count: int = 0,
        turnout_percentage: float = 0.0,
        accredited_voters: int = 0,
        is_anomalous: bool = False,
    ):
        self.pu_code = pu_code
        self.latitude = latitude
        self.longitude = longitude
        self.ward = ward
        self.lga = lga
        self.vote_count = vote_count
        self.turnout_percentage = turnout_percentage
        self.accredited_voters = accredited_voters
        self.is_anomalous = is_anomalous
    
    @property
    def features(self) -> np.ndarray:
        """Extract node features for GNN input."""
        return np.array([
            self.turnout_percentage,
            self.vote_count / max(self.accredited_voters, 1) if self.accredited_voters > 0 else 0.0,
            self.latitude / 10.0,  # Normalize
            self.longitude / 10.0,  # Normalize
        ])


class ElectionGraphBuilder:
    """Builds election validation graphs from PU data."""
    
    def __init__(self, distance_threshold_km: float = 2.0):
        self.distance_threshold_km = distance_threshold_km
    
    @staticmethod
    def haversine_distance(lat1: float, lon1: float, lat2: float, lon2: float) -> float:
        """Calculate distance between two points in km."""
        R = 6371.0  # Earth radius in km
        
        lat1_rad = np.radians(lat1)
        lat2_rad = np.radians(lat2)
        dlat = np.radians(lat2 - lat1)
        dlon = np.radians(lon2 - lon1)
        
        a = np.sin(dlat / 2)**2 + np.cos(lat1_rad) * np.cos(lat2_rad) * np.sin(dlon / 2)**2
        c = 2 * np.arctan2(np.sqrt(a), np.sqrt(1 - a))
        
        return R * c
    
    def build_graph(self, nodes: List[ElectionNode]) -> Data:
        """Build PyTorch Geometric graph from election nodes.
        
        Edge creation strategy:
        1. Same ward → always connected (administrative boundary)
        2. Same LGA → connected (administrative proximity)
        3. Haversine distance < threshold → connected (geographic proximity)
        """
        num_nodes = len(nodes)
        
        if num_nodes == 0:
            return None
        
        # Node features
        x = np.array([node.features for node in nodes], dtype=np.float32)
        
        # Edge indices (COO format)
        edge_index = []
        
        for i, node_a in enumerate(nodes):
            for j, node_b in enumerate(nodes):
                if i >= j:
                    continue
                
                # Connect if same ward
                if node_a.ward == node_b.ward:
                    edge_index.extend([i, j])
                    continue
                
                # Connect if same LGA
                if node_a.lga == node_b.lga:
                    edge_index.extend([i, j])
                    continue
                
                # Connect if within distance threshold
                dist = self.haversine_distance(
                    node_a.latitude, node_a.longitude,
                    node_b.latitude, node_b.longitude,
                )
                
                if dist < self.distance_threshold_km:
                    edge_index.extend([i, j])
        
        if len(edge_index) == 0:
            return None
        
        edge_index = np.array(edge_index, dtype=np.int64).reshape(2, -1)
        edge_index = torch.from_numpy(edge_index).long()
        
        # Add self-loops for GCN
        edge_index, _ = add_self_loops(edge_index, num_nodes=num_nodes)
        
        # Labels (1 if anomalous, 0 if normal)
        y = torch.tensor([1.0 if node.is_anomalous else 0.0 for node in nodes], dtype=torch.float)
        
        return Data(x=torch.from_numpy(x), edge_index=edge_index, y=y)


class GNNAnomalyDetection(nn.Module):
    """GNN for election anomaly detection using message passing.
    
    Architecture:
    - GCN layers for feature propagation
    - Attention mechanism for neighborhood weighting
    - Binary classification for anomaly detection
    """
    
    def __init__(self, num_features: int, hidden_channels: int = 64, num_classes: int = 1):
        super().__init__()
        
        self.conv1 = GCNConv(num_features, hidden_channels)
        self.conv2 = GCNConv(hidden_channels, hidden_channels)
        self.conv3 = GCNConv(hidden_channels, hidden_channels // 2)
        
        self.attention = nn.Sequential(
            nn.Linear(hidden_channels // 2, 16),
            nn.ReLU(),
            nn.Linear(16, 1),
            nn.Sigmoid(),
        )
        
        self.classifier = nn.Sequential(
            nn.Linear(hidden_channels // 2, 32),
            nn.ReLU(),
            nn.Dropout(0.3),
            nn.Linear(32, num_classes),
        )
        
        self.bn1 = nn.BatchNorm1d(hidden_channels)
        self.bn2 = nn.BatchNorm1d(hidden_channels)
    
    def forward(self, data) -> torch.Tensor:
        """Forward pass with attention mechanism.
        
        Args:
            data: PyTorch Geometric Data object
            
        Returns:
            Anomaly scores for each node
        """
        x, edge_index = data.x, data.edge_index
        
        # GCN layers with batch normalization
        x = self.conv1(x, edge_index)
        x = self.bn1(x)
        x = torch.relu(x)
        x = F.dropout(x, p=0.3, training=self.training)
        
        x = self.conv2(x, edge_index)
        x = self.bn2(x)
        x = torch.relu(x)
        x = F.dropout(x, p=0.3, training=self.training)
        
        x = self.conv3(x, edge_index)
        x = torch.relu(x)
        
        # Attention mechanism
        attention_weights = self.attention(x)
        x = x * attention_weights
        
        # Classification
        x = self.classifier(x)
        
        return x


class EnhancedGNNAnomalyDetection(nn.Module):
    """Enhanced GNN with Graph Attention Networks (GAT).
    
    Uses multi-head attention to learn important neighborhood relationships,
    providing better anomaly detection performance.
    """
    
    def __init__(
        self,
        num_features: int,
        hidden_channels: int = 64,
        num_heads: int = 4,
        num_classes: int = 1,
    ):
        super().__init__()
        
        self.gat1 = GATConv(
            num_features,
            hidden_channels // num_heads,
            heads=num_heads,
            dropout=0.3,
        )
        self.gat2 = GATConv(
            hidden_channels,
            hidden_channels // num_heads,
            heads=num_heads,
            dropout=0.3,
        )
        self.gat3 = GATConv(
            hidden_channels,
            hidden_channels // 2,
            heads=num_heads,
            dropout=0.3,
        )
        
        self.classifier = nn.Sequential(
            nn.Linear(hidden_channels // 2, 32),
            nn.ReLU(),
            nn.Dropout(0.3),
            nn.Linear(32, num_classes),
        )
    
    def forward(self, data) -> torch.Tensor:
        """Forward pass with GAT attention."""
        x, edge_index = data.x, data.edge_index
        
        x = self.gat1(x, edge_index)
        x = torch.relu(x)
        x = F.dropout(x, p=0.3, training=self.training)
        
        x = self.gat2(x, edge_index)
        x = torch.relu(x)
        x = F.dropout(x, p=0.3, training=self.training)
        
        x = self.gat3(x, edge_index)
        x = torch.relu(x)
        
        x = self.classifier(x)
        
        return x


def train_gnn_model(
    model: nn.Module,
    train_loader: DataLoader,
    val_loader: DataLoader,
    device: torch.device,
    epochs: int = 100,
    learning_rate: float = 1e-3,
) -> Dict:
    """Train GNN model for anomaly detection."""
    
    model = model.to(device)
    criterion = nn.BCEWithLogitsLoss()
    optimizer = torch.optim.Adam(model.parameters(), lr=learning_rate, weight_decay=1e-5)
    scheduler = torch.optim.lr_scheduler.CosineAnnealingLR(optimizer, T_max=epochs)
    
    best_val_auc = 0.0
    training_history = []
    
    for epoch in range(epochs):
        # Training phase
        model.train()
        train_loss = 0.0
        
        for data in train_loader:
            data = data.to(device)
            
            optimizer.zero_grad()
            outputs = model(data).squeeze()
            loss = criterion(outputs, data.y)
            loss.backward()
            optimizer.step()
            
            train_loss += loss.item()
        
        # Validation phase
        model.eval()
        val_loss = 0.0
        all_preds, all_labels = [], []
        
        with torch.no_grad():
            for data in val_loader:
                data = data.to(device)
                outputs = model(data).squeeze()
                loss = criterion(outputs, data.y)
                
                val_loss += loss.item()
                
                probs = torch.sigmoid(outputs)
                all_preds.extend(probs.cpu().numpy())
                all_labels.extend(data.y.cpu().numpy())
        
        # Calculate AUC
        from sklearn.metrics import roc_auc_score
        try:
            val_auc = roc_auc_score(all_labels, all_preds)
        except ValueError:
            val_auc = 0.0
        
        scheduler.step()
        
        history = {
            'epoch': epoch + 1,
            'train_loss': train_loss / len(train_loader),
            'val_loss': val_loss / len(val_loader),
            'val_auc': val_auc,
            'learning_rate': scheduler.get_last_lr()[0],
        }
        training_history.append(history)
        
        print(f"Epoch [{epoch+1}/{epochs}] "
              f"Train Loss: {history['train_loss']:.4f} | "
              f"Val Loss: {history['val_loss']:.4f} | "
              f"Val AUC: {history['val_auc']:.4f}")
        
        # Save best model
        if val_auc > best_val_auc:
            best_val_auc = val_auc
            torch.save({
                'epoch': epoch + 1,
                'model_state_dict': model.state_dict(),
                'optimizer_state_dict': optimizer.state_dict(),
                'val_auc': val_auc,
            }, GNN_MODEL_PATH)
            print(f"✓ Saved best model (AUC: {val_auc:.4f})")
    
    return {
        'best_val_auc': best_val_auc,
        'training_history': training_history,
    }


class GNNPredictor:
    """Production GNN predictor for election anomaly detection."""
    
    def __init__(self, model_path: str = None):
        self.device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
        
        if model_path is None:
            model_path = str(GNN_MODEL_PATH)
        
        checkpoint = torch.load(model_path, map_location=self.device)
        
        self.model = EnhancedGNNAnomalyDetection(
            num_features=4,
            hidden_channels=64,
            num_heads=4,
        )
        self.model.load_state_dict(checkpoint['model_state_dict'])
        self.model = self.model.to(self.device)
        self.model.eval()
        
        self.ready = True
    
    def predict(self, graph_data) -> Dict:
        """Predict anomalies for graph nodes.
        
        Args:
            graph_data: PyTorch Geometric Data object
            
        Returns:
            Dict with prediction results per node
        """
        graph_data = graph_data.to(self.device)
        
        with torch.no_grad():
            outputs = self.model(graph_data).squeeze()
            probs = torch.sigmoid(outputs)
        
        predictions = []
        for i, prob in enumerate(probs.cpu().numpy()):
            predictions.append({
                'node_index': i,
                'is_anomalous': bool(prob > 0.5),
                'anomaly_score': float(prob),
                'confidence': float(max(prob, 1.0 - prob)),
            })
        
        return {
            'predictions': predictions,
            'total_anomalies': sum(1 for p in predictions if p['is_anomalous']),
        }


def create_synthetic_election_data(
    num_nodes: int = 1000,
    anomaly_rate: float = 0.05,
) -> Tuple[List[ElectionNode], ElectionGraphBuilder]:
    """Create synthetic election data for training."""
    
    print(f"Creating synthetic election data: {num_nodes} PUs")
    
    nodes = []
    base_latitude = 9.0579  # Abuja
    base_longitude = 7.4951
    
    for i in range(num_nodes):
        # Generate PU with geographic clustering
        cluster_id = i % 20  # 20 clusters
        cluster_lat = base_latitude + (cluster_id % 5) * 0.1
        cluster_lon = base_longitude + (cluster_id // 5) * 0.1
        
        latitude = cluster_lat + np.random.normal(0, 0.02)
        longitude = cluster_lon + np.random.normal(0, 0.02)
        
        ward = f"Ward-{cluster_id}"
        lga = f"LGA-{cluster_id // 4}"
        
        accredited_voters = np.random.randint(500, 2000)
        
        # Normal vote distribution
        vote_count = int(accredited_voters * np.random.normal(0.65, 0.1))
        vote_count = max(0, min(vote_count, accredited_voters))
        
        is_anomalous = np.random.random() < anomaly_rate
        
        if is_anomalous:
            # Anomalous: unusual vote patterns
            if np.random.random() < 0.5:
                # Overvoting
                vote_count = int(accredited_voters * np.random.uniform(1.1, 1.5))
            else:
                # Underreporting
                vote_count = int(accredited_voters * np.random.uniform(0.01, 0.1))
        
        turnout_percentage = (vote_count / accredited_voters * 100) if accredited_voters > 0 else 0.0
        
        node = ElectionNode(
            pu_code=f"PU-{i:05d}",
            latitude=latitude,
            longitude=longitude,
            ward=ward,
            lga=lga,
            vote_count=vote_count,
            turnout_percentage=turnout_percentage,
            accredited_voters=accredited_voters,
            is_anomalous=is_anomalous,
        )
        nodes.append(node)
    
    return nodes, ElectionGraphBuilder()


def main():
    """Main training pipeline."""
    parser = argparse.ArgumentParser(description='Train GNN for Election Validation')
    parser.add_argument('--num-nodes', type=int, default=1000, help='Number of PU nodes')
    parser.add_argument('--anomaly-rate', type=float, default=0.05, help='Anomaly rate in data')
    parser.add_argument('--epochs', type=int, default=100, help='Training epochs')
    parser.add_argument('--batch-size', type=int, default=32, help='Batch size')
    parser.add_argument('--learning-rate', type=float, default=1e-3, help='Learning rate')
    parser.add_argument('--pretrained', action='store_true', help='Use pretrained model')
    parser.add_argument('--model-type', type=str, default='gat',
                       choices=['gcn', 'gat'], help='GNN architecture')
    
    args = parser.parse_args()
    
    device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
    print(f"Using device: {device}")
    
    # Create synthetic data
    nodes, graph_builder = create_synthetic_election_data(
        num_nodes=args.num_nodes,
        anomaly_rate=args.anomaly_rate,
    )
    
    # Build graphs
    graph = graph_builder.build_graph(nodes)
    
    if graph is None:
        print("Error: Could not build graph")
        return
    
    # Split data (train/val/test)
    num_nodes = len(nodes)
    indices = torch.randperm(num_nodes)
    
    train_mask = indices[:int(0.8 * num_nodes)]
    val_mask = indices[int(0.8 * num_nodes):int(0.9 * num_nodes)]
    test_mask = indices[int(0.9 * num_nodes):]
    
    # Create separate graphs for each split (simplified)
    train_data = Data(
        x=graph.x[train_mask],
        edge_index=graph.edge_index,
        y=graph.y[train_mask],
    )
    val_data = Data(
        x=graph.x[val_mask],
        edge_index=graph.edge_index,
        y=graph.y[val_mask],
    )
    
    train_loader = DataLoader([train_data], batch_size=1, shuffle=False)
    val_loader = DataLoader([val_data], batch_size=1, shuffle=False)
    
    # Initialize model
    if args.model_type == 'gat':
        model = EnhancedGNNAnomalyDetection(
            num_features=4,
            hidden_channels=64,
            num_heads=4,
        )
    else:
        model = GNNAnomalyDetection(
            num_features=4,
            hidden_channels=64,
        )
    
    # Load pretrained if available
    if args.pretrained and GNN_MODEL_PATH.exists():
        checkpoint = torch.load(GNN_MODEL_PATH, map_location=device)
        model.load_state_dict(checkpoint['model_state_dict'])
        print(f"Loaded pretrained model (AUC: {checkpoint['val_auc']:.4f})")
    
    # Train model
    results = train_gnn_model(
        model=model,
        train_loader=train_loader,
        val_loader=val_loader,
        device=device,
        epochs=args.epochs,
        learning_rate=args.learning_rate,
    )
    
    print(f"\n✓ Training complete!")
    print(f"  Best Validation AUC: {results['best_val_auc']:.4f}")
    
    # Save final model
    torch.save({
        'epoch': args.epochs,
        'model_state_dict': model.state_dict(),
        'optimizer_state_dict': None,
        'val_auc': results['best_val_auc'],
    }, GNN_MODEL_PATH)
    print(f"✓ Model saved to {GNN_MODEL_PATH}")


if __name__ == "__main__":
    main()
