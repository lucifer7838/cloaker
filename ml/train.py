"""
train.py - Complete XGBoost training pipeline for GhostRoute bot detection.

Includes feature engineering, SMOTE oversampling (after split only),
hyperparameter tuning with Optuna, and model evaluation.
"""

import json
import os
from datetime import datetime
from typing import Any, Dict, List

import joblib
import numpy as np
import optuna
import pandas as pd
import xgboost as xgb
from imblearn.over_sampling import SMOTE
from optuna.samplers import TPESampler
from scipy.stats import entropy
from sklearn.metrics import (
    average_precision_score,
    classification_report,
    precision_recall_curve,
    roc_auc_score,
)
from sklearn.model_selection import train_test_split


class FeatureEngineer:
    """Extract features from raw click data for bot detection."""

    KNOWN_BROWSER_JA3 = {
        "771,4865-4866-4867-49195-49199-49196-49200-52393-52392": "Chrome 120+",
        "771,4865-4867-4866-49195-49199-52393-52392-49196-49200": "Firefox 121+",
        "771,4865-4866-4867-49196-49195-52393-49200-49199-52392": "Safari 17+",
    }

    def extract_features(self, click_data: Dict[str, Any]) -> Dict[str, float]:
        """Extract all features from a single click event."""
        features = {}
        features.update(self._time_of_day_features(click_data.get("timestamp", 0)))
        features.update(self._request_rate_features(click_data))
        features.update(
            self._fingerprint_entropy(click_data.get("fingerprint", {}))
        )
        features.update(
            self._header_anomaly_score(click_data.get("headers", {}))
        )
        features.update(
            self._mouse_movement_features(click_data.get("mouse_data", []))
        )
        features.update(
            self._js_timing_features(click_data.get("js_timing", {}))
        )
        features.update(
            self._tls_fingerprint_score(click_data.get("ja3", ""))
        )
        return features

    def _time_of_day_features(self, timestamp: float) -> Dict[str, float]:
        """Cyclical encoding of hour using sin/cos transformation."""
        dt = datetime.fromtimestamp(timestamp) if timestamp else datetime.utcnow()
        hour = dt.hour + dt.minute / 60.0
        return {
            "hour_sin": float(np.sin(2 * np.pi * hour / 24.0)),
            "hour_cos": float(np.cos(2 * np.pi * hour / 24.0)),
            "day_of_week": float(dt.weekday()),
            "is_weekend": float(dt.weekday() >= 5),
        }

    def _request_rate_features(self, click_data: Dict) -> Dict[str, float]:
        """Calculate request rate from same IP/fingerprint in rolling window."""
        recent_clicks = click_data.get("recent_clicks_same_ip", [])
        if len(recent_clicks) < 2:
            return {
                "clicks_per_minute": 0.0,
                "click_burst_score": 0.0,
                "total_clicks_1min": 0.0,
            }

        timestamps = sorted(recent_clicks)
        time_span = (timestamps[-1] - timestamps[0]) / 60.0
        clicks_per_minute = len(timestamps) / max(time_span, 0.01)

        intervals = np.diff(timestamps)
        burst_score = (
            1.0 / (float(np.std(intervals)) + 0.01)
            if len(intervals) > 1
            else 0.0
        )

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
            return {
                "fp_entropy": 0.0,
                "fp_component_count": 0.0,
                "fp_uniqueness_score": 0.0,
            }

        char_counts = np.array([combined.count(c) for c in set(combined)])
        char_probs = char_counts / char_counts.sum()
        fp_entropy = float(entropy(char_probs, base=2))

        non_empty = sum(1 for c in components if c)

        return {
            "fp_entropy": fp_entropy,
            "fp_component_count": float(non_empty),
            "fp_uniqueness_score": fp_entropy / max(len(set(combined)), 1),
        }

    def _header_anomaly_score(self, headers: Dict[str, str]) -> Dict[str, float]:
        """Score anomalies in HTTP headers."""
        expected_headers = [
            "user-agent",
            "accept",
            "accept-language",
            "accept-encoding",
            "connection",
            "host",
        ]
        missing_count = sum(1 for h in expected_headers if h not in headers)

        header_count = len(headers)
        count_anomaly = abs(header_count - 11) / 11.0

        suspicious = ["x-forwarded-for", "via", "x-real-ip"]
        suspicious_count = sum(1 for h in suspicious if h in headers)

        header_keys = list(headers.keys())
        order_score = 0.0
        if header_keys and header_keys[0].lower() != "host":
            order_score += 0.3
        if "user-agent" in headers:
            try:
                idx = list(headers.keys()).index("user-agent")
                if idx > 5:
                    order_score += 0.2
            except ValueError:
                pass

        return {
            "missing_headers": float(missing_count),
            "header_count_anomaly": count_anomaly,
            "suspicious_header_count": float(suspicious_count),
            "header_order_score": order_score,
        }

    def _mouse_movement_features(
        self, mouse_data: List[Dict]
    ) -> Dict[str, float]:
        """Extract features from mouse movement telemetry."""
        if not mouse_data or len(mouse_data) < 3:
            return {
                "mouse_velocity_var": 0.0,
                "mouse_straightness": 1.0,
                "click_timing_std": 0.0,
                "has_mouse_data": 0.0,
            }

        points = [(m["x"], m["y"], m["t"]) for m in mouse_data]

        velocities = []
        for i in range(1, len(points)):
            dx = points[i][0] - points[i - 1][0]
            dy = points[i][1] - points[i - 1][1]
            dt = max(points[i][2] - points[i - 1][2], 1)
            velocity = np.sqrt(dx**2 + dy**2) / dt
            velocities.append(velocity)

        velocity_var = float(np.var(velocities)) if velocities else 0.0

        total_path = sum(
            np.sqrt(
                (points[i][0] - points[i - 1][0]) ** 2
                + (points[i][1] - points[i - 1][1]) ** 2
            )
            for i in range(1, len(points))
        )
        direct_dist = np.sqrt(
            (points[-1][0] - points[0][0]) ** 2
            + (points[-1][1] - points[0][1]) ** 2
        )
        straightness = direct_dist / max(total_path, 0.01)

        click_times = [m["t"] for m in mouse_data if m.get("click")]
        click_timing_std = (
            float(np.std(np.diff(click_times))) if len(click_times) > 1 else 0.0
        )

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

        suspicious = 0.0
        if exec_time < 50 and completed:
            suspicious = 0.8
        elif exec_time > 30000:
            suspicious = 0.6
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

        if ja3 in self.KNOWN_BROWSER_JA3:
            return {"tls_match_score": 1.0, "tls_known_browser": 1.0}

        ja3_parts = ja3.split(",")
        best_match = 0.0
        for known_ja3 in self.KNOWN_BROWSER_JA3:
            known_parts = known_ja3.split(",")
            if ja3_parts[0] == known_parts[0]:
                client_ciphers = (
                    set(ja3_parts[1].split("-")) if len(ja3_parts) > 1 else set()
                )
                known_ciphers = (
                    set(known_parts[1].split("-"))
                    if len(known_parts) > 1
                    else set()
                )
                if client_ciphers and known_ciphers:
                    overlap = len(client_ciphers & known_ciphers) / len(
                        known_ciphers
                    )
                    best_match = max(best_match, overlap)

        return {"tls_match_score": best_match, "tls_known_browser": 0.0}


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
        "hour_sin",
        "hour_cos",
        "day_of_week",
        "is_weekend",
        "clicks_per_minute",
        "click_burst_score",
        "total_clicks_1min",
        "fp_entropy",
        "fp_component_count",
        "fp_uniqueness_score",
        "missing_headers",
        "header_count_anomaly",
        "suspicious_header_count",
        "header_order_score",
        "mouse_velocity_var",
        "mouse_straightness",
        "click_timing_std",
        "has_mouse_data",
        "js_exec_time_ms",
        "js_challenge_completed",
        "js_timing_suspicious",
        "tls_match_score",
        "tls_known_browser",
    ]

    def __init__(self, data_path: str, model_output_dir: str):
        self.data_path = data_path
        self.model_output_dir = model_output_dir
        self.feature_engineer = FeatureEngineer()

    def load_and_prepare_data(self) -> tuple:
        """Load click data and extract features."""
        df = pd.read_parquet(self.data_path)

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
            features_list.append(
                self.feature_engineer.extract_features(click_data)
            )

        X = pd.DataFrame(features_list, columns=self.FEATURE_NAMES)
        y = df["is_bot"].values

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
        print(
            f"Train: {X_train.shape[0]}, Val: {X_val.shape[0]}, Test: {X_test.shape[0]}"
        )
        return X_train, X_val, X_test, y_train, y_val, y_test

    def apply_smote(
        self, X_train: pd.DataFrame, y_train: np.ndarray
    ) -> tuple:
        """Apply SMOTE oversampling to minority class (bots). Only on training set."""
        smote = SMOTE(
            sampling_strategy=0.5,
            k_neighbors=5,
            random_state=42,
            n_jobs=-1,
        )
        X_resampled, y_resampled = smote.fit_resample(X_train, y_train)
        print(f"Before SMOTE: {X_train.shape[0]} samples")
        print(f"After SMOTE: {X_resampled.shape[0]} samples")
        print(
            f"New class distribution: bots={y_resampled.sum()}, humans={(1-y_resampled).sum()}"
        )
        return X_resampled, y_resampled

    def train_model(
        self, X_train, y_train, X_val, y_val, params: dict = None
    ) -> xgb.XGBClassifier:
        """Train XGBoost model with early stopping."""
        if params is None:
            params = self.DEFAULT_PARAMS.copy()

        n_negative = (y_train == 0).sum()
        n_positive = (y_train == 1).sum()
        params["scale_pos_weight"] = n_negative / max(n_positive, 1)

        model = xgb.XGBClassifier(**params)
        model.fit(
            X_train,
            y_train,
            eval_set=[(X_val, y_val)],
            verbose=50,
        )

        print(f"Best iteration: {model.best_iteration}")
        print(f"Best score: {model.best_score:.6f}")
        return model

    def calibrate_threshold(
        self,
        model: xgb.XGBClassifier,
        X_val,
        y_val,
        target_precision: float = 0.95,
    ) -> float:
        """Find threshold achieving target precision using PR curve."""
        y_proba = model.predict_proba(X_val)[:, 1]
        precisions, recalls, thresholds = precision_recall_curve(
            y_val, y_proba
        )

        valid_idx = np.where(precisions >= target_precision)[0]
        if len(valid_idx) == 0:
            print(f"Warning: Cannot achieve precision={target_precision}")
            best_idx = np.argmax(precisions[:-1])
        else:
            best_idx = valid_idx[np.argmax(recalls[valid_idx])]

        threshold = float(thresholds[best_idx])
        print(f"Calibrated threshold: {threshold:.4f}")
        print(
            f"At this threshold - Precision: {precisions[best_idx]:.4f}, "
            f"Recall: {recalls[best_idx]:.4f}"
        )
        return threshold

    def evaluate(self, model, X_test, y_test, threshold: float) -> dict:
        """Full evaluation with classification report."""
        y_proba = model.predict_proba(X_test)[:, 1]
        y_pred = (y_proba >= threshold).astype(int)

        report = classification_report(
            y_test, y_pred, target_names=["Human", "Bot"], output_dict=True
        )
        auc_roc = roc_auc_score(y_test, y_proba)
        auc_pr = average_precision_score(y_test, y_proba)

        print("\n" + "=" * 60)
        print("EVALUATION RESULTS")
        print("=" * 60)
        print(
            classification_report(
                y_test, y_pred, target_names=["Human", "Bot"]
            )
        )
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

    def save_model(
        self, model, threshold: float, metrics: dict, version: str
    ):
        """Save model with metadata for serving."""
        version_dir = os.path.join(self.model_output_dir, version)
        os.makedirs(version_dir, exist_ok=True)

        model_path = os.path.join(version_dir, "model.joblib")
        joblib.dump(model, model_path)

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

        X, y = self.load_and_prepare_data()
        X_train, X_val, X_test, y_train, y_val, y_test = self.split_data(X, y)

        # SMOTE oversampling on training set only
        X_train_smote, y_train_smote = self.apply_smote(X_train, y_train)

        model = self.train_model(X_train_smote, y_train_smote, X_val, y_val)

        threshold = self.calibrate_threshold(
            model, X_val, y_val, target_precision=0.95
        )

        metrics = self.evaluate(model, X_test, y_test, threshold)

        self.save_model(model, threshold, metrics, version)

        return model, threshold, metrics


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
            "learning_rate": trial.suggest_float(
                "learning_rate", 0.01, 0.3, log=True
            ),
            "n_estimators": trial.suggest_int(
                "n_estimators", 100, 1000, step=100
            ),
            "min_child_weight": trial.suggest_int("min_child_weight", 1, 10),
            "subsample": trial.suggest_float("subsample", 0.6, 1.0),
            "colsample_bytree": trial.suggest_float(
                "colsample_bytree", 0.6, 1.0
            ),
            "gamma": trial.suggest_float("gamma", 0.0, 5.0),
            "reg_alpha": trial.suggest_float(
                "reg_alpha", 1e-8, 10.0, log=True
            ),
            "reg_lambda": trial.suggest_float(
                "reg_lambda", 1e-8, 10.0, log=True
            ),
            "scale_pos_weight": trial.suggest_float(
                "scale_pos_weight", 1.0, 20.0
            ),
            "objective": "binary:logistic",
            "eval_metric": "aucpr",
            "tree_method": "hist",
            "random_state": 42,
            "n_jobs": -1,
        }

        model = xgb.XGBClassifier(**params)
        model.fit(
            self.X_train,
            self.y_train,
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


def train_with_tuning(data_path: str, output_dir: str):
    """Full pipeline with Optuna tuning."""
    trainer = BotDetectionTrainer(data_path, output_dir)
    X, y = trainer.load_and_prepare_data()
    X_train, X_val, X_test, y_train, y_val, y_test = trainer.split_data(X, y)
    X_train_smote, y_train_smote = trainer.apply_smote(X_train, y_train)

    tuner = HyperparameterTuner(X_train_smote, y_train_smote, X_val, y_val)
    best_params = tuner.run_tuning(n_trials=20)

    best_params["eval_metric"] = ["aucpr", "logloss"]
    best_params["objective"] = "binary:logistic"
    best_params["tree_method"] = "hist"
    best_params["random_state"] = 42
    best_params["n_jobs"] = -1

    model = trainer.train_model(
        X_train_smote, y_train_smote, X_val, y_val, best_params
    )
    threshold = trainer.calibrate_threshold(model, X_val, y_val)
    metrics = trainer.evaluate(model, X_test, y_test, threshold)

    version = datetime.utcnow().strftime("v%Y%m%d_%H%M%S")
    trainer.save_model(model, threshold, metrics, version)
    return model, threshold, metrics


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="Train bot detection model")
    parser.add_argument(
        "--data", required=True, help="Path to training data parquet file"
    )
    parser.add_argument(
        "--output", default="/models", help="Output directory for model"
    )
    parser.add_argument("--version", default=None, help="Model version string")
    parser.add_argument(
        "--tune", action="store_true", help="Run hyperparameter tuning"
    )
    args = parser.parse_args()

    if args.tune:
        train_with_tuning(args.data, args.output)
    else:
        trainer = BotDetectionTrainer(args.data, args.output)
        trainer.run_full_pipeline(version=args.version)
