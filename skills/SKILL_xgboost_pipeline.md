# SKILL: XGBoost Bot Detection Pipeline

## Overview

Complete ML pipeline for bot detection in GhostRoute using XGBoost with SMOTE
oversampling for class imbalance, Optuna hyperparameter tuning, and FastAPI
model serving with hot-swap capability for zero-downtime model updates.

## Requirements

```text
# requirements.txt
xgboost==2.1.3
scikit-learn==1.6.1
imbalanced-learn==0.12.4
optuna==4.1.0
fastapi==0.115.6
uvicorn==0.34.0
joblib==1.4.2
numpy==2.2.1
pandas==2.2.3
boto3==1.35.86
pydantic==2.10.4
```

## Feature Engineering

### Click Data Feature Extraction

```python
import numpy as np
import pandas as pd
from scipy.stats import entropy
from typing import Dict, List, Any


class FeatureEngineer:
    """Extract features from raw click data for bot detection."""

    # Known browser JA3 fingerprints (partial list)
    KNOWN_BROWSER_JA3 = {
        "771,4865-4866-4867-49195-49199-49196-49200-52393-52392": "Chrome 120+",
        "771,4865-4867-4866-49195-49199-52393-52392-49196-49200": "Firefox 121+",
        "771,4865-4866-4867-49196-49195-52393-49200-49199-52392": "Safari 17+",
    }

    def extract_features(self, click_data: Dict[str, Any]) -> Dict[str, float]:
        """Extract all features from a single click event."""
        features = {}
        features.update(self._time_of_day_features(click_data["timestamp"]))
        features.update(self._request_rate_features(click_data))
        features.update(self._fingerprint_entropy(click_data["fingerprint"]))
        features.update(self._header_anomaly_score(click_data["headers"]))
        features.update(self._mouse_movement_features(click_data.get("mouse_data", [])))
        features.update(self._js_timing_features(click_data.get("js_timing", {})))
        features.update(self._tls_fingerprint_score(click_data.get("ja3", "")))
        return features

    def _time_of_day_features(self, timestamp: float) -> Dict[str, float]:
        """Cyclical encoding of hour using sin/cos transformation."""
        from datetime import datetime
        dt = datetime.fromtimestamp(timestamp)
        hour = dt.hour + dt.minute / 60.0
        return {
            "hour_sin": np.sin(2 * np.pi * hour / 24.0),
            "hour_cos": np.cos(2 * np.pi * hour / 24.0),
            "day_of_week": dt.weekday(),
            "is_weekend": float(dt.weekday() >= 5),
        }

    def _request_rate_features(self, click_data: Dict) -> Dict[str, float]:
        """Calculate request rate from same IP/fingerprint in rolling window."""
        recent_clicks = click_data.get("recent_clicks_same_ip", [])
        if len(recent_clicks) < 2:
            return {"clicks_per_minute": 0.0, "click_burst_score": 0.0}

        timestamps = sorted(recent_clicks)
        time_span = (timestamps[-1] - timestamps[0]) / 60.0
        clicks_per_minute = len(timestamps) / max(time_span, 0.01)

        # Burst detection: variance in inter-click intervals
        intervals = np.diff(timestamps)
        burst_score = 1.0 / (np.std(intervals) + 0.01) if len(intervals) > 1 else 0.0

        return {
            "clicks_per_minute": clicks_per_minute,
            "click_burst_score": float(burst_score),
            "total_clicks_1min": float(len(timestamps)),
        }

    def _fingerprint_entropy(self, fingerprint: Dict) -> Dict[str, float]:
        """Shannon entropy of fingerprint components."""
        components = [
            str(fingerprint.get("canvas_hash", "")),
            str(fingerprint.get("webgl_hash", "")),
            str(fingerprint.get("audio_hash", "")),
            str(fingerprint.get("fonts", "")),
            str(fingerprint.get("plugins", "")),
            str(fingerprint.get("screen_res", "")),
            str(fingerprint.get("timezone", "")),
            str(fingerprint.get("language", "")),
        ]
        combined = "".join(components)
        if not combined:
            return {"fp_entropy": 0.0, "fp_component_count": 0.0}

        # Character-level entropy
        char_counts = np.array([combined.count(c) for c in set(combined)])
        char_probs = char_counts / char_counts.sum()
        fp_entropy = float(entropy(char_probs, base=2))

        # Component completeness
        non_empty = sum(1 for c in components if c)

        return {
            "fp_entropy": fp_entropy,
            "fp_component_count": float(non_empty),
            "fp_uniqueness_score": fp_entropy / max(len(set(combined)), 1),
        }

    def _header_anomaly_score(self, headers: Dict[str, str]) -> Dict[str, float]:
        """Score anomalies in HTTP headers."""
        expected_headers = [
            "user-agent", "accept", "accept-language",
            "accept-encoding", "connection", "host"
        ]
        missing_count = sum(1 for h in expected_headers if h not in headers)

        # Header count anomaly (normal browsers send 8-15 headers)
        header_count = len(headers)
        count_anomaly = abs(header_count - 11) / 11.0

        # Check for suspicious headers
        suspicious = ["x-forwarded-for", "via", "x-real-ip"]
        suspicious_count = sum(1 for h in suspicious if h in headers)

        # Header order score (simplified)
        header_keys = list(headers.keys())
        order_score = 0.0
        if header_keys and header_keys[0].lower() != "host":
            order_score += 0.3
        if "user-agent" in headers and list(headers.keys()).index("user-agent") > 5:
            order_score += 0.2

        return {
            "missing_headers": float(missing_count),
            "header_count_anomaly": count_anomaly,
            "suspicious_header_count": float(suspicious_count),
            "header_order_score": order_score,
        }

    def _mouse_movement_features(self, mouse_data: List[Dict]) -> Dict[str, float]:
        """Extract features from mouse movement telemetry."""
        if not mouse_data or len(mouse_data) < 3:
            return {
                "mouse_velocity_var": 0.0,
                "mouse_straightness": 1.0,
                "click_timing_std": 0.0,
                "has_mouse_data": 0.0,
            }

        points = [(m["x"], m["y"], m["t"]) for m in mouse_data]

        # Velocity variance
        velocities = []
        for i in range(1, len(points)):
            dx = points[i][0] - points[i-1][0]
            dy = points[i][1] - points[i-1][1]
            dt = max(points[i][2] - points[i-1][2], 1)
            velocity = np.sqrt(dx**2 + dy**2) / dt
            velocities.append(velocity)

        velocity_var = float(np.var(velocities)) if velocities else 0.0

        # Straightness ratio (direct distance / path length)
        total_path = sum(
            np.sqrt((points[i][0]-points[i-1][0])**2 + (points[i][1]-points[i-1][1])**2)
            for i in range(1, len(points))
        )
        direct_dist = np.sqrt(
            (points[-1][0]-points[0][0])**2 + (points[-1][1]-points[0][1])**2
        )
        straightness = direct_dist / max(total_path, 0.01)

        # Click timing standard deviation
        click_times = [m["t"] for m in mouse_data if m.get("click")]
        click_timing_std = float(np.std(np.diff(click_times))) if len(click_times) > 1 else 0.0

        return {
            "mouse_velocity_var": velocity_var,
            "mouse_straightness": float(straightness),
            "click_timing_std": click_timing_std,
            "has_mouse_data": 1.0,
        }

    def _js_timing_features(self, js_timing: Dict) -> Dict[str, float]:
        """Features from JavaScript challenge execution timing."""
        if not js_timing:
            return {
                "js_exec_time_ms": 0.0,
                "js_challenge_completed": 0.0,
                "js_timing_suspicious": 1.0,
            }

        exec_time = js_timing.get("execution_ms", 0)
        completed = js_timing.get("completed", False)

        # Bots often complete JS challenges too fast (<50ms) or not at all
        suspicious = 0.0
        if exec_time < 50 and completed:
            suspicious = 0.8  # Suspiciously fast
        elif exec_time > 30000:
            suspicious = 0.6  # Very slow, possible headless
        elif not completed:
            suspicious = 1.0

        return {
            "js_exec_time_ms": float(exec_time),
            "js_challenge_completed": float(completed),
            "js_timing_suspicious": suspicious,
        }

    def _tls_fingerprint_score(self, ja3: str) -> Dict[str, float]:
        """Match JA3 fingerprint against known browser database."""
        if not ja3:
            return {"tls_match_score": 0.0, "tls_known_browser": 0.0}

        # Check exact match
        if ja3 in self.KNOWN_BROWSER_JA3:
            return {"tls_match_score": 1.0, "tls_known_browser": 1.0}

        # Partial match on cipher suite prefix
        ja3_parts = ja3.split(",")
        best_match = 0.0
        for known_ja3 in self.KNOWN_BROWSER_JA3:
            known_parts = known_ja3.split(",")
            if ja3_parts[0] == known_parts[0]:  # Same TLS version
                # Compare cipher overlap
                client_ciphers = set(ja3_parts[1].split("-")) if len(ja3_parts) > 1 else set()
                known_ciphers = set(known_parts[1].split("-")) if len(known_parts) > 1 else set()
                if client_ciphers and known_ciphers:
                    overlap = len(client_ciphers & known_ciphers) / len(known_ciphers)
                    best_match = max(best_match, overlap)

        return {"tls_match_score": best_match, "tls_known_browser": 0.0}
```

