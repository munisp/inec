"""ArcFace Face Embedding Model Training.

Trains an ArcFace model for face recognition/embedding extraction.
ArcFace achieves state-of-the-art results on LFW, CFP-FP, and AgeDB benchmarks.

Usage:
    python train_arcface.py --dataset casia-webface --epochs 50
    python train_arcface.py --pretrained-insightface --fine-tune

Model saved to: services/biometric-python/models/arcface_embedding.onnx
"""

import os
import argparse
from pathlib import Path
from typing import Dict, List, Tuple

import numpy as np
import onnx
import onnxruntime as ort
from PIL import Image
import torch
import torch.nn as nn
import torch.nn.functional as F
import torch.optim as optim
from torch.utils.data import DataLoader, Dataset

# Configuration
MODEL_DIR = Path(__file__).parent.parent / "biometric-python" / "models"
MODEL_DIR.mkdir(parents=True, exist_ok=True)
ARCFACE_MODEL_PATH = MODEL_DIR / "arcface_embedding.onnx"


class ArcMarginProduct(nn.Module):
    """ArcFace Margin-based Loss for face recognition.
    
    Implements additive angular margin (SAM) loss:
    cos(θ + m) = cos(θ)cos(m) - sin(θ)sin(m)
    
    This creates angular margin between classes, improving discriminability.
    """
    
    def __init__(self, in_features: int, out_features: int, s: float = 64.0, m: float = 0.5):
        super().__init__()
        self.in_features = in_features
        self.out_features = out_features
        self.s = s  # Scale factor
        self.m = m  # Angular margin
        self.weight = nn.Parameter(torch.FloatTensor(out_features, in_features))
        nn.init.xavier_uniform_(self.weight)
    
    def forward(self, embedded: torch.Tensor, labels: torch.Tensor) -> torch.Tensor:
        """Forward pass with ArcFace loss.
        
        Args:
            embedded: Normalized face embeddings (L2 normalized)
            labels: Ground truth class labels
            
        Returns:
            Modified features with angular margin applied
        """
        # Normalize weights for cosine similarity
        weight_norm = F.normalize(self.weight, dim=1)
        embedded_norm = F.normalize(embedded, dim=1)
        
        # Compute cosine similarity
        cosine = torch.mm(embedded_norm, weight_norm.t())
        
        # Convert to angle
        sine = torch.sqrt(1.0 - torch.pow(cosine, 2))
        
        # Apply angular margin
        cos_theta = cosine * 1.0
        target_mask = torch.zeros_like(cos_theta)
        target_mask.scatter_(1, labels.view(-1, 1).long(), 1.0)
        
        # cos(θ + m) for target class, cos(θ) for others
        output = (cos_theta * 1.0)
        output[target_mask.bool()] = cos_theta[target_mask.bool()] * torch.cos(
            torch.tensor(self.m)
        ) - sine[target_mask.bool()] * torch.sin(torch.tensor(self.m))
        
        output *= self.s
        
        return output


class InsightFaceResNet(nn.Module):
    """ResNet backbone for face embedding extraction.
    
    Architecture based on ResNet-34 with modifications for face recognition:
    - Initial conv layers preserve spatial resolution
    - Global average pooling for fixed-size output
    - L2 normalized embedding output
    
    Expected output: 512-dimensional embedding
    """
    
    def __init__(self, embedding_size: int = 512):
        super().__init__()
        
        # Initial conv layers
        self.conv1 = nn.Conv2d(3, 64, kernel_size=3, stride=2, padding=1, bias=False)
        self.bn1 = nn.BatchNorm2d(64)
        self.relu = nn.ReLU(inplace=True)
        self.maxpool = nn.MaxPool2d(kernel_size=3, stride=2, padding=1)
        
        # ResNet blocks (simplified ResNet-34)
        self.layer1 = self._make_layer(64, 64, block_count=3)
        self.layer2 = self._make_layer(64, 128, block_count=4)
        self.layer3 = self._make_layer(128, 256, block_count=6)
        self.layer4 = self._make_layer(256, 512, block_count=3)
        
        # Feature reduction
        self.pool = nn.AdaptiveAvgPool2d((1, 1))
        self.fc = nn.Linear(512, embedding_size)
        self.bn2 = nn.BatchNorm1d(embedding_size)
    
    def _make_layer(self, in_channels: int, out_channels: int, block_count: int) -> nn.Module:
        """Create a layer of residual blocks."""
        layers = []
        for i in range(block_count):
            layers.append(BasicBlock(in_channels if i == 0 else out_channels, out_channels))
        return nn.Sequential(*layers)
    
    def forward(self, x: torch.Tensor) -> torch.Tensor:
        """Forward pass for embedding extraction."""
        x = self.conv1(x)
        x = self.bn1(x)
        x = self.relu(x)
        x = self.maxpool(x)
        
        x = self.layer1(x)
        x = self.layer2(x)
        x = self.layer3(x)
        x = self.layer4(x)
        
        x = self.pool(x)
        x = x.view(x.size(0), -1)
        x = self.fc(x)
        x = self.bn2(x)
        
        return x


