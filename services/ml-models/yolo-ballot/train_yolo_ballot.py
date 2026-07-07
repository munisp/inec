"""YOLO-Based Video Ballot Counting Training.

Trains a YOLOv8 model for automated ballot counting in election video feeds.
This enables real-time, automated ballot counting from surveillance footage.

Usage:
    python train_yolo_ballot.py --data datasets/ballot-videos --epochs 100
    python train_yolo_ballot.py --pretrained --export-onnx

Model saved to: services/ml-models/yolo-ballot/yolo_ballot_counting.pt
"""

import os
import argparse
from pathlib import Path
from typing import Dict, List, Tuple, Optional
import subprocess
import json

import numpy as np
import cv2
import torch
from ultralytics import YOLO

# Configuration
MODEL_DIR = Path(__file__).parent
MODEL_DIR.mkdir(parents=True, exist_ok=True)
YOLO_MODEL_PATH = MODEL_DIR / "yolo_ballot_counting.pt"
YOLO_ONNX_PATH = MODEL_DIR / "yolo_ballot_counting.onnx"


class BallotCountingDataset:
    """Dataset loader for YOLO ballot counting training.
    
    Expected directory structure for YOLO format:
    datasets/
    ├── ballot-videos/
    │   ├── images/
    │   │   ├── train/
    │   │   │   ├── frame001.jpg
    │   │   │   └── ...
    │   │   └── val/
    │   │       └── ...
    │   └── labels/
    │       ├── train/
    │       │   ├── frame001.txt
    │       │   └── ...
    │       └── val/
    │           └── ...
    │
    classes.txt:
    ballot
    """
    
    def __init__(self, data_dir: str):
        self.data_dir = Path(data_dir)
    
    def create_yaml_config(self, output_path: str = None) -> str:
        """Create YOLO YAML configuration file."""
        if output_path is None:
            output_path = str(self.data_dir / "ballot_dataset.yaml")
        
        config = {
            'path': str(self.data_dir),
            'train': 'images/train',
            'val': 'images/val',
            'test': '',
            'names': {
                0: 'ballot',
            },
        }
        
        with open(output_path, 'w') as f:
            yaml_str = json.dumps(config, indent=2)
            f.write(yaml_str)
        
        print(f"✓ Created dataset config: {output_path}")
        return output_path
    
    @staticmethod
    def extract_frames(video_path: str, output_dir: str, fps: int = 1) -> int:
        """Extract frames from video at specified FPS.
        
        Args:
            video_path: Path to input video
            output_dir: Directory to save frames
            fps: Frames per second to extract
            
        Returns:
            Number of frames extracted
        """
        os.makedirs(output_dir, exist_ok=True)
        
        cap = cv2.VideoCapture(video_path)
        frame_count = 0
        frame_interval = int(cap.get(cv2.CAP_PROP_FPS) / max(fps, 1))
        
        while True:
            ret, frame = cap.read()
            if not ret:
                break
            
            if frame_count % frame_interval == 0:
                output_path = os.path.join(output_dir, f"frame{frame_count:06d}.jpg")
                cv2.imwrite(output_path, frame)
                frame_count += 1
        
        cap.release()
        print(f"Extracted {frame_count} frames from {video_path}")
        
        return frame_count