## Training Pipeline

### Data Loading and SMOTE Oversampling

```python
import xgboost as xgb
from sklearn.model_selection import train_test_split, StratifiedKFold
from sklearn.metrics import (
    classification_report, precision_recall_curve,
    roc_auc_score, average_precision_score, f1_score
)
from imblearn.over_sampling import SMOTE
from imblearn.pipeline import Pipeline as ImbPipeline
import joblib
import json
import os
from datetime import datetime


class BotDetectionTrainer:
    """Complete training pipeline for bot detection model."""

    DEFAULT_PARAMS = {
        "max_depth": 6,
        "learning_rate": 0.1,
        "n_estimators": 500,
        "objective": "binary:logistic",
        "eval_metric": ["aucpr", "logloss"],
        "tree_method": "hist",
        "random_state": 42,
        "n_jobs": -1,
    }

    FEATURE_NAMES = [
        "hour_sin", "hour_cos", "day_of_week", "is_weekend",
        "clicks_per_minute", "click_burst_score", "total_clicks_1min",
        "fp_entropy", "fp_component_count", "fp_uniqueness_score",
        "missing_headers", "header_count_anomaly",
        "suspicious_header_count", "header_order_score",
        "mouse_velocity_var", "mouse_straightness",
        "click_timing_std", "has_mouse_data",
        "js_exec_time_ms", "js_challenge_completed", "js_timing_suspicious",
        "tls_match_score", "tls_known_browser",
    ]

    def __init__(self, data_path: str, model_output_dir: str):
        self.data_path = data_path
        self.model_output_dir = model_output_dir
        self.feature_engineer = FeatureEngineer()

    def load_and_prepare_data(self) -> tuple:
        """Load click data and extract features."""
        df = pd.read_parquet(self.data_path)

        # Extract features for each click record
        features_list = []
        for _, row in df.iterrows():
            click_data = {
                "timestamp": row["timestamp"],
                "recent_clicks_same_ip": json.loads(row["recent_clicks"]),
                "fingerprint": json.loads(row["fingerprint_data"]),
                "headers": json.loads(row["request_headers"]),
                "mouse_data": json.loads(row.get("mouse_events", "[]")),
                "js_timing": json.loads(row.get("js_timing", "{}")),
                "ja3": row.get("ja3_fingerprint", ""),
            }
            features_list.append(self.feature_engineer.extract_features(click_data))

        X = pd.DataFrame(features_list, columns=self.FEATURE_NAMES)
        y = df["is_bot"].values  # 1 = confirmed bot, 0 = human

        print(f"Dataset shape: {X.shape}")
        print(f"Class distribution: bots={y.sum()}, humans={(1-y).sum()}")
        print(f"Bot ratio: {y.mean():.4f}")

        return X, y

    def split_data(self, X: pd.DataFrame, y: np.ndarray) -> tuple:
        """Stratified train/validation/test split (60/20/20)."""
        X_temp, X_test, y_temp, y_test = train_test_split(
            X, y, test_size=0.2, stratify=y, random_state=42
        )
        X_train, X_val, y_train, y_val = train_test_split(
            X_temp, y_temp, test_size=0.25, stratify=y_temp, random_state=42
        )
        print(f"Train: {X_train.shape[0]}, Val: {X_val.shape[0]}, Test: {X_test.shape[0]}")
        return X_train, X_val, X_test, y_train, y_val, y_test

    def apply_smote(self, X_train: pd.DataFrame, y_train: np.ndarray) -> tuple:
        """Apply SMOTE oversampling to minority class (bots)."""
        smote = SMOTE(
            sampling_strategy=0.5,  # Minority becomes 50% of majority
            k_neighbors=5,
            random_state=42,
            n_jobs=-1,
        )
        X_resampled, y_resampled = smote.fit_resample(X_train, y_train)
        print(f"Before SMOTE: {X_train.shape[0]} samples")
        print(f"After SMOTE: {X_resampled.shape[0]} samples")
        print(f"New class distribution: bots={y_resampled.sum()}, humans={(1-y_resampled).sum()}")
        return X_resampled, y_resampled

    def train_model(
        self, X_train, y_train, X_val, y_val, params: dict = None
    ) -> xgb.XGBClassifier:
        """Train XGBoost model with early stopping."""
        if params is None:
            params = self.DEFAULT_PARAMS.copy()

        # Calculate scale_pos_weight from class ratio
        n_negative = (y_train == 0).sum()
        n_positive = (y_train == 1).sum()
        params["scale_pos_weight"] = n_negative / max(n_positive, 1)

        model = xgb.XGBClassifier(**params)
        model.fit(
            X_train, y_train,
            eval_set=[(X_val, y_val)],
            verbose=50,
        )

        # Report best iteration
        print(f"Best iteration: {model.best_iteration}")
        print(f"Best score: {model.best_score:.6f}")
        return model

    def calibrate_threshold(
        self, model: xgb.XGBClassifier, X_val, y_val, target_precision: float = 0.95
    ) -> float:
        """Find threshold achieving target precision using PR curve."""
        y_proba = model.predict_proba(X_val)[:, 1]
        precisions, recalls, thresholds = precision_recall_curve(y_val, y_proba)

        # Find threshold for target precision
        valid_idx = np.where(precisions >= target_precision)[0]
        if len(valid_idx) == 0:
            print(f"Warning: Cannot achieve precision={target_precision}")
            # Use highest precision threshold
            best_idx = np.argmax(precisions[:-1])
        else:
            # Among those achieving target precision, pick highest recall
            best_idx = valid_idx[np.argmax(recalls[valid_idx])]

        threshold = float(thresholds[best_idx])
        print(f"Calibrated threshold: {threshold:.4f}")
        print(f"At this threshold - Precision: {precisions[best_idx]:.4f}, Recall: {recalls[best_idx]:.4f}")
        return threshold

    def evaluate(self, model, X_test, y_test, threshold: float) -> dict:
        """Full evaluation with classification report."""
        y_proba = model.predict_proba(X_test)[:, 1]
        y_pred = (y_proba >= threshold).astype(int)

        report = classification_report(y_test, y_pred, target_names=["Human", "Bot"], output_dict=True)
        auc_roc = roc_auc_score(y_test, y_proba)
        auc_pr = average_precision_score(y_test, y_proba)

        print("
" + "=" * 60)
        print("EVALUATION RESULTS")
        print("=" * 60)
        print(classification_report(y_test, y_pred, target_names=["Human", "Bot"]))
        print(f"AUC-ROC: {auc_roc:.4f}")
        print(f"AUC-PR: {auc_pr:.4f}")
        print(f"Threshold: {threshold:.4f}")

        return {
            "classification_report": report,
            "auc_roc": auc_roc,
            "auc_pr": auc_pr,
            "threshold": threshold,
            "test_samples": len(y_test),
            "evaluated_at": datetime.utcnow().isoformat(),
        }

    def save_model(self, model, threshold: float, metrics: dict, version: str):
        """Save model with metadata for serving."""
        version_dir = os.path.join(self.model_output_dir, version)
        os.makedirs(version_dir, exist_ok=True)

        # Save model
        model_path = os.path.join(version_dir, "model.joblib")
        joblib.dump(model, model_path)

        # Save metadata
        metadata = {
            "version": version,
            "feature_names": self.FEATURE_NAMES,
            "threshold": threshold,
            "metrics": metrics,
            "trained_at": datetime.utcnow().isoformat(),
            "xgboost_version": xgb.__version__,
            "n_features": len(self.FEATURE_NAMES),
            "best_iteration": model.best_iteration,
        }
        metadata_path = os.path.join(version_dir, "metadata.json")
        with open(metadata_path, "w") as f:
            json.dump(metadata, f, indent=2)

        print(f"Model saved to {version_dir}")
        return version_dir

    def run_full_pipeline(self, version: str = None):
        """Execute the complete training pipeline."""
        if version is None:
            version = datetime.utcnow().strftime("v%Y%m%d_%H%M%S")

        print(f"Training model version: {version}")
        print("=" * 60)

        # Load and prepare
        X, y = self.load_and_prepare_data()
        X_train, X_val, X_test, y_train, y_val, y_test = self.split_data(X, y)

        # SMOTE oversampling on training set only
        X_train_smote, y_train_smote = self.apply_smote(X_train, y_train)

        # Train
        model = self.train_model(X_train_smote, y_train_smote, X_val, y_val)

        # Calibrate threshold for target precision
        threshold = self.calibrate_threshold(model, X_val, y_val, target_precision=0.95)

        # Evaluate on held-out test set
        metrics = self.evaluate(model, X_test, y_test, threshold)

        # Save
        self.save_model(model, threshold, metrics, version)

        return model, threshold, metrics
```