class BasicBlock(nn.Module):
    """Basic ResNet block with 3x3 convolutions."""
    
    expansion = 1
    
    def __init__(self, in_planes: int, planes: int, stride: int = 1):
        super().__init__()
        self.conv1 = nn.Conv2d(in_planes, planes, kernel_size=3, stride=stride, padding=1, bias=False)
        self.bn1 = nn.BatchNorm2d(planes)
        self.conv2 = nn.Conv2d(planes, planes, kernel_size=3, stride=1, padding=1, bias=False)
        self.bn2 = nn.BatchNorm2d(planes)
        
        self.shortcut = nn.Sequential()
        if stride != 1 or in_planes != self.expansion * planes:
            self.shortcut = nn.Sequential(
                nn.Conv2d(in_planes, self.expansion * planes, kernel_size=1, stride=stride, bias=False),
                nn.BatchNorm2d(self.expansion * planes),
            )
    
    def forward(self, x: torch.Tensor) -> torch.Tensor:
        out = F.relu(self.bn1(self.conv1(x)))
        out = self.bn2(self.conv2(out))
        out += self.shortcut(x)
        out = F.relu(out)
        return out


class FaceRecognitionDataset(Dataset):
    """Dataset loader for face recognition training.
    
    Expected directory structure:
    datasets/
    ├── casia-webface/
    │   ├── class_0001/
    │   │   ├── img001.jpg
    │   │   └── ...
    │   ├── class_0002/
    │   │   ├── img001.jpg
    │   │   └── ...
    │   └── ...
    """
    
    def __init__(self, root_dir: str, transform=None):
        self.root_dir = Path(root_dir)
        self.transform = transform
        self.samples = []
        self.class_to_idx = {}
        self.idx_to_class = {}
        self._load_dataset()
    
    def _load_dataset(self):
        """Load dataset with class labels."""
        for idx, class_dir in enumerate(sorted(self.root_dir.iterdir())):
            if class_dir.is_dir():
                self.class_to_idx[class_dir.name] = idx
                self.idx_to_class[idx] = class_dir.name
                
                for img_path in class_dir.glob("*.jpg"):
                    self.samples.append((str(img_path), idx))
                
                for img_path in class_dir.glob("*.png"):
                    self.samples.append((str(img_path), idx))
        
        print(f"Loaded {len(self.samples)} samples from {len(self.class_to_idx)} classes")
    
    def __len__(self):
        return len(self.samples)
    
    def __getitem__(self, idx):
        img_path, label = self.samples[idx]
        image = Image.open(img_path).convert('RGB')

        if self.transform:
            image = self.transform(image)

        return image, torch.tensor(label, dtype=torch.long)


def train_arcface_model(
    model: nn.Module,
    arc_margin: ArcMarginProduct,
    train_loader: DataLoader,
    val_loader: DataLoader,
    device: torch.device,
    epochs: int = 50,
    learning_rate: float = 1e-3,
) -> Dict:
    """Train ArcFace model for face recognition."""
    
    model = model.to(device)
    arc_margin = arc_margin.to(device)
    
    # Use ArcFace loss
    criterion = nn.CrossEntropyLoss()
    optimizer = optim.Adam(
        list(model.parameters()) + list(arc_margin.parameters()),
        lr=learning_rate,
        weight_decay=5e-4,
    )
    scheduler = optim.lr_scheduler.CosineAnnealingLR(optimizer, T_max=epochs)
    
    best_val_accuracy = 0.0
    training_history = []
    
    for epoch in range(epochs):
        # Training phase
        model.train()
        arc_margin.train()
        train_loss = 0.0
        
        for images, labels in train_loader:
            images, labels = images.to(device), labels.to(device)
            
            # Forward pass
            embedded = model(images)
            # Normalize embeddings
            embedded = F.normalize(embedded, dim=1)
            
            # Apply ArcMargin
            logits = arc_margin(embedded, labels)
            
            # Compute loss
            loss = criterion(logits, labels)
            
            # Backward pass
            optimizer.zero_grad()
            loss.backward()
            optimizer.step()
            
            train_loss += loss.item()
        
        # Validation phase (simplified - just accuracy)
        model.eval()
        arc_margin.eval()
        val_correct = 0
        val_total = 0
        
        with torch.no_grad():
            for images, labels in val_loader:
                images, labels = images.to(device), labels.to(device)
                
                embedded = model(images)
                embedded = F.normalize(embedded, dim=1)
                logits = arc_margin(embedded, labels)
                
                _, predicted = torch.max(logits, 1)
                val_correct += (predicted == labels).sum().item()
                val_total += labels.size(0)
        
        val_accuracy = val_correct / val_total
        scheduler.step()
        
        history = {
            'epoch': epoch + 1,
            'train_loss': train_loss / len(train_loader),
            'val_accuracy': val_accuracy,
            'learning_rate': scheduler.get_last_lr()[0],
        }
        training_history.append(history)
        
        print(f"Epoch [{epoch+1}/{epochs}] "
              f"Train Loss: {history['train_loss']:.4f} | "
              f"Val Acc: {history['val_accuracy']:.4f}")
        
        # Save best model
        if val_accuracy > best_val_accuracy:
            best_val_accuracy = val_accuracy
            torch.save({
                'epoch': epoch + 1,
                'model_state_dict': model.state_dict(),
                'arc_margin_state_dict': arc_margin.state_dict(),
                'val_accuracy': val_accuracy,
            }, MODEL_DIR / "arcface_best.pth")
            print(f"✓ Saved best model (Accuracy: {val_accuracy:.4f})")
    
    return {
        'best_val_accuracy': best_val_accuracy,
        'training_history': training_history,
    }


