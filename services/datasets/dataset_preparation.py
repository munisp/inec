"""Dataset Preparation Scripts for Election AI/ML Models.

Provides utilities for preparing, augmenting, and validating training datasets
for all election AI/ML models including:
- Biometric datasets (face, fingerprint, iris, PAD)
- Election results datasets
- Video ballot datasets
- Financial transaction datasets

Usage:
    from dataset_preparation import BiometricDatasetPrep, ElectionDatasetPrep
    prep = BiometricDatasetPrep(input_dir="data/raw", output_dir="data/processed")
    prep.prepare_face_dataset()
    prep.prepare_fingerprint_dataset()
    
    election_prep = ElectionDatasetPrep(input_dir="data/election")
    election_prep.prepare_training_data()
"""

import os
import json
import hashlib
import random
from pathlib import Path
from typing import Dict, List, Optional, Tuple, Any
from datetime import datetime
from dataclasses import dataclass, field

import numpy as np
import cv2
from PIL import Image


@dataclass
class DatasetStats:
    """Statistics for a prepared dataset."""
    dataset_name: str
    total_samples: int = 0
    train_samples: int = 0
    val_samples: int = 0
    test_samples: int = 0
    classes: Dict[str, int] = field(default_factory=dict)
    mean_values: Dict[str, float] = field(default_factory=dict)
    std_values: Dict[str, float] = field(default_factory=dict)
    metadata: Dict = field(default_factory=dict)
    prepared_at: str = field(default_factory=lambda: datetime.now().isoformat())


class DatasetPreparationBase:
    """Base class for dataset preparation."""
    
    def __init__(
        self,
        input_dir: str = "data/raw",
        output_dir: str = "data/processed",
        random_seed: int = 42,
    ):
        self.input_dir = Path(input_dir)
        self.output_dir = Path(output_dir)
        self.random_seed = random_seed
        self.rng = np.random.RandomState(random_seed)
        
        # Create output directories
        os.makedirs(self.output_dir, exist_ok=True)
    
    def split_dataset(
        self,
        samples: List,
        train_ratio: float = 0.8,
        val_ratio: float = 0.1,
        test_ratio: float = 0.1,
    ) -> Tuple[List, List, List]:
        """Split dataset into train/val/test splits.
        
        Args:
            samples: List of samples
            train_ratio: Training split ratio
            val_ratio: Validation split ratio
            test_ratio: Test split ratio
            
        Returns:
            Tuple of (train, val, test) splits
        """
        assert abs(train_ratio + val_ratio + test_ratio - 1.0) < 1e-6, \
            "Ratios must sum to 1.0"
        
        # Shuffle samples
        indices = list(range(len(samples)))
        self.rng.shuffle(indices)
        
        # Calculate split points
        train_end = int(len(samples) * train_ratio)
        val_end = int(len(samples) * (train_ratio + val_ratio))
        
        train_indices = indices[:train_end]
        val_indices = indices[train_end:val_end]
        test_indices = indices[val_end:]
        
        train = [samples[i] for i in train_indices]
        val = [samples[i] for i in val_indices]
        test = [samples[i] for i in test_indices]
        
        print(f"  Split: {len(train)} train, {len(val)} val, {len(test)} test")
        
        return train, val, test
    
    def calculate_statistics(self, samples: List[np.ndarray]) -> Dict:
        """Calculate dataset statistics.
        
        Args:
            samples: List of sample arrays
            
        Returns:
            Dict with mean and std statistics
        """
        all_samples = np.array(samples)
        mean = np.mean(all_samples, axis=0).tolist()
        std = np.std(all_samples, axis=0).tolist()
        
        return {
            'mean': mean,
            'std': std,
            'shape': list(all_samples.shape),
            'dtype': str(all_samples.dtype),
        }
    
    def save_dataset_manifest(
        self,
        dataset_name: str,
        stats: DatasetStats,
        output_path: str = None,
    ):
        """Save dataset manifest with metadata."""
        if output_path is None:
            output_path = str(self.output_dir / f"{dataset_name}_manifest.json")
        
        manifest = {
            'dataset_name': dataset_name,
            'stats': {
                'total_samples': stats.total_samples,
                'train_samples': stats.train_samples,
                'val_samples': stats.val_samples,
                'test_samples': stats.test_samples,
                'classes': stats.classes,
                'mean_values': stats.mean_values,
                'std_values': stats.std_values,
                'metadata': stats.metadata,
                'prepared_at': stats.prepared_at,
            },
            'file_paths': {},
        }
        
        with open(output_path, 'w') as f:
            json.dump(manifest, f, indent=2)
        
        print(f"✓ Saved dataset manifest: {output_path}")
        
        return manifest