## Hyperparameter Tuning with Optuna

```python
import optuna
from optuna.samplers import TPESampler


class HyperparameterTuner:
    """Optuna-based hyperparameter tuning for XGBoost bot detection."""

    def __init__(self, X_train, y_train, X_val, y_val):
        self.X_train = X_train
        self.y_train = y_train
        self.X_val = X_val
        self.y_val = y_val

    def objective(self, trial: optuna.Trial) -> float:
        """Optuna objective function maximizing AUC-PR."""
        params = {
            "max_depth": trial.suggest_int("max_depth", 3, 10),
            "learning_rate": trial.suggest_float("learning_rate", 0.01, 0.3, log=True),
            "n_estimators": trial.suggest_int("n_estimators", 100, 1000, step=100),
            "min_child_weight": trial.suggest_int("min_child_weight", 1, 10),
            "subsample": trial.suggest_float("subsample", 0.6, 1.0),
            "colsample_bytree": trial.suggest_float("colsample_bytree", 0.6, 1.0),
            "gamma": trial.suggest_float("gamma", 0.0, 5.0),
            "reg_alpha": trial.suggest_float("reg_alpha", 1e-8, 10.0, log=True),
            "reg_lambda": trial.suggest_float("reg_lambda", 1e-8, 10.0, log=True),
            "scale_pos_weight": trial.suggest_float("scale_pos_weight", 1.0, 20.0),
            "objective": "binary:logistic",
            "eval_metric": "aucpr",
            "tree_method": "hist",
            "random_state": 42,
            "n_jobs": -1,
        }

        model = xgb.XGBClassifier(**params)
        model.fit(
            self.X_train, self.y_train,
            eval_set=[(self.X_val, self.y_val)],
            verbose=0,
        )

        y_proba = model.predict_proba(self.X_val)[:, 1]
        auc_pr = average_precision_score(self.y_val, y_proba)
        return auc_pr

    def run_tuning(self, n_trials: int = 20) -> dict:
        """Run Optuna hyperparameter search with TPE sampler."""
        sampler = TPESampler(seed=42)
        study = optuna.create_study(
            direction="maximize",
            sampler=sampler,
            study_name="ghostroute_bot_detection",
        )
        study.optimize(
            self.objective,
            n_trials=n_trials,
            show_progress_bar=True,
        )

        print(f"Best trial AUC-PR: {study.best_value:.4f}")
        print(f"Best params: {study.best_params}")

        return study.best_params


# Usage with trainer
def train_with_tuning(data_path: str, output_dir: str):
    """Full pipeline with Optuna tuning."""
    trainer = BotDetectionTrainer(data_path, output_dir)
    X, y = trainer.load_and_prepare_data()
    X_train, X_val, X_test, y_train, y_val, y_test = trainer.split_data(X, y)
    X_train_smote, y_train_smote = trainer.apply_smote(X_train, y_train)

    # Tune hyperparameters
    tuner = HyperparameterTuner(X_train_smote, y_train_smote, X_val, y_val)
    best_params = tuner.run_tuning(n_trials=20)

    # Train final model with best params
    best_params["eval_metric"] = ["aucpr", "logloss"]
    best_params["objective"] = "binary:logistic"
    best_params["tree_method"] = "hist"
    best_params["random_state"] = 42
    best_params["n_jobs"] = -1

    model = trainer.train_model(X_train_smote, y_train_smote, X_val, y_val, best_params)
    threshold = trainer.calibrate_threshold(model, X_val, y_val)
    metrics = trainer.evaluate(model, X_test, y_test, threshold)

    version = datetime.utcnow().strftime("v%Y%m%d_%H%M%S")
    trainer.save_model(model, threshold, metrics, version)
    return model, threshold, metrics
```

