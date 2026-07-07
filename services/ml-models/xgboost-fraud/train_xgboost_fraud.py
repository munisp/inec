"""XGBoost Fraud Detection Training Pipeline.

Trains an XGBoost model for real-time fraud detection in election results.
Uses historical data patterns to identify suspicious voting behaviors.

Usage:
    python train_xgboost_fraud.py --data datasets/election-data --epochs 100
    python train_xgboost_fraud.py --pretrained --tuning

Model saved to: services/ml-models/xgboost-fraud/xgboost_fraud_model.json
"""

import os
import json
import argparse
from pathlib import Path
from typing import Dict, List, Tuple, Optional

import numpy as np
import xgboost as xgb
from sklearn.model_selection import train_test_split, cross_val_score
from sklearn.metrics import (
    accuracy_score, precision_score, recall_score, f1_score,
    roc_auc_score, confusion_matrix, classification_report,
)
import pandas as pd

# Configuration
MODEL_DIR = Path(__file__).parent
MODEL_DIR.mkdir(parents=True, exist_ok=True)
FRAUD_MODEL_PATH = MODEL_DIR / "xgboost_fraud_model.json"
FRAUD_MODEL_PARAMS_PATH = MODEL_DIR / "xgboost_fraud_params.json"


class ElectionFraudFeatures:
    """Feature engineering for election fraud detection."""
    
    @staticmethod
    def extract_features(records: List[Dict]) -> pd.DataFrame:
        """Extract features from election records.
        
        Features include:
        - Turnout statistics (percentage, deviation from mean)
        - Vote distribution patterns (Benford's law compliance)
        - Temporal patterns (vote submission timing)
        - Geographic patterns (comparison with neighboring PUs)
        - Statistical outliers (z-scores, IQR)
        """
        df = pd.DataFrame(records)
        
        if df.empty:
            return pd.DataFrame()
        
        # Basic turnout features
        df['turnout_percentage'] = (df['votes_cast'] / df['accredited_voters'] * 100)
        df['turnout_deviation'] = df['turnout_percentage'] - df['turnout_percentage'].mean()
        
        # Vote distribution features
        df['party_a_percentage'] = (df['party_a_votes'] / df['votes_cast'] * 100)
        df['party_b_percentage'] = (df['party_b_votes'] / df['votes_cast'] * 100)
        
        # First digit analysis (Benford's law)
        def first_digit(n):
            while n >= 10:
                n //= 10
            return n
        
        df['first_digit'] = df['votes_cast'].apply(first_digit)
        df['benford_deviation'] = abs(df['first_digit'] - 1)  # Simplified
        
        # Temporal features
        df['submission_hour'] = pd.to_datetime(df['submitted_at']).dt.hour
        df['submission_minute'] = pd.to_datetime(df['submitted_at']).dt.minute
        
        # Geographic features
        df['latitude_deviation'] = df['latitude'] - df['latitude'].mean()
        df['longitude_deviation'] = df['longitude'] - df['longitude'].mean()
        
        # Statistical features
        df['vote_count_zscore'] = (df['votes_cast'] - df['votes_cast'].mean()) / max(df['votes_cast'].std(), 1)
        df['turnout_zscore'] = (df['turnout_percentage'] - df['turnout_percentage'].mean()) / max(df['turnout_percentage'].std(), 1)
        
        # IQR-based outlier detection
        Q1 = df['turnout_percentage'].quantile(0.25)
        Q3 = df['turnout_percentage'].quantile(0.75)
        IQR = Q3 - Q1
        df['is_iqr_outlier'] = ((df['turnout_percentage'] < Q1 - 1.5 * IQR) | 
                                 (df['turnout_percentage'] > Q3 + 1.5 * IQR)).astype(int)
        
        return df
    
    @staticmethod
    def get_feature_columns(df: pd.DataFrame) -> List[str]:
        """Get list of feature columns for model training."""
        exclude_cols = ['pu_code', 'submitted_at', 'is_anomalous', 'first_digit']
        return [col for col in df.columns if col not in exclude_cols and df[col].dtype in ['float64', 'int64', 'float32', 'int32']]


