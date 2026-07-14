"""Train a Presentation Attack Detection model.

Usage:
    python train_pad_model.py --epochs 50 --dataset oulu-npu
    python train_pad_model.py --epochs 30 --dataset livedet

Datasets:
    - OULU-NPU: https://www.cse.cuhk.edu.hk/leojia/projects/PAD/oulu_npua.zip
    - LivDet: https://www.nist.gov/programs-projects/livedetection
    - SiW: https://www.tru.ac.in/ce/wp-content/uploads/2021/04/SiW.pdf
"""

from __future__ import annotations

import argparse
from pathlib import Path
from typing import Optional

import torch
import torch.nn as nn
from torch.utils.data import DataLoader, Dataset

try:
    from sklearn.metrics import roc_auc_score
    SKLEARN_AVAILABLE = True
except ImportError:
    SKLEARN_AVAILABLE = False

DEVICE = "cuda" if torch.cuda.is_available() else "cpu"


# ---------------------------------------------------------------------------
# Dummy dataset for demonstration (replace with real PAD dataset loading)
# ---------------------------------------------------------------------------


class DummyPADDataset(Dataset):
    """Synthetic PAD dataset for training demonstration.

    In production, replace with real datasets:
    - OULU-NPU: https://www.cse.cuhc.edu.hk/leojia/projects/PAD/oulu_npua.zip
    - LivDet: https://www.nist.gov/programs-projects/livedetection
    - SiW: https://www.tru.ac.in/ce/wp-content/uploads/2021/04/SiW.pdf
    """

    def __init__(self, num_samples: int = 1000, image_size: int = 128, real_ratio: float = 0.5):
        self.num_samples = num_samples
        self.image_size = image_size
        self.real_ratio = real_ratio
        self.labels = []
        self.images = []
        self._generate()

    def _generate(self):
        for i in range(self.num_samples):
            is_real = torch.rand(1).item() < self.real_ratio
            label = 1 if is_real else 0

            # Real face: smoother gradients
            if is_real:
                img = torch.rand(3, self.image_size, self.image_size) * 0.3 + 0.35
            else:
                # Spoof: more noise, higher frequency patterns
                img = torch.rand(3, self.image_size, self.image_size) * 0.5 + 0.25

            self.images.append(img)
            self.labels.append(label)

    def __len__(self):
        return self.num_samples

    def __getitem__(self, idx):
        return self.images[idx], self.labels[idx]


class PADTrainer:
    """Trainer for Presentation Attack Detection models.

    Supports training on OULU-NPU, LivDet, and other PAD datasets.
    Uses BCEWithLogitsLoss for binary classification (real vs spoof).
    """

    def __init__(self, model: nn.Module, device: str = "cuda", lr: float = 1e-4, weight_decay: float = 1e-5):
        self.model = model.to(device)
        self.device = device
        self.criterion = nn.BCEWithLogitsLoss()
        self.optimizer = torch.optim.Adam(model.parameters(), lr=lr, weight_decay=weight_decay)
        self.scheduler = torch.optim.lr_scheduler.CosineAnnealingLR(self.optimizer, T_max=50)

    def train_epoch(self, dataloader: DataLoader) -> float:
        """Train for one epoch. Returns average loss."""
        self.model.train()
        total_loss = 0
        for inputs, labels in dataloader:
            inputs, labels = inputs.to(self.device), labels.to(self.device)
            self.optimizer.zero_grad()
            outputs = self.model(inputs).squeeze()
            loss = self.criterion(outputs, labels.float())
            loss.backward()
            self.optimizer.step()
            total_loss += loss.item()
        self.scheduler.step()
        return total_loss / len(dataloader)

    def evaluate(self, dataloader: DataLoader) -> dict[str, float]:
        """Evaluate model. Returns accuracy and AUC."""
        self.model.eval()
        all_preds, all_labels = [], []
        with torch.no_grad():
            for inputs, labels in dataloader:
                inputs = inputs.to(self.device)
                outputs = torch.sigmoid(self.model(inputs).squeeze())
                all_preds.extend(outputs.cpu().numpy())
                all_labels.extend(labels.numpy())

        preds = (torch.tensor(all_preds) > 0.5).float()
        accuracy = (preds == torch.tensor(all_labels)).float().mean().item()

        result: dict[str, float] = {"accuracy": accuracy}

        if SKLEARN_AVAILABLE and len(set(all_labels)) > 1:
            result["auc"] = roc_auc_score(all_labels, all_preds)

        return result

    def train(self, train_loader: DataLoader, val_loader: DataLoader, epochs: int = 50,
              save_path: Optional[str] = None):
        """Full training loop with validation."""
        for epoch in range(1, epochs + 1):
            train_loss = self.train_epoch(train_loader)
            val_metrics = self.evaluate(val_loader)

            print(
                f"Epoch {epoch}/{epochs} | "
                f"train_loss: {train_loss:.4f} | "
                f"val_acc: {val_metrics['accuracy']:.4f} | "
                f"val_auc: {val_metrics.get('auc', 'N/A')}"
            )

            # Save best model
            if save_path:
                torch.save({
                    "epoch": epoch,
                    "model_state_dict": self.model.state_dict(),
                    "optimizer_state_dict": self.optimizer.state_dict(),
                    "metrics": val_metrics,
                    "train_loss": train_loss,
                }, save_path)


# ---------------------------------------------------------------------------
# Model architectures
# ---------------------------------------------------------------------------