class BallotCountingTrainer:
    """YOLO trainer for ballot counting."""
    
    def __init__(
        self,
        model_name: str = 'yolov8n.pt',
        image_size: int = 640,
        batch_size: int = 16,
        epochs: int = 100,
        learning_rate: float = 0.01,
    ):
        self.model_name = model_name
        self.image_size = image_size
        self.batch_size = batch_size
        self.epochs = epochs
        self.learning_rate = learning_rate
        
        # Load pretrained YOLO model
        print(f"Loading YOLO model: {model_name}")
        self.model = YOLO(model_name)
    
    def train(
        self,
        data_yaml: str,
        project: str = str(MODEL_DIR / "runs"),
        name: str = "ballot_counting",
        pretrained: bool = True,
    ) -> Dict:
        """Train YOLO model for ballot counting.
        
        Args:
            data_yaml: Path to dataset YAML config
            project: Project directory for results
            name: Run name
            pretrained: Use pretrained weights
            
        Returns:
            Training results dictionary
        """
        print(f"Training YOLO model for ballot counting...")
        print(f"  Dataset: {data_yaml}")
        print(f"  Epochs: {self.epochs}")
        print(f"  Batch size: {self.batch_size}")
        print(f"  Image size: {self.image_size}")
        
        # Train model
        results = self.model.train(
            data=data_yaml,
            epochs=self.epochs,
            imgsz=self.image_size,
            batch=self.batch_size,
            lr0=self.learning_rate,
            patience=20,
            save=True,
            project=project,
            name=name,
            pretrained=pretrained,
            verbose=True,
        )
        
        # Get training metrics
        metrics = results.metrics
        print(f"\n✓ Training complete!")
        print(f"  mAP@50: {results.results_dict.get('metrics/mAPbox_50', 0):.4f}")
        print(f"  mAP@50-95: {results.results_dict.get('metrics/mAPbox_50-95', 0):.4f}")
        
        return results.results_dict
    
    def export_to_onnx(self, output_path: str = None):
        """Export trained model to ONNX format."""
        if output_path is None:
            output_path = str(YOLO_ONNX_PATH)
        
        print(f"Exporting model to ONNX: {output_path}")
        
        self.model.export(
            format='onnx',
            opset=11,
            dynamic=False,
            simplify=True,
        )
        
        print(f"✓ Model exported to {output_path}")
    
    def predict(self, image: np.ndarray, conf_threshold: float = 0.5) -> Dict:
        """Predict ballots in an image.
        
        Args:
            image: Input image (HWC format)
            conf_threshold: Confidence threshold
            
        Returns:
            Dict with prediction results
        """
        results = self.model.predict(
            source=image,
            conf=conf_threshold,
            imgsz=self.image_size,
            verbose=False,
        )
        
        # Parse results
        predictions = []
        total_count = 0
        
        for result in results:
            boxes = result.boxes
            if boxes is not None:
                for box in boxes:
                    confidence = float(box.conf[0])
                    class_id = int(box.cls[0])
                    bbox = box.xyxy[0].tolist()
                    
                    predictions.append({
                        'class': 'ballot',
                        'confidence': confidence,
                        'bbox': [int(x) for x in bbox],
                    })
                    total_count += 1
        
        return {
            'ballot_count': total_count,
            'predictions': predictions,
            'image_size': image.shape[:2],
        }


class VideoBallotCounter:
    """Production video ballot counter."""
    
    def __init__(self, model_path: str = None):
        if model_path is None:
            model_path = str(YOLO_MODEL_PATH)
        
        self.model = YOLO(model_path)
        self.ready = True
    
    def count_ballots_in_video(
        self,
        video_path: str,
        output_video: str = None,
        conf_threshold: float = 0.5,
        frame_skip: int = 10,
    ) -> Dict:
        """Count ballots in a video feed.
        
        Args:
            video_path: Path to input video
            output_video: Path to save output video with annotations
            conf_threshold: Confidence threshold for predictions
            frame_skip: Process every Nth frame
            
        Returns:
            Dict with counting results
        """
        cap = cv2.VideoCapture(video_path)
        
        if not cap.isOpened():
            raise ValueError(f"Cannot open video: {video_path}")
        
        fps = cap.get(cv2.CAP_PROP_FPS)
        width = int(cap.get(cv2.CAP_PROP_FRAME_WIDTH))
        height = int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT))
        
        total_frames = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
        frame_count = 0
        processed_frames = 0
        
        all_predictions = []
        
        # Setup output video writer
        if output_video:
            fourcc = cv2.VideoWriter_fourcc(*'mp4v')
            out = cv2.VideoWriter(output_video, fourcc, fps / max(frame_skip, 1),
                                (width, height))
        
        while True:
            ret, frame = cap.read()
            if not ret:
                break
            
            frame_count += 1
            
            # Skip frames
            if frame_count % frame_skip != 0:
                continue
            
            # Predict
            results = self.model.predict(
                source=frame,
                conf=conf_threshold,
                imgsz=640,
                verbose=False,
            )
            
            # Process predictions
            predictions = []
            for result in results:
                boxes = result.boxes
                if boxes is not None:
                    for box in boxes:
                        confidence = float(box.conf[0])
                        bbox = box.xyxy[0].tolist()
                        predictions.append({
                            'bbox': [int(x) for x in bbox],
                            'confidence': confidence,
                        })
                        
                        # Draw bounding box
                        x1, y1, x2, y2 = map(int, bbox)
                        cv2.rectangle(frame, (x1, y1), (x2, y2), (0, 255, 0), 2)
                        cv2.putText(frame, f"Ballot {confidence:.2f}",
                                  (x1, y1 - 10), cv2.FONT_HERSHEY_SIMPLEX,
                                  0.5, (0, 255, 0), 2)
            
            all_predictions.extend(predictions)
            processed_frames += 1
            
            # Write to output video
            if output_video:
                out.write(frame)
        
        cap.release()
        if output_video:
            out.release()
        
        # Calculate statistics
        total_ballots_detected = len(all_predictions)
        avg_confidence = np.mean([p['confidence'] for p in all_predictions]) if all_predictions else 0.0
        
        return {
            'total_ballots_detected': total_ballots_detected,
            'total_frames_processed': processed_frames,
            'total_frames_in_video': total_frames,
            'average_confidence': float(avg_confidence),
            'predictions_per_frame': total_ballots_detected / max(processed_frames, 1),
            'output_video': output_video,
        }