class XGBoostFraudDetector:
    """XGBoost model for election fraud detection."""
    
    def __init__(
        self,
        max_depth: int = 6,
        learning_rate: float = 0.1,
        n_estimators: int = 100,
        scale_pos_weight: float = 1.0,
        subsample: float = 0.8,
        colsample_bytree: float = 0.8,
    ):
        self.model = xgb.XGBClassifier(
            max_depth=max_depth,
            learning_rate=learning_rate,
            n_estimators=n_estimators,
            scale_pos_weight=scale_pos_weight,
            subsample=subsample,
            colsample_bytree=colsample_bytree,
            eval_metric='auc',
            use_label_encoder=False,
            random_state=42,
        )
        
        self.feature_columns: List[str] = []
        self.training_history: Dict = {}
    
    def train(
        self,
        X_train: pd.DataFrame,
        y_train: pd.Series,
        X_val: Optional[pd.DataFrame] = None,
        y_val: Optional[pd.Series] = None,
    ) -> Dict:
        """Train the XGBoost model."""
        
        print(f"Training XGBoost model on {len(X_train)} samples...")
        
        # Prepare data
        X_train_arr = X_train[self.feature_columns].values if self.feature_columns else X_train.values
        y_train_arr = y_train.values
        
        if X_val is not None and y_val is not None:
            X_val_arr = X_val[self.feature_columns].values if self.feature_columns else X_val.values
            y_val_arr = y_val.values
            
            eval_set = [(X_val_arr, y_val_arr)]
        else:
            eval_set = None
        
        # Train model
        self.model.fit(
            X_train_arr, y_train_arr,
            eval_set=eval_set,
            verbose=True,
        )
        
        # Get feature importances
        feature_importances = dict(zip(
            self.feature_columns,
            self.model.feature_importances_.tolist(),
        ))
        
        # Training history
        self.training_history = {
            'feature_importances': feature_importances,
            'n_estimators': self.model.n_estimators,
        }
        
        return self.training_history
    
    def evaluate(
        self,
        X_test: pd.DataFrame,
        y_test: pd.Series,
    ) -> Dict:
        """Evaluate model performance."""
        
        X_test_arr = X_test[self.feature_columns].values if self.feature_columns else X_test.values
        y_test_arr = y_test.values
        
        # Predictions
        y_pred = self.model.predict(X_test_arr)
        y_pred_proba = self.model.predict_proba(X_test_arr)[:, 1]
        
        # Metrics
        metrics = {
            'accuracy': accuracy_score(y_test_arr, y_pred),
            'precision': precision_score(y_test_arr, y_pred, zero_division=0),
            'recall': recall_score(y_test_arr, y_pred, zero_division=0),
            'f1_score': f1_score(y_test_arr, y_pred, zero_division=0),
            'roc_auc': roc_auc_score(y_test_arr, y_pred_proba),
            'confusion_matrix': confusion_matrix(y_test_arr, y_pred).tolist(),
            'classification_report': classification_report(
                y_test_arr, y_pred, output_dict=True, zero_division=0
            ),
        }
        
        print("\n" + "=" * 60)
        print("MODEL EVALUATION RESULTS")
        print("=" * 60)
        print(f"Accuracy:  {metrics['accuracy']:.4f}")
        print(f"Precision: {metrics['precision']:.4f}")
        print(f"Recall:    {metrics['recall']:.4f}")
        print(f"F1 Score:  {metrics['f1_score']:.4f}")
        print(f"ROC AUC:   {metrics['roc_auc']:.4f}")
        print("\nConfusion Matrix:")
        print(f"  TN  FP  \n  {metrics['confusion_matrix'][0][0]:>4}  {metrics['confusion_matrix'][0][1]:>4}")
        print(f"  FN  TN  \n  {metrics['confusion_matrix'][1][0]:>4}  {metrics['confusion_matrix'][1][1]:>4}")
        print("=" * 60 + "\n")
        
        return metrics
    
    def save_model(self, path: str = None):
        """Save trained model to disk."""
        if path is None:
            path = str(FRAUD_MODEL_PATH)
        
        self.model.save_model(path)
        
        # Save parameters and feature columns
        params = {
            'model_params': self.model.get_params(),
            'feature_columns': self.feature_columns,
            'training_history': self.training_history,
        }
        
        params_path = path.replace('.json', '_params.json')
        with open(params_path, 'w') as f:
            json.dump(params, f, indent=2, default=str)
        
        print(f"✓ Model saved to {path}")
        print(f"✓ Model parameters saved to {params_path}")
    
    @classmethod
    def load_model(cls, path: str = None) -> 'XGBoostFraudDetector':
        """Load trained model from disk."""
        if path is None:
            path = str(FRAUD_MODEL_PATH)
        
        # Load model
        model = cls()
        model.model = xgb.XGBClassifier()
        model.model.load_model(path)
        
        # Load parameters
        params_path = path.replace('.json', '_params.json')
        if os.path.exists(params_path):
            with open(params_path, 'r') as f:
                params = json.load(f)
            model.feature_columns = params.get('feature_columns', [])
            model.training_history = params.get('training_history', {})
        
        return model