## FastAPI Model Serving with Hot-Swap

```python
import threading
import hashlib
from pathlib import Path
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, BackgroundTasks
from pydantic import BaseModel
import uvicorn


class PredictionRequest(BaseModel):
    """Request body for bot prediction."""
    features: dict  # Feature name -> value mapping
    visitor_id: str = ""  # For A/B testing model routing


class PredictionResponse(BaseModel):
    """Response from bot prediction."""
    probability: float
    is_bot: bool
    threshold: float
    model_version: str
    confidence: str  # "high", "medium", "low"


class ModelInfo(BaseModel):
    """Current model information."""
    version: str
    loaded_at: str
    feature_count: int
    threshold: float
    metrics: dict
    ab_test_active: bool
    ab_test_split: float


class ModelVersion:
    """Container for a loaded model version."""

    def __init__(self, model_dir: str):
        self.model_dir = model_dir
        self.model = None
        self.metadata = None
        self.loaded_at = None
        self._load()

    def _load(self):
        model_path = os.path.join(self.model_dir, "model.joblib")
        metadata_path = os.path.join(self.model_dir, "metadata.json")

        self.model = joblib.load(model_path)
        with open(metadata_path) as f:
            self.metadata = json.load(f)
        self.loaded_at = datetime.utcnow().isoformat()

    @property
    def version(self) -> str:
        return self.metadata["version"]

    @property
    def threshold(self) -> float:
        return self.metadata["threshold"]

    @property
    def feature_names(self) -> list:
        return self.metadata["feature_names"]

    def predict(self, features: dict) -> tuple:
        """Return (probability, is_bot) tuple."""
        feature_vector = np.array([[features.get(f, 0.0) for f in self.feature_names]])
        proba = float(self.model.predict_proba(feature_vector)[0, 1])
        is_bot = proba >= self.threshold
        return proba, is_bot


class ModelManager:
    """Thread-safe model manager with hot-swap and A/B testing."""

    def __init__(self, models_dir: str):
        self.models_dir = models_dir
        self._lock = threading.Lock()
        self._primary_model: ModelVersion = None
        self._secondary_model: ModelVersion = None
        self._ab_split: float = 0.0  # 0 = all primary, 0.5 = 50/50

    def load_model(self, version_dir: str, slot: str = "primary"):
        """Hot-swap a model into the specified slot."""
        new_model = ModelVersion(version_dir)
        with self._lock:
            if slot == "primary":
                self._primary_model = new_model
            else:
                self._secondary_model = new_model
        print(f"Loaded model {new_model.version} into {slot} slot")

    def set_ab_split(self, split: float):
        """Set A/B test traffic split (0.0 to 1.0 for secondary)."""
        with self._lock:
            self._ab_split = max(0.0, min(1.0, split))

    def get_model_for_visitor(self, visitor_id: str) -> ModelVersion:
        """Route visitor to model version based on consistent hashing."""
        with self._lock:
            if self._ab_split <= 0 or self._secondary_model is None:
                return self._primary_model

            # Consistent routing using visitor_id hash
            hash_val = int(hashlib.md5(visitor_id.encode()).hexdigest()[:8], 16)
            bucket = (hash_val % 100) / 100.0

            if bucket < self._ab_split:
                return self._secondary_model
            return self._primary_model

    @property
    def primary(self) -> ModelVersion:
        return self._primary_model

    @property
    def ab_test_active(self) -> bool:
        return self._ab_split > 0 and self._secondary_model is not None


# Global model manager
model_manager: ModelManager = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Load initial model on startup."""
    global model_manager
    models_dir = os.environ.get("MODELS_DIR", "/models")
    model_manager = ModelManager(models_dir)

    # Load latest version
    versions = sorted(Path(models_dir).iterdir(), reverse=True)
    if versions:
        model_manager.load_model(str(versions[0]))
        print(f"Loaded initial model: {model_manager.primary.version}")
    yield


app = FastAPI(title="GhostRoute Bot Detection", version="1.0.0", lifespan=lifespan)


@app.get("/health")
async def health():
    """Health check endpoint."""
    if model_manager is None or model_manager.primary is None:
        raise HTTPException(status_code=503, detail="Model not loaded")
    return {"status": "healthy", "model_version": model_manager.primary.version}


@app.post("/predict", response_model=PredictionResponse)
async def predict(request: PredictionRequest):
    """Predict bot probability for a visitor."""
    if model_manager is None or model_manager.primary is None:
        raise HTTPException(status_code=503, detail="Model not loaded")

    model = model_manager.get_model_for_visitor(request.visitor_id)
    proba, is_bot = model.predict(request.features)

    # Confidence level
    distance_from_threshold = abs(proba - model.threshold)
    if distance_from_threshold > 0.3:
        confidence = "high"
    elif distance_from_threshold > 0.1:
        confidence = "medium"
    else:
        confidence = "low"

    return PredictionResponse(
        probability=proba,
        is_bot=is_bot,
        threshold=model.threshold,
        model_version=model.version,
        confidence=confidence,
    )


@app.post("/model/load")
async def load_model(version_dir: str, slot: str = "primary"):
    """Load a new model version without restart."""
    if not os.path.exists(version_dir):
        raise HTTPException(status_code=404, detail=f"Model dir not found: {version_dir}")
    try:
        model_manager.load_model(version_dir, slot)
        return {"status": "loaded", "version_dir": version_dir, "slot": slot}
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to load model: {str(e)}")


@app.get("/model/info", response_model=ModelInfo)
async def model_info():
    """Return current model version and metrics."""
    if model_manager is None or model_manager.primary is None:
        raise HTTPException(status_code=503, detail="No model loaded")

    primary = model_manager.primary
    return ModelInfo(
        version=primary.version,
        loaded_at=primary.loaded_at,
        feature_count=len(primary.feature_names),
        threshold=primary.threshold,
        metrics=primary.metadata.get("metrics", {}),
        ab_test_active=model_manager.ab_test_active,
        ab_test_split=model_manager._ab_split,
    )


@app.post("/model/ab-test")
async def configure_ab_test(split: float, secondary_dir: str = None):
    """Configure A/B testing between model versions."""
    if secondary_dir:
        model_manager.load_model(secondary_dir, slot="secondary")
    model_manager.set_ab_split(split)
    return {"status": "configured", "split": split}
```