def export_arcface_to_onnx(model: nn.Module, device: torch.device):
    """Export ArcFace model to ONNX format."""
    
    model.eval()
    dummy_input = torch.randn(1, 3, 112, 112, device=device)  # Standard face recognition input size
    
    torch.onnx.export(
        model,
        dummy_input,
        str(ARCFACE_MODEL_PATH),
        input_names=['input'],
        output_names=['embedding'],
        dynamic_axes={
            'input': {0: 'batch_size'},
            'embedding': {0: 'batch_size'},
        },
        opset_version=11,
        do_constant_folding=True,
    )
    
    print(f"✓ ArcFace model exported to ONNX: {ARCFACE_MODEL_PATH}")
    
    # Validate
    onnx_model = onnx.load(str(ARCFACE_MODEL_PATH))
    onnx.checker.check_model(onnx_model)
    print("✓ ONNX model validation passed")


class ArcFacePredictor:
    """Production predictor for ArcFace face embeddings."""
    
    def __init__(self, model_path: str = None):
        if model_path is None:
            model_path = str(ARCFACE_MODEL_PATH)
        
        self.session = ort.InferenceSession(model_path)
        self.input_name = self.session.get_inputs()[0].name
        self.ready = True
    
    def extract_embedding(self, image: np.ndarray) -> np.ndarray:
        """Extract 512-d face embedding from face image.
        
        Args:
            image: RGB face image (112x112)
            
        Returns:
            L2-normalized 512-dimensional embedding vector
        """
        if len(image.shape) == 2:
            image = cv2.cvtColor(image, cv2.COLOR_GRAY2RGB)
        elif image.shape[2] == 4:
            image = cv2.cvtColor(image, cv2.COLOR_RGBA2RGB)
        
        # Preprocess
        image = cv2.resize(image, (112, 112))
        image = image.astype(np.float32) / 255.0
        image = (image - 0.5) / 0.5  # Normalize to [-1, 1]
        image = np.transpose(image, (2, 0, 1))  # CHW format
        image = np.expand_dims(image, axis=0)  # Add batch dimension
        
        # Run inference
        outputs = self.session.run(None, {self.input_name: image})
        embedding = outputs[0][0]
        
        # L2 normalize
        embedding = embedding / np.linalg.norm(embedding)
        
        return embedding
    
    def match_faces(self, img1: np.ndarray, img2: np.ndarray, threshold: float = 0.6) -> Dict:
        """Match two face images using cosine similarity.
        
        Args:
            img1: First face image
            img2: Second face image
            threshold: Similarity threshold for match
            
        Returns:
            Dict with match results
        """
        emb1 = self.extract_embedding(img1)
        emb2 = self.extract_embedding(img2)
        
        similarity = np.dot(emb1, emb2)  # Cosine similarity for L2-normalized vectors
        
        return {
            'is_match': float(similarity) > threshold,
            'similarity': float(similarity),
            'threshold': threshold,
        }