class FraudPredictor:
    """Production predictor for fraud detection."""
    
    def __init__(self, model_path: str = None):
        if model_path is None:
            model_path = str(FRAUD_MODEL_PATH)
        
        self.detector = XGBoostFraudDetector.load_model(model_path)
        self.ready = True
    
    def predict(self, record: Dict) -> Dict:
        """Predict fraud probability for a single record.
        
        Args:
            record: Dict with election data fields
            
        Returns:
            Dict with prediction results
        """
        # Convert to DataFrame
        df = pd.DataFrame([record])
        
        # Extract features
        features_df = ElectionFraudFeatures.extract_features([record])
        
        if features_df.empty:
            return {
                'is_fraud': False,
                'fraud_probability': 0.0,
                'risk_level': 'low',
                'confidence': 1.0,
            }
        
        # Get feature values
        X = features_df[self.detector.feature_columns].values
        
        # Predict
        fraud_proba = self.detector.model.predict_proba(X)[0][1]
        is_fraud = fraud_proba > 0.5
        
        # Risk level
        if fraud_proba > 0.8:
            risk_level = 'critical'
        elif fraud_proba > 0.6:
            risk_level = 'high'
        elif fraud_proba > 0.3:
            risk_level = 'medium'
        else:
            risk_level = 'low'
        
        return {
            'is_fraud': bool(is_fraud),
            'fraud_probability': float(fraud_proba),
            'risk_level': risk_level,
            'confidence': float(max(fraud_proba, 1.0 - fraud_proba)),
            'features_analyzed': len(self.detector.feature_columns),
        }