## Background Model Refresh

```python
import asyncio
import boto3
from botocore.exceptions import ClientError


class ModelRefresher:
    """Background task that checks for new model versions."""

    def __init__(self, model_manager: ModelManager, check_interval: int = 300):
        self.model_manager = model_manager
        self.check_interval = check_interval  # seconds
        self.s3_client = boto3.client("s3")
        self.bucket = os.environ.get("MODEL_BUCKET", "ghostroute-models")
        self.prefix = os.environ.get("MODEL_PREFIX", "bot-detection/")
        self._running = False

    async def start(self):
        """Start the background refresh loop."""
        self._running = True
        while self._running:
            try:
                await self._check_for_new_version()
            except Exception as e:
                print(f"Model refresh error: {e}")
            await asyncio.sleep(self.check_interval)

    async def stop(self):
        self._running = False

    async def _check_for_new_version(self):
        """Check S3 for a newer model version."""
        try:
            response = self.s3_client.list_objects_v2(
                Bucket=self.bucket,
                Prefix=self.prefix,
                Delimiter="/",
            )
            versions = sorted(
                [p["Prefix"].rstrip("/").split("/")[-1]
                 for p in response.get("CommonPrefixes", [])],
                reverse=True,
            )

            if not versions:
                return

            latest_version = versions[0]
            current_version = self.model_manager.primary.version if self.model_manager.primary else None

            if latest_version != current_version:
                print(f"New model version found: {latest_version}")
                local_dir = f"/models/{latest_version}"
                await self._download_model(latest_version, local_dir)
                self.model_manager.load_model(local_dir)
                print(f"Model updated to {latest_version}")

        except ClientError as e:
            print(f"S3 error checking for new model: {e}")

    async def _download_model(self, version: str, local_dir: str):
        """Download model files from S3."""
        os.makedirs(local_dir, exist_ok=True)
        prefix = f"{self.prefix}{version}/"

        response = self.s3_client.list_objects_v2(Bucket=self.bucket, Prefix=prefix)
        for obj in response.get("Contents", []):
            key = obj["Key"]
            filename = key.split("/")[-1]
            local_path = os.path.join(local_dir, filename)
            self.s3_client.download_file(self.bucket, key, local_path)
            print(f"Downloaded {filename}")
```