class BiometricDatasetPrep(DatasetPreparationBase):
    """Prepare biometric datasets for training."""
    
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.face_dir = self.output_dir / "faces"
        self.fingerprint_dir = self.output_dir / "fingerprints"
        self.iris_dir = self.output_dir / "irises"
        self.pad_dir = self.output_dir / "pad"
        
        # Create directories
        for d in [self.face_dir, self.fingerprint_dir, self.iris_dir, self.pad_dir]:
            os.makedirs(d, exist_ok=True)
    
    def prepare_face_dataset(
        self,
        input_dir: str = None,
        target_size: Tuple[int, int] = (112, 112),
        train_ratio: float = 0.8,
        val_ratio: float = 0.1,
    ) -> DatasetStats:
        """Prepare face recognition dataset.
        
        Expected input format:
        input_dir/
        ├── class_0001/
        │   ├── img001.jpg
        │   └── ...
        ├── class_0002/
        │   └── ...
        
        Output format:
        output_dir/
        ├── faces/
        │   ├── train/
        │   │   ├── class_0001/
        │   │   └── ...
        │   ├── val/
        │   │   └── ...
        │   └── test/
        │       └── ...
        """
        print("\nPreparing face dataset...")
        
        if input_dir is None:
            input_dir = str(self.input_dir / "faces")
        
        input_path = Path(input_dir)
        if not input_path.exists():
            print(f"⚠ Input directory not found: {input_dir}")
            print("Generating synthetic face dataset for demonstration...")
            input_path = self._generate_synthetic_faces(input_dir, num_classes=10, samples_per_class=50)
        
        # Collect samples
        samples = []
        class_to_idx = {}
        idx_to_class = {}
        
        for idx, class_dir in enumerate(sorted(input_path.iterdir())):
            if class_dir.is_dir():
                class_name = class_dir.name
                class_to_idx[class_name] = idx
                idx_to_class[idx] = class_name
                
                for img_path in class_dir.glob("*.jpg"):
                    img = cv2.imread(str(img_path))
                    if img is not None:
                        # Resize to target size
                        img_resized = cv2.resize(img, target_size)
                        samples.append({
                            'image': img_resized,
                            'label': idx,
                            'class_name': class_name,
                            'source': str(img_path),
                        })
        
        # Split dataset
        train, val, test = self.split_dataset(samples, train_ratio, val_ratio)
        
        # Save samples
        self._save_samples(train, self.face_dir / "train", target_size)
        self._save_samples(val, self.face_dir / "val", target_size)
        self._save_samples(test, self.face_dir / "test", target_size)
        
        # Calculate statistics
        sample_images = [s['image'] for s in samples]
        stats_dict = self.calculate_statistics(sample_images)
        
        # Create classes dict
        classes = {name: sum(1 for s in samples if s['label'] == idx)
                  for name, idx in class_to_idx.items()}
        
        stats = DatasetStats(
            dataset_name="faces",
            total_samples=len(samples),
            train_samples=len(train),
            val_samples=len(val),
            test_samples=len(test),
            classes=classes,
            mean_values={f'channel_{i}': stats_dict['mean'][i] for i in range(3)},
            std_values={f'channel_{i}': stats_dict['std'][i] for i in range(3)},
            metadata={
                'target_size': list(target_size),
                'num_classes': len(class_to_idx),
                'augmentation': ['horizontal_flip', 'rotation', 'color_jitter'],
            },
        )
        
        self.save_dataset_manifest("faces", stats)
        
        print(f"✓ Face dataset prepared: {len(samples)} samples")
        
        return stats
    
    def prepare_fingerprint_dataset(
        self,
        input_dir: str = None,
        train_ratio: float = 0.8,
        val_ratio: float = 0.1,
    ) -> DatasetStats:
        """Prepare fingerprint dataset.
        
        Expected input format:
        input_dir/
        ├── subject_0001/
        │   ├── print_001.jpg
        │   └── ...
        ├── subject_0002/
        │   └── ...
        """
        print("\nPreparing fingerprint dataset...")
        
        if input_dir is None:
            input_dir = str(self.input_dir / "fingerprints")
        
        input_path = Path(input_dir)
        if not input_path.exists():
            print(f"⚠ Input directory not found: {input_dir}")
            print("Generating synthetic fingerprint dataset...")
            input_path = self._generate_synthetic_fingerprints(input_dir, num_subjects=20, prints_per_subject=5)
        
        # Collect samples
        samples = []
        class_to_idx = {}
        
        for idx, subject_dir in enumerate(sorted(input_path.iterdir())):
            if subject_dir.is_dir():
                class_to_idx[subject_dir.name] = idx
                
                for img_path in subject_dir.glob("*.jpg"):
                    img = cv2.imread(str(img_path))
                    if img is not None:
                        img_resized = cv2.resize(img, (128, 128))
                        samples.append({
                            'image': img_resized,
                            'label': idx,
                            'subject': subject_dir.name,
                            'source': str(img_path),
                        })
        
        # Split dataset
        train, val, test = self.split_dataset(samples, train_ratio, val_ratio)
        
        # Save samples
        self._save_samples(train, self.fingerprint_dir / "train")
        self._save_samples(val, self.fingerprint_dir / "val")
        self._save_samples(test, self.fingerprint_dir / "test")
        
        classes = {name: sum(1 for s in samples if s['label'] == idx)
                  for name, idx in class_to_idx.items()}
        
        stats = DatasetStats(
            dataset_name="fingerprints",
            total_samples=len(samples),
            train_samples=len(train),
            val_samples=len(val),
            test_samples=len(test),
            classes=classes,
            metadata={
                'target_size': [128, 128],
                'num_subjects': len(class_to_idx),
                'augmentation': ['rotation', 'elastic_transform', 'noise'],
            },
        )
        
        self.save_dataset_manifest("fingerprints", stats)
        
        print(f"✓ Fingerprint dataset prepared: {len(samples)} samples")
        
        return stats
    
    def prepare_pad_dataset(
        self,
        input_dir: str = None,
        train_ratio: float = 0.8,
        val_ratio: float = 0.1,
    ) -> DatasetStats:
        """Prepare PAD (Presentation Attack Detection) dataset.
        
        Expected input format:
        input_dir/
        ├── real/
        │   ├── subject_0001/
        │   │   └── ...
        ├── spoof/
        │   ├── print_0001/
        │   │   └── ...
        │   ├── replay_0001/
        │   │   └── ...
        """
        print("\nPreparing PAD dataset...")
        
        if input_dir is None:
            input_dir = str(self.input_dir / "pad")
        
        input_path = Path(input_dir)
        if not input_path.exists():
            print(f"⚠ Input directory not found: {input_dir}")
            print("Generating synthetic PAD dataset...")
            input_path = self._generate_synthetic_pad_dataset(input_dir)
        
        # Collect samples
        samples = []
        
        # Real samples
        real_dir = input_path / "real"
        if real_dir.exists():
            for video_dir in real_dir.iterdir():
                if video_dir.is_dir():
                    for img_path in video_dir.glob("*.jpg"):
                        img = cv2.imread(str(img_path))
                        if img is not None:
                            img_resized = cv2.resize(img, (128, 128))
                            samples.append({
                                'image': img_resized,
                                'label': 0,  # Real
                                'type': 'real',
                                'source': str(img_path),
                            })
        
        # Spoof samples
        spoof_dir = input_path / "spoof"
        if spoof_dir.exists():
            for attack_type_dir in spoof_dir.iterdir():
                if attack_type_dir.is_dir():
                    for img_path in attack_type_dir.glob("*.jpg"):
                        img = cv2.imread(str(img_path))
                        if img is not None:
                            img_resized = cv2.resize(img, (128, 128))
                            samples.append({
                                'image': img_resized,
                                'label': 1,  # Spoof
                                'type': attack_type_dir.name,
                                'source': str(img_path),
                            })
        
        # Split dataset
        train, val, test = self.split_dataset(samples, train_ratio, val_ratio)
        
        # Save samples
        self._save_samples(train, self.pad_dir / "train")
        self._save_samples(val, self.pad_dir / "val")
        self._save_samples(test, self.pad_dir / "test")
        
        # Calculate statistics
        sample_images = [s['image'] for s in samples]
        stats_dict = self.calculate_statistics(sample_images)
        
        classes = {
            'real': sum(1 for s in samples if s['label'] == 0),
            'spoof': sum(1 for s in samples if s['label'] == 1),
        }
        
        stats = DatasetStats(
            dataset_name="pad",
            total_samples=len(samples),
            train_samples=len(train),
            val_samples=len(val),
            test_samples=len(test),
            classes=classes,
            mean_values={f'channel_{i}': stats_dict['mean'][i] for i in range(3)},
            std_values={f'channel_{i}': stats_dict['std'][i] for i in range(3)},
            metadata={
                'target_size': [128, 128],
                'attack_types': ['print', 'replay', 'mask'],
                'augmentation': ['gaussian_noise', 'blur', 'rotation'],
            },
        )
        
        self.save_dataset_manifest("pad", stats)
        
        print(f"✓ PAD dataset prepared: {len(samples)} samples")
        
        return stats
    
    def _save_samples(self, samples: List[Dict], output_dir: Path, target_size: Tuple[int, int] = None):
        """Save samples to disk."""
        os.makedirs(output_dir, exist_ok=True)
        
        for idx, sample in enumerate(samples):
            img = sample['image']
            
            if target_size:
                img = cv2.resize(img, target_size)
            
            output_path = output_dir / f"sample_{idx:06d}.jpg"
            cv2.imwrite(str(output_path), img)
    
    def _generate_synthetic_faces(
        self,
        output_dir: str,
        num_classes: int = 10,
        samples_per_class: int = 50,
    ) -> Path:
        """Generate synthetic face dataset for testing."""
        output_path = Path(output_dir)
        
        for class_idx in range(num_classes):
            class_dir = output_path / f"class_{class_idx:04d}"
            os.makedirs(class_dir, exist_ok=True)
            
            for sample_idx in range(samples_per_class):
                # Generate random face-like image
                img = np.random.randint(50, 200, (112, 112, 3), dtype=np.uint8)
                cv2.imwrite(str(class_dir / f"face_{sample_idx:04d}.jpg"), img)
        
        return output_path
    
    def _generate_synthetic_fingerprints(
        self,
        output_dir: str,
        num_subjects: int = 20,
        prints_per_subject: int = 5,
    ) -> Path:
        """Generate synthetic fingerprint dataset."""
        output_path = Path(output_dir)
        
        for subject_idx in range(num_subjects):
            subject_dir = output_path / f"subject_{subject_idx:04d}"
            os.makedirs(subject_dir, exist_ok=True)
            
            for print_idx in range(prints_per_subject):
                # Generate random fingerprint-like image
                img = np.random.randint(0, 255, (128, 128), dtype=np.uint8)
                cv2.imwrite(str(subject_dir / f"print_{print_idx:04d}.jpg"), img)
        
        return output_path
    
    def _generate_synthetic_pad_dataset(self, output_dir: str) -> Path:
        """Generate synthetic PAD dataset."""
        output_path = Path(output_dir)
        
        # Real samples
        real_dir = output_path / "real" / "video_0001"
        os.makedirs(real_dir, exist_ok=True)
        for i in range(50):
            img = np.random.randint(100, 200, (128, 128, 3), dtype=np.uint8)
            cv2.imwrite(str(real_dir / f"real_{i:04d}.jpg"), img)
        
        # Spoof samples
        spoof_dir = output_path / "spoof"
        for attack_type in ['print', 'replay', 'mask']:
            attack_dir = spoof_dir / attack_type
            os.makedirs(attack_dir, exist_ok=True)
            for i in range(25):
                img = np.random.randint(50, 150, (128, 128, 3), dtype=np.uint8)
                cv2.imwrite(str(attack_dir / f"spoof_{i:04d}.jpg"), img)
        
        return output_path