def main():
    """Main training pipeline."""
    parser = argparse.ArgumentParser(description='Train ArcFace Model')
    parser.add_argument('--dataset', type=str, default='casia-webface', help='Dataset path')
    parser.add_argument('--epochs', type=int, default=50, help='Training epochs')
    parser.add_argument('--batch-size', type=int, default=32, help='Batch size')
    parser.add_argument('--learning-rate', type=float, default=1e-3, help='Learning rate')
    parser.add_argument('--embedding-size', type=int, default=512, help='Embedding dimension')
    parser.add_argument('--pretrained', action='store_true', help='Use pretrained weights')
    parser.add_argument('--export-onnx', action='store_true', help='Export to ONNX')
    
    args = parser.parse_args()
    
    device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
    print(f"Using device: {device}")
    
    # Initialize model
    model = InsightFaceResNet(embedding_size=args.embedding_size)
    arc_margin = ArcMarginProduct(
        in_features=args.embedding_size,
        out_features=1000,  # Number of identity classes
        s=64.0,
        m=0.5,
    )
    
    # Load pretrained if available
    if args.pretrained and (MODEL_DIR / "arcface_best.pth").exists():
        checkpoint = torch.load(MODEL_DIR / "arcface_best.pth", map_location=device)
        model.load_state_dict(checkpoint['model_state_dict'])
        arc_margin.load_state_dict(checkpoint['arc_margin_state_dict'])
        print(f"Loaded pretrained model (Accuracy: {checkpoint['val_accuracy']:.4f})")
    
    # Setup transforms
    from torchvision import transforms
    
    train_transform = transforms.Compose([
        transforms.RandomHorizontalFlip(),
        transforms.RandomAffine(degrees=15, translate=(0.1, 0.1)),
        transforms.ColorJitter(brightness=0.2, contrast=0.2),
        transforms.ToTensor(),
    ])
    
    val_transform = transforms.Compose([
        transforms.ToTensor(),
    ])
    
    # Load dataset
    dataset_path = Path(__file__).parent / ".." / ".." / "datasets" / args.dataset
    
    if dataset_path.exists():
        train_dataset = FaceRecognitionDataset(str(dataset_path), transform=train_transform)
        val_dataset = FaceRecognitionDataset(str(dataset_path), transform=val_transform)
    else:
        print(f"⚠ Dataset not found at {dataset_path}")
        print("Generating synthetic dataset...")
        # Metric learning needs multiple identities, each with a CONSISTENT
        # facial structure across its images (so the model learns to cluster an
        # identity), plus per-image variation (lighting/pose/noise). Pure noise
        # gives no identity signal. We synthesize distinct low-frequency face
        # templates per identity and add realistic capture variation.
        # NOTE: production must fine-tune on a real face corpus (e.g. INEC voter
        # enrolment photos); drop it at datasets/<name> to override.
        num_identities = 16
        imgs_per_identity = 16
        yy, xx = np.mgrid[0:112, 0:112].astype(np.float32)
        rng = np.random.default_rng(42)

        def identity_template(seed):
            r = np.random.default_rng(seed)
            tmpl = np.zeros((112, 112, 3), dtype=np.float32)
            # A few smooth Gaussian blobs per channel = a stable, distinct face.
            for _ in range(6):
                cx, cy = r.uniform(20, 92, 2)
                sx, sy = r.uniform(12, 34, 2)
                amp = r.uniform(40, 110)
                ch = r.integers(0, 3)
                tmpl[:, :, ch] += amp * np.exp(-(((xx - cx) ** 2) / (2 * sx ** 2) + ((yy - cy) ** 2) / (2 * sy ** 2)))
            return tmpl + r.uniform(40, 90)

        for k in range(num_identities):
            cdir = dataset_path / f"class_{k+1:04d}"
            os.makedirs(str(cdir), exist_ok=True)
            tmpl = identity_template(1000 + k)
            for i in range(imgs_per_identity):
                brightness = rng.uniform(0.85, 1.15)
                shift = rng.integers(-4, 5, size=2)
                var = np.roll(tmpl * brightness, shift, axis=(0, 1))
                var = var + rng.normal(0, 8, (112, 112, 3))
                Image.fromarray(np.clip(var, 0, 255).astype(np.uint8)).save(
                    str(cdir / f"face_{i:03d}.jpg"))

        train_dataset = FaceRecognitionDataset(str(dataset_path), transform=train_transform)
        val_dataset = FaceRecognitionDataset(str(dataset_path), transform=val_transform)
    
    train_loader = DataLoader(train_dataset, batch_size=args.batch_size, shuffle=True)
    val_loader = DataLoader(val_dataset, batch_size=args.batch_size, shuffle=False)
    
    print(f"Training on {len(train_dataset)} samples, {len(train_dataset.class_to_idx)} classes")
    
    # Train model
    results = train_arcface_model(
        model=model,
        arc_margin=arc_margin,
        train_loader=train_loader,
        val_loader=val_loader,
        device=device,
        epochs=args.epochs,
        learning_rate=args.learning_rate,
    )
    
    print(f"\n✓ Training complete!")
    print(f"  Best Validation Accuracy: {results['best_val_accuracy']:.4f}")
    
    # Export to ONNX
    if args.export_onnx:
        export_arcface_to_onnx(model, device)
        print("✓ ArcFace model ready for production deployment")


if __name__ == "__main__":
    main()