## Model Versioning Strategy

### Directory Structure

```
/models/
  v20250115_143022/
    model.joblib          # Serialized XGBoost model
    metadata.json         # Version info, metrics, threshold, feature names
  v20250116_091545/
    model.joblib
    metadata.json
  latest -> v20250116_091545  # Symlink to latest
```

### metadata.json Schema

```json
{
  "version": "v20250116_091545",
  "feature_names": [
    "hour_sin", "hour_cos", "day_of_week", "is_weekend",
    "clicks_per_minute", "click_burst_score", "total_clicks_1min",
    "fp_entropy", "fp_component_count", "fp_uniqueness_score",
    "missing_headers", "header_count_anomaly",
    "suspicious_header_count", "header_order_score",
    "mouse_velocity_var", "mouse_straightness",
    "click_timing_std", "has_mouse_data",
    "js_exec_time_ms", "js_challenge_completed", "js_timing_suspicious",
    "tls_match_score", "tls_known_browser"
  ],
  "threshold": 0.7234,
  "metrics": {
    "auc_roc": 0.9847,
    "auc_pr": 0.9612,
    "classification_report": {
      "Human": {"precision": 0.98, "recall": 0.99, "f1-score": 0.985},
      "Bot": {"precision": 0.95, "recall": 0.92, "f1-score": 0.935}
    }
  },
  "trained_at": "2025-01-16T09:15:45.123456",
  "xgboost_version": "2.1.3",
  "n_features": 23,
  "best_iteration": 347,
  "training_data": {
    "total_samples": 1250000,
    "bot_ratio": 0.08,
    "smote_applied": true
  }
}
```