def build_mobilenetv2_pad(pretrained: bool = True) -> nn.Module:
    """MobileNetV2-based PAD model with ImageNet pre-trained backbone."""
    from torchvision.models import MobileNet_V2_Weights, mobilenet_v2

    weights = MobileNet_V2_Weights.IMAGENET1K_V1 if pretrained else None
    base = mobilenet_v2(weights=weights)

    num_features = base.classifier[1].in_features
    base.classifier = nn.Sequential(
        nn.Dropout(0.3),
        nn.Linear(num_features, 128),
        nn.ReLU(),
        nn.Dropout(0.1),
        nn.Linear(128, 1),  # Binary: real vs spoof (no sigmoid — BCEWithLogitsLoss)
    )
    return base


def build_lightweight_cnn() -> nn.Module:
    """Lightweight CNN for fast PAD inference on edge devices."""
    return nn.Sequential(
        # Block 1: 128x128 → 64x64
        nn.Conv2d(3, 32, 3, padding=1), nn.BatchNorm2d(32), nn.ReLU(),
        nn.Conv2d(32, 32, 3, padding=1), nn.BatchNorm2d(32), nn.ReLU(),
        nn.MaxPool2d(2),
        # Block 2: 64x64 → 32x32
        nn.Conv2d(32, 64, 3, padding=1), nn.BatchNorm2d(64), nn.ReLU(),
        nn.Conv2d(64, 64, 3, padding=1), nn.BatchNorm2d(64), nn.ReLU(),
        nn.MaxPool2d(2),
        # Block 3: 32x32 → 16x16
        nn.Conv2d(64, 128, 3, padding=1), nn.BatchNorm2d(128), nn.ReLU(),
        nn.Conv2d(128, 128, 3, padding=1), nn.BatchNorm2d(128), nn.ReLU(),
        nn.MaxPool2d(2),
        # Classification head
        nn.AdaptiveAvgPool2d(1),
        nn.Flatten(),
        nn.Linear(128, 64), nn.ReLU(), nn.Dropout(0.3),
        nn.Linear(64, 1),  # Binary output
    )


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def main():
    parser = argparse.ArgumentParser(description="Train a PAD model")
    parser.add_argument("--epochs", type=int, default=50, help="Number of training epochs")
    parser.add_argument("--dataset", type=str, default="oulu-npu", choices=["oulu-npu", "livedet", "siw", "dummy"],
                        help="Dataset to use for training")
    parser.add_argument("--batch-size", type=int, default=32, help="Batch size")
    parser.add_argument("--lr", type=float, default=1e-4, help="Learning rate")
    parser.add_argument("--model", type=str, default="mobilenetv2", choices=["mobilenetv2", "lightweight"],
                        help="Model architecture")
    parser.add_argument("--save-path", type=str, default=None, help="Path to save the trained model")
    parser.add_argument("--num-samples", type=int, default=1000, help="Number of samples for dummy dataset")
    parser.add_argument("--export-onnx", type=str, default=None, help="Export trained model to ONNX at this path")
    args = parser.parse_args()

    # Select model
    if args.model == "mobilenetv2":
        model = build_mobilenetv2_pad(pretrained=True)
    else:
        model = build_lightweight_cnn()

    # Create datasets
    if args.dataset == "dummy":
        print("Using synthetic dummy dataset")
        train_dataset = DummyPADDataset(num_samples=args.num_samples * 2, real_ratio=0.5)
        val_dataset = DummyPADDataset(num_samples=args.num_samples, real_ratio=0.5)
    else:
        print(f"Dataset '{args.dataset}' selected.")
        print("Note: Replace DummyPADDataset with real dataset loading code.")
        print(f"Expected dataset: {args.dataset}")
        print("  OULU-NPU: https://www.cse.cuhk.edu.hk/leojia/projects/PAD/oulu_npua.zip")
        print("  LivDet:   https://www.nist.gov/programs-projects/livedetection")
        print("  SiW:      https://www.tru.ac.in/ce/wp-content/uploads/2021/04/SiW.pdf")
        train_dataset = DummyPADDataset(num_samples=args.num_samples * 2, real_ratio=0.5)
        val_dataset = DummyPADDataset(num_samples=args.num_samples, real_ratio=0.5)

    train_loader = DataLoader(train_dataset, batch_size=args.batch_size, shuffle=True)
    val_loader = DataLoader(val_dataset, batch_size=args.batch_size, shuffle=False)

    # Trainer
    trainer = PADTrainer(model, device=DEVICE, lr=args.lr)

    # Save path
    if args.save_path is None:
        save_path = f"models/pad_model_{args.model}_{args.dataset}.pt"
    else:
        save_path = args.save_path

    Path(save_path).parent.mkdir(parents=True, exist_ok=True)

    print(f"Training on {DEVICE} | Epochs: {args.epochs} | Batch size: {args.batch_size}")
    print(f"Model: {args.model} | Dataset: {args.dataset}")
    print(f"Saving to: {save_path}\n")

    # Train
    trainer.train(train_loader, val_loader, epochs=args.epochs, save_path=save_path)

    # Final evaluation
    final_metrics = trainer.evaluate(val_loader)
    print("\nFinal validation metrics:")
    print(f"  Accuracy: {final_metrics['accuracy']:.4f}")
    if "auc" in final_metrics:
        print(f"  AUC:      {final_metrics['auc']:.4f}")

    # Export to ONNX if requested
    if args.export_onnx:
        Path(args.export_onnx).parent.mkdir(parents=True, exist_ok=True)
        model.eval()
        dummy_input = torch.randn(1, 3, 128, 128).to(DEVICE)
        torch.onnx.export(
            model.to("cpu"),
            dummy_input.to("cpu"),
            args.export_onnx,
            input_names=["input"],
            output_names=["output"],
            dynamic_axes={"input": {0: "batch_size"}, "output": {0: "batch_size"}},
            opset_version=11,
        )
        print(f"Model exported to ONNX: {args.export_onnx}")


if __name__ == "__main__":
    main()