def create_synthetic_training_data(
    num_images: int = 100,
    image_size: Tuple[int, int] = (640, 480),
    output_dir: str = None,
) -> str:
    """Create synthetic training data for demonstration.
    
    Generates fake ballot images with bounding box annotations.
    In production, replace with real annotated ballot images.
    """
    if output_dir is None:
        output_dir = str(MODEL_DIR / "synthetic-data")
    
    images_dir = os.path.join(output_dir, "images", "train")
    labels_dir = os.path.join(output_dir, "labels", "train")
    
    os.makedirs(images_dir, exist_ok=True)
    os.makedirs(labels_dir, exist_ok=True)
    
    # Create dataset YAML
    yaml_path = os.path.join(output_dir, "ballot_dataset.yaml")
    
    yaml_content = f"""
path: {output_dir}
train: images/train
val: images/train
names:
  0: ballot
"""
    
    with open(yaml_path, 'w') as f:
        f.write(yaml_content)
    
    # Generate synthetic images
    for i in range(num_images):
        # Create random image
        image = np.random.randint(0, 255, (*image_size, 3), dtype=np.uint8)
        
        # Add some ballot-like rectangles
        num_ballots = np.random.randint(1, 5)
        for j in range(num_ballots):
            x1 = np.random.randint(50, image_size[0] - 150)
            y1 = np.random.randint(50, image_size[1] - 100)
            x2 = x1 + np.random.randint(50, 150)
            y2 = y1 + np.random.randint(30, 100)
            
            # Draw rectangle
            cv2.rectangle(image, (x1, y1), (x2, y2), (255, 255, 255), -1)
            cv2.rectangle(image, (x1, y1), (x2, y2), (0, 0, 0), 2)
        
        # Save image
        image_path = os.path.join(images_dir, f"ballot_{i:04d}.jpg")
        cv2.imwrite(image_path, image)
        
        # Create YOLO annotation
        # Normalize coordinates to [0, 1]
        img_h, img_w = image_size
        classes = [0] * num_ballots
        boxes = []
        
        for j in range(num_ballots):
            x1 = np.random.randint(50, image_size[0] - 150) / img_w
            y1 = np.random.randint(50, image_size[1] - 100) / img_h
            x2 = (x1 * img_w + np.random.randint(50, 150)) / img_w
            y2 = (y1 * img_h + np.random.randint(30, 100)) / img_h
            
            # Convert to center-x, center-y, width, height
            cx = (x1 + x2) / 2
            cy = (y1 + y2) / 2
            w = x2 - x1
            h = y2 - y1
            
            boxes.append(f"{0} {cx:.6f} {cy:.6f} {w:.6f} {h:.6f}")
        
        # Save annotation
        label_path = os.path.join(labels_dir, f"ballot_{i:04d}.txt")
        with open(label_path, 'w') as f:
            f.write('\n'.join(boxes))
    
    print(f"✓ Created synthetic training data in {output_dir}")
    print(f"  Images: {num_images}")
    print(f"  Dataset YAML: {yaml_path}")
    
    return yaml_path


def main():
    """Main training pipeline."""
    parser = argparse.ArgumentParser(description='Train YOLO for Ballot Counting')
    parser.add_argument('--data', type=str, default=None, help='Dataset directory')
    parser.add_argument('--epochs', type=int, default=100, help='Training epochs')
    parser.add_argument('--batch-size', type=int, default=16, help='Batch size')
    parser.add_argument('--pretrained', action='store_true', help='Use pretrained model')
    parser.add_argument('--export-onnx', action='store_true', help='Export to ONNX')
    parser.add_argument('--synthetic', action='store_true', help='Create synthetic data')
    
    args = parser.parse_args()
    
    # Create synthetic data if requested
    data_yaml = None
    if args.synthetic or args.data is None:
        data_yaml = create_synthetic_training_data()
    elif os.path.exists(args.data):
        data_yaml = os.path.join(args.data, "ballot_dataset.yaml")
        if not os.path.exists(data_yaml):
            dataset = BallotCountingDataset(args.data)
            data_yaml = dataset.create_yaml_config()
    
    # Initialize trainer
    trainer = BallotCountingTrainer(
        model_name='yolov8n.pt',
        epochs=args.epochs,
        batch_size=args.batch_size,
    )
    
    # Train model
    if data_yaml:
        trainer.train(
            data_yaml=data_yaml,
            pretrained=args.pretrained,
        )
    
    # Export to ONNX
    if args.export_onnx:
        trainer.export_to_onnx()
        print("✓ Model ready for production deployment")


if __name__ == "__main__":
    main()