## A/B Testing Between Model Versions

```python
class ABTestConfig:
    """Configuration for model A/B testing."""

    def __init__(self, primary_version: str, secondary_version: str, split: float):
        self.primary_version = primary_version
        self.secondary_version = secondary_version
        self.split = split  # Fraction routed to secondary
        self.started_at = datetime.utcnow().isoformat()
        self.results = {"primary": {"predictions": 0, "bots_detected": 0},
                       "secondary": {"predictions": 0, "bots_detected": 0}}

    def record_prediction(self, model_slot: str, is_bot: bool):
        """Record a prediction for analysis."""
        self.results[model_slot]["predictions"] += 1
        if is_bot:
            self.results[model_slot]["bots_detected"] += 1

    def get_stats(self) -> dict:
        """Get current A/B test statistics."""
        stats = {}
        for slot in ["primary", "secondary"]:
            total = self.results[slot]["predictions"]
            bots = self.results[slot]["bots_detected"]
            stats[slot] = {
                "predictions": total,
                "bots_detected": bots,
                "bot_rate": bots / max(total, 1),
            }
        return stats


def route_visitor_to_model(visitor_id: str, ab_config: ABTestConfig) -> str:
    """Deterministically route a visitor to a model version.

    Uses MD5 hash of visitor_id for consistent routing.
    Same visitor always hits the same model version.
    """
    hash_bytes = hashlib.md5(visitor_id.encode()).digest()
    hash_int = int.from_bytes(hash_bytes[:4], byteorder="big")
    bucket = (hash_int % 1000) / 1000.0

    if bucket < ab_config.split:
        return "secondary"
    return "primary"
```

## Dockerfiles

### Training Container

```dockerfile
# Dockerfile.train
FROM python:3.11-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends     libgomp1     && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY train/ ./train/
COPY features/ ./features/

# Model output volume
VOLUME /models

ENV PYTHONUNBUFFERED=1
ENV DATA_PATH=/data/clicks.parquet
ENV MODEL_OUTPUT_DIR=/models

ENTRYPOINT ["python", "-m", "train.pipeline"]
CMD ["--tune", "--n-trials", "20"]
```