class ElectionDatasetPrep(DatasetPreparationBase):
    """Prepare election results datasets for training."""
    
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.election_dir = self.output_dir / "election"
        os.makedirs(self.election_dir, exist_ok=True)
    
    def prepare_training_data(
        self,
        input_dir: str = None,
        output_path: str = None,
    ) -> DatasetStats:
        """Prepare election results training dataset.
        
        Creates structured dataset with features for fraud detection,
        anomaly detection, and GNN training.
        """
        print("\nPreparing election training dataset...")
        
        if input_dir is None:
            input_dir = str(self.input_dir / "election")
        
        input_path = Path(input_dir)
        
        # Generate synthetic election data
        samples = self._generate_synthetic_election_data(num_samples=10000)
        
        # Split dataset
        train, val, test = self.split_dataset(samples, train_ratio=0.8, val_ratio=0.1)
        
        # Save as JSON
        if output_path is None:
            output_path = str(self.election_dir / "election_data.json")
        
        dataset = {
            'train': train,
            'val': val,
            'test': test,
            'metadata': {
                'num_features': len(train[0]['features']) if train else 0,
                'feature_names': self._get_feature_names(),
                'target_distribution': {
                    'normal': sum(1 for s in samples if s['label'] == 0),
                    'anomalous': sum(1 for s in samples if s['label'] == 1),
                },
            },
        }
        
        with open(output_path, 'w') as f:
            json.dump(dataset, f, indent=2, default=str)
        
        stats = DatasetStats(
            dataset_name="election",
            total_samples=len(samples),
            train_samples=len(train),
            val_samples=len(val),
            test_samples=len(test),
            classes={
                'normal': sum(1 for s in samples if s['label'] == 0),
                'anomalous': sum(1 for s in samples if s['label'] == 1),
            },
            metadata={
                'num_features': len(self._get_feature_names()),
                'feature_names': self._get_feature_names(),
            },
        )
        
        self.save_dataset_manifest("election", stats, output_path.replace('.json', '_manifest.json'))
        
        print(f"✓ Election dataset prepared: {len(samples)} samples")
        
        return stats
    
    def prepare_gnn_training_data(
        self,
        num_nodes: int = 1000,
        output_path: str = None,
    ) -> DatasetStats:
        """Prepare GNN training data with graph structure.
        
        Returns:
            DatasetStats with graph information
        """
        print("\nPreparing GNN training dataset...")
        
        # Generate synthetic graph data
        nodes = []
        edges = []
        
        base_latitude = 9.0579  # Abuja
        base_longitude = 7.4951
        
        for i in range(num_nodes):
            # Geographic clustering
            cluster_id = i % 20
            cluster_lat = base_latitude + (cluster_id % 5) * 0.1
            cluster_lon = base_longitude + (cluster_id // 5) * 0.1
            
            latitude = cluster_lat + np.random.normal(0, 0.02)
            longitude = cluster_lon + np.random.normal(0, 0.02)
            
            node = {
                'node_id': i,
                'features': [
                    np.random.uniform(50, 100),  # Turnout %
                    np.random.uniform(0, 1),     # Vote ratio
                    latitude / 10.0,
                    longitude / 10.0,
                ],
                'label': np.random.random() < 0.05,  # 5% anomalous
                'ward': f"Ward-{cluster_id}",
                'lga': f"LGA-{cluster_id // 4}",
            }
            nodes.append(node)
        
        # Create edges based on geographic proximity
        for i in range(num_nodes):
            for j in range(i + 1, num_nodes):
                # Connect if same ward or close
                if nodes[i]['ward'] == nodes[j]['ward']:
                    edges.append([i, j])
                else:
                    dist = np.sqrt(
                        (nodes[i]['features'][2] - nodes[j]['features'][2])**2 +
                        (nodes[i]['features'][3] - nodes[j]['features'][3])**2
                    )
                    if dist < 0.05:  # Within ~5km
                        edges.append([i, j])
        
        # Save graph data
        if output_path is None:
            output_path = str(self.election_dir / "gnn_graph.json")
        
        graph_data = {
            'nodes': nodes,
            'edges': edges,
            'metadata': {
                'num_nodes': len(nodes),
                'num_edges': len(edges),
                'features_per_node': len(nodes[0]['features']),
            },
        }
        
        with open(output_path, 'w') as f:
            json.dump(graph_data, f, indent=2)
        
        stats = DatasetStats(
            dataset_name="gnn_graph",
            total_samples=len(nodes),
            metadata={
                'num_nodes': len(nodes),
                'num_edges': len(edges),
                'feature_dim': len(nodes[0]['features']),
            },
        )
        
        self.save_dataset_manifest("gnn_graph", stats, output_path.replace('.json', '_manifest.json'))
        
        print(f"✓ GNN dataset prepared: {len(nodes)} nodes, {len(edges)} edges")
        
        return stats
    
    def _generate_synthetic_election_data(self, num_samples: int = 10000) -> List[Dict]:
        """Generate synthetic election data."""
        np.random.seed(42)
        
        samples = []
        
        for i in range(num_samples):
            accredited_voters = np.random.randint(500, 2000)
            
            # Normal distribution
            if np.random.random() < 0.9:
                votes_cast = int(accredited_voters * np.random.normal(0.65, 0.1))
                is_anomalous = False
            else:
                # Anomalous
                if np.random.random() < 0.5:
                    votes_cast = int(accredited_voters * np.random.uniform(1.1, 1.5))
                else:
                    votes_cast = int(accredited_voters * np.random.uniform(0.01, 0.1))
                is_anomalous = True
            
            votes_cast = max(0, min(votes_cast, accredited_voters))
            
            sample = {
                'pu_code': f"PU-{i:05d}",
                'features': [
                    votes_cast / max(accredited_voters, 1),
                    np.random.normal(0.65, 0.1),
                    np.random.normal(9.0579, 0.05),
                    np.random.normal(7.4951, 0.05),
                ],
                'label': 1 if is_anomalous else 0,
                'accredited_voters': accredited_voters,
                'votes_cast': votes_cast,
            }
            samples.append(sample)
        
        return samples
    
    def _get_feature_names(self) -> List[str]:
        """Get feature names for election dataset."""
        return [
            'vote_ratio',
            'turnout_percentage',
            'latitude',
            'longitude',
        ]


def main():
    """Run all dataset preparation."""
    print("=" * 60)
    print("Election AI/ML Dataset Preparation")
    print("=" * 60)
    
    # Prepare biometric datasets
    biometric_prep = BiometricDatasetPrep(
        input_dir="data/raw/biometric",
        output_dir="data/processed/biometric",
    )
    
    face_stats = biometric_prep.prepare_face_dataset()
    fingerprint_stats = biometric_prep.prepare_fingerprint_dataset()
    pad_stats = biometric_prep.prepare_pad_dataset()
    
    # Prepare election datasets
    election_prep = ElectionDatasetPrep(
        input_dir="data/raw/election",
        output_dir="data/processed/election",
    )
    
    election_stats = election_prep.prepare_training_data()
    gnn_stats = election_prep.prepare_gnn_training_data()
    
    print("\n" + "=" * 60)
    print("Dataset Preparation Complete")
    print("=" * 60)
    
    print(f"\nFace dataset: {face_stats.total_samples} samples")
    print(f"Fingerprint dataset: {fingerprint_stats.total_samples} samples")
    print(f"PAD dataset: {pad_stats.total_samples} samples")
    print(f"Election dataset: {election_stats.total_samples} samples")
    print(f"GNN dataset: {gnn_stats.total_samples} nodes")


if __name__ == "__main__":
    main()