def create_synthetic_fraud_data(
    num_samples: int = 10000,
    fraud_rate: float = 0.1,
) -> Tuple[pd.DataFrame, pd.Series]:
    """Create synthetic election data for training."""
    
    print(f"Creating synthetic fraud detection data: {num_samples} samples")
    
    np.random.seed(42)
    
    # Normal records
    num_normal = int(num_samples * (1 - fraud_rate))
    num_fraud = num_samples - num_normal
    
    # Generate normal records
    normal_records = {
        'pu_code': [f"PU-{i:05d}" for i in range(num_normal)],
        'accredited_voters': np.random.randint(500, 2000, num_normal),
        'votes_cast': np.random.normal(1000, 150, num_normal),
        'party_a_votes': np.random.normal(400, 80, num_normal),
        'party_b_votes': np.random.normal(350, 70, num_normal),
        'latitude': np.random.normal(9.0579, 0.05, num_normal),
        'longitude': np.random.normal(7.4951, 0.05, num_normal),
        'submitted_at': pd.date_range('2026-01-01', periods=num_normal, freq='H'),
        'is_anomalous': [0] * num_normal,
    }
    
    # Generate fraud records (suspicious patterns)
    fraud_records = {
        'pu_code': [f"PU-{i+num_normal:05d}" for i in range(num_fraud)],
        'accredited_voters': np.random.randint(500, 2000, num_fraud),
        'votes_cast': np.concatenate([
            np.random.normal(1800, 100, num_fraud // 2),  # Overvoting
            np.random.normal(50, 20, num_fraud // 2),     # Underreporting
        ]),
        'party_a_votes': np.random.normal(900, 50, num_fraud),  # Extreme skew
        'party_b_votes': np.random.normal(50, 20, num_fraud),
        'latitude': np.random.normal(9.0579, 0.05, num_fraud),
        'longitude': np.random.normal(7.4951, 0.05, num_fraud),
        'submitted_at': pd.date_range('2026-01-01', periods=num_fraud, freq='H'),
        'is_anomalous': [1] * num_fraud,
    }
    
    # Combine
    df = pd.concat([pd.DataFrame(normal_records), pd.DataFrame(fraud_records)], ignore_index=True)
    
    # Ensure valid ranges
    df['votes_cast'] = df['votes_cast'].clip(0, df['accredited_voters'])
    df['party_a_votes'] = df['party_a_votes'].clip(0, df['votes_cast'])
    df['party_b_votes'] = (df['votes_cast'] - df['party_a_votes']).clip(0)
    
    # Split features and labels
    feature_cols = ['accredited_voters', 'votes_cast', 'party_a_votes', 'party_b_votes',
                   'latitude', 'longitude', 'submitted_at']
    
    X = df[feature_cols]
    y = df['is_anomalous']
    
    return X, y


def hyperparameter_tuning(
    X_train: pd.DataFrame,
    y_train: pd.Series,
    X_val: pd.DataFrame,
    y_val: pd.Series,
) -> Dict:
    """Perform hyperparameter tuning for XGBoost model."""
    
    print("Performing hyperparameter tuning...")
    
    param_grid = {
        'max_depth': [4, 6, 8],
        'learning_rate': [0.01, 0.1, 0.2],
        'n_estimators': [100, 200, 300],
        'subsample': [0.7, 0.8, 0.9],
        'colsample_bytree': [0.7, 0.8, 0.9],
    }
    
    best_auc = 0.0
    best_params = {}
    
    # Grid search (simplified - sample combinations)
    from itertools import product
    
    keys = param_grid.keys()
    values = param_grid.values()
    
    for i, combo in enumerate(product(*values)):
        if i >= 20:  # Limit to 20 combinations for speed
            break
        
        params = dict(zip(keys, combo))
        
        model = xgb.XGBClassifier(
            **params,
            eval_metric='auc',
            use_label_encoder=False,
            random_state=42,
        )
        
        X_train_arr = X_train.values
        X_val_arr = X_val.values
        
        model.fit(X_train_arr, y_train.values, eval_set=[(X_val_arr, y_val.values)], verbose=False)
        
        y_pred_proba = model.predict_proba(X_val_arr)[:, 1]
        try:
            auc = roc_auc_score(y_val.values, y_pred_proba)
        except ValueError:
            auc = 0.0
        
        if auc > best_auc:
            best_auc = auc
            best_params = params
    
    print(f"✓ Best AUC: {best_auc:.4f}")
    print(f"✓ Best Parameters: {best_params}")
    
    return best_params


def main():
    """Main training pipeline."""
    parser = argparse.ArgumentParser(description='Train XGBoost Fraud Detection Model')
    parser.add_argument('--num-samples', type=int, default=10000, help='Number of samples')
    parser.add_argument('--fraud-rate', type=float, default=0.1, help='Fraud rate in data')
    parser.add_argument('--epochs', type=int, default=100, help='Number of estimators')
    parser.add_argument('--pretrained', action='store_true', help='Use pretrained model')
    parser.add_argument('--tuning', action='store_true', help='Perform hyperparameter tuning')
    
    args = parser.parse_args()
    
    # Create/load data
    X, y = create_synthetic_fraud_data(
        num_samples=args.num_samples,
        fraud_rate=args.fraud_rate,
    )
    
    # Split data
    X_train, X_temp, y_train, y_temp = train_test_split(
        X, y, test_size=0.3, random_state=42, stratify=y
    )
    X_val, X_test, y_val, y_test = train_test_split(
        y_temp, y_temp, test_size=0.5, random_state=42  # Simplified
    )
    
    # Extract features
    X_train_features = ElectionFraudFeatures.extract_features(
        pd.concat([X_train, pd.DataFrame({'is_anomalous': y_train.values})], axis=1).to_dict('records')
    )
    X_val_features = ElectionFraudFeatures.extract_features(
        pd.concat([X_val, pd.DataFrame({'is_anomalous': y_val.values})], axis=1).to_dict('records')
    )
    X_test_features = ElectionFraudFeatures.extract_features(
        pd.concat([X_test, pd.DataFrame({'is_anomalous': y_test.values})], axis=1).to_dict('records')
    )
    
    feature_columns = ElectionFraudFeatures.get_feature_columns(X_train_features)
    print(f"Features: {feature_columns}")
    
    # Hyperparameter tuning
    if args.tuning:
        best_params = hyperparameter_tuning(
            X_train_features[feature_columns], y_train,
            X_val_features[feature_columns], y_val,
        )
        
        detector = XGBoostFraudDetector(**best_params)
    else:
        detector = XGBoostFraudDetector()
    
    detector.feature_columns = feature_columns
    
    # Train model
    print("\nTraining model...")
    history = detector.train(
        X_train=X_train_features,
        y_train=y_train,
        X_val=X_val_features,
        y_val=y_val,
    )
    
    # Evaluate
    print("\nEvaluating model...")
    metrics = detector.evaluate(
        X_test=X_test_features,
        y_test=y_test,
    )
    
    # Save model
    detector.save_model()
    
    print(f"\n✓ Training complete!")
    print(f"  Final AUC: {metrics['roc_auc']:.4f}")
    print(f"  Final F1: {metrics['f1_score']:.4f}")


if __name__ == "__main__":
    main()