### Serving Container

```dockerfile
# Dockerfile.serve
FROM python:3.11-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends     libgomp1 curl     && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY serve/ ./serve/

# Models volume
VOLUME /models

ENV PYTHONUNBUFFERED=1
ENV MODELS_DIR=/models
ENV MODEL_BUCKET=ghostroute-models
ENV MODEL_PREFIX=bot-detection/
ENV REFRESH_INTERVAL=300

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --retries=3     CMD curl -f http://localhost:8000/health || exit 1

ENTRYPOINT ["uvicorn", "serve.app:app", "--host", "0.0.0.0", "--port", "8000", "--workers", "4"]
```

## Threshold Calibration

### Precision-Recall Analysis

```python
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt


def analyze_threshold(model, X_val, y_val, output_path: str = "pr_curve.png"):
    """Generate precision-recall curve and find optimal threshold."""
    y_proba = model.predict_proba(X_val)[:, 1]
    precisions, recalls, thresholds = precision_recall_curve(y_val, y_proba)

    # Find thresholds for different precision targets
    targets = [0.90, 0.95, 0.99]
    print("Threshold Analysis:")
    print("-" * 50)
    print(f"{'Target Precision':<20}{'Threshold':<12}{'Actual Prec':<14}{'Recall':<10}")
    print("-" * 50)

    for target in targets:
        valid_idx = np.where(precisions[:-1] >= target)[0]
        if len(valid_idx) > 0:
            best_idx = valid_idx[np.argmax(recalls[valid_idx])]
            print(f"{target:<20.2f}{thresholds[best_idx]:<12.4f}"
                  f"{precisions[best_idx]:<14.4f}{recalls[best_idx]:<10.4f}")

    # Plot PR curve
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(14, 5))

    ax1.plot(recalls, precisions, "b-", linewidth=2)
    ax1.axhline(y=0.95, color="r", linestyle="--", label="Target precision=0.95")
    ax1.set_xlabel("Recall")
    ax1.set_ylabel("Precision")
    ax1.set_title("Precision-Recall Curve")
    ax1.legend()
    ax1.grid(True, alpha=0.3)

    # Threshold vs F1
    f1_scores = 2 * (precisions[:-1] * recalls[:-1]) / (precisions[:-1] + recalls[:-1] + 1e-8)
    ax2.plot(thresholds, f1_scores, "g-", linewidth=2)
    ax2.set_xlabel("Threshold")
    ax2.set_ylabel("F1 Score")
    ax2.set_title("Threshold vs F1 Score")
    ax2.grid(True, alpha=0.3)

    plt.tight_layout()
    plt.savefig(output_path, dpi=150, bbox_inches="tight")
    print(f"PR curve saved to {output_path}")
```

## Example Evaluation Output

```
============================================================
EVALUATION RESULTS
============================================================
              precision    recall  f1-score   support

       Human       0.99      0.99      0.99    230000
         Bot       0.95      0.92      0.94     20000

    accuracy                           0.98    250000
   macro avg       0.97      0.96      0.96    250000
weighted avg       0.98      0.98      0.98    250000

AUC-ROC: 0.9847
AUC-PR: 0.9612
Threshold: 0.7234

Threshold Analysis:
--------------------------------------------------
Target Precision    Threshold   Actual Prec   Recall
--------------------------------------------------
0.90                0.5812      0.9023        0.9456
0.95                0.7234      0.9512        0.9198
0.99                0.9156      0.9907        0.7234
```

## Running the Pipeline

```bash
# Train with default parameters
docker build -f Dockerfile.train -t ghostroute-ml-train .
docker run -v ./data:/data -v ./models:/models ghostroute-ml-train

# Train with Optuna tuning
docker run -v ./data:/data -v ./models:/models ghostroute-ml-train --tune --n-trials 20

# Start serving
docker build -f Dockerfile.serve -t ghostroute-ml-serve .
docker run -p 8000:8000 -v ./models:/models ghostroute-ml-serve

# Test prediction
curl -X POST http://localhost:8000/predict   -H "Content-Type: application/json"   -d ''{
    "features": {
      "hour_sin": 0.5,
      "hour_cos": 0.866,
      "clicks_per_minute": 12.5,
      "fp_entropy": 3.2,
      "missing_headers": 2.0,
      "mouse_velocity_var": 0.001,
      "js_exec_time_ms": 25.0,
      "tls_match_score": 0.3
    },
    "visitor_id": "abc123"
  }'' 

# Hot-swap model
curl -X POST "http://localhost:8000/model/load?version_dir=/models/v20250116_091545&slot=primary"

# Configure A/B test (20% traffic to new model)
curl -X POST "http://localhost:8000/model/ab-test?split=0.2&secondary_dir=/models/v20250117_082030"

# Check model info
curl http://localhost:8000/model/info
```
