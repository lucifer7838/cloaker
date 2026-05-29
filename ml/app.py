"""
app.py - FastAPI application for GhostRoute bot detection model serving.

Provides endpoints for prediction, model hot-swap, and A/B testing.
"""

import hashlib
import json
import os
import threading
from contextlib import asynccontextmanager
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, Optional

import joblib
import numpy as np
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel


class PredictionRequest(BaseModel):
    """Request body for bot prediction."""

    features: Dict[str, float]
    visitor_id: str = ""


class PredictionResponse(BaseModel):
    """Response from bot prediction."""

    probability: float
    is_bot: bool
    threshold: float
    model_version: str
    confidence: str


class ModelInfo(BaseModel):
    """Current model information."""

    version: str
    loaded_at: str
    feature_count: int
    threshold: float
    metrics: Dict[str, Any]
    ab_test_active: bool
    ab_test_split: float


class ModelVersion:
    """Container for a loaded model version."""

    def __init__(self, model_dir: str):
        self.model_dir = model_dir
        self.model = None
        self.metadata: Dict[str, Any] = {}
        self.loaded_at: str = ""
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
        return self.metadata.get("version", "unknown")

    @property
    def threshold(self) -> float:
        return self.metadata.get("threshold", 0.5)

    @property
    def feature_names(self) -> list:
        return self.metadata.get("feature_names", [])

    def predict(self, features: Dict[str, float]) -> tuple:
        """Return (probability, is_bot) tuple."""
        feature_vector = np.array(
            [[features.get(f, 0.0) for f in self.feature_names]]
        )
        proba = float(self.model.predict_proba(feature_vector)[0, 1])
        is_bot = proba >= self.threshold
        return proba, is_bot


class ModelManager:
    """Thread-safe model manager with hot-swap and A/B testing."""

    def __init__(self, models_dir: str):
        self.models_dir = models_dir
        self._lock = threading.Lock()
        self._primary_model: Optional[ModelVersion] = None
        self._secondary_model: Optional[ModelVersion] = None
        self._ab_split: float = 0.0

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

    def get_model_for_visitor(self, visitor_id: str) -> Optional[ModelVersion]:
        """Route visitor to model version based on consistent hashing."""
        with self._lock:
            if self._ab_split <= 0 or self._secondary_model is None:
                return self._primary_model

            hash_val = int(
                hashlib.md5(visitor_id.encode()).hexdigest()[:8], 16
            )
            bucket = (hash_val % 100) / 100.0

            if bucket < self._ab_split:
                return self._secondary_model
            return self._primary_model

    @property
    def primary(self) -> Optional[ModelVersion]:
        return self._primary_model

    @property
    def ab_test_active(self) -> bool:
        return self._ab_split > 0 and self._secondary_model is not None


model_manager: Optional[ModelManager] = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Load initial model on startup."""
    global model_manager
    models_dir = os.environ.get("MODELS_DIR", "/models")
    model_manager = ModelManager(models_dir)

    models_path = Path(models_dir)
    if models_path.exists():
        versions = sorted(
            [d for d in models_path.iterdir() if d.is_dir()], reverse=True
        )
        if versions:
            try:
                model_manager.load_model(str(versions[0]))
                print(f"Loaded initial model: {model_manager.primary.version}")
            except Exception as e:
                print(f"Warning: failed to load initial model: {e}")
    yield


app = FastAPI(
    title="GhostRoute Bot Detection", version="1.0.0", lifespan=lifespan
)


@app.get("/health")
async def health():
    """Health check endpoint."""
    if model_manager is None or model_manager.primary is None:
        return {"status": "healthy", "model_version": "none"}
    return {"status": "healthy", "model_version": model_manager.primary.version}


@app.post("/predict", response_model=PredictionResponse)
async def predict(request: PredictionRequest):
    """Predict bot probability for a visitor."""
    if model_manager is None or model_manager.primary is None:
        raise HTTPException(status_code=503, detail="Model not loaded")

    model = model_manager.get_model_for_visitor(request.visitor_id)
    if model is None:
        raise HTTPException(status_code=503, detail="Model not available")

    proba, is_bot = model.predict(request.features)

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
    if model_manager is None:
        raise HTTPException(status_code=503, detail="Model manager not initialized")
    if not os.path.exists(version_dir):
        raise HTTPException(
            status_code=404, detail=f"Model dir not found: {version_dir}"
        )
    try:
        model_manager.load_model(version_dir, slot)
        return {"status": "loaded", "version_dir": version_dir, "slot": slot}
    except Exception as e:
        raise HTTPException(
            status_code=500, detail=f"Failed to load model: {str(e)}"
        )


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
    if model_manager is None:
        raise HTTPException(status_code=503, detail="Model manager not initialized")
    if secondary_dir:
        model_manager.load_model(secondary_dir, slot="secondary")
    model_manager.set_ab_split(split)
    return {"status": "configured", "split": split}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8000, workers=1)
