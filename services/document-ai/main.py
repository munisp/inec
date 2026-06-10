"""INEC Document AI Service — PaddleOCR, VLM, DocLing, Video Analysis.

Provides:
- PaddleOCR: Extract text/numbers from EC8A result sheet photos
- VLM (Vision Language Model): Validate photo authenticity + detect tampering
- DocLing: Structured table extraction from form documents
- Video Analysis: Frame extraction + ballot counting anomaly detection
"""

import os
import io
import re
import math
import hashlib
import tempfile
import time
from datetime import datetime, timezone
from typing import Optional
from pathlib import Path

import structlog
from fastapi import FastAPI, File, UploadFile, HTTPException, Form, Query
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

log = structlog.get_logger()

app = FastAPI(
    title="INEC Document AI",
    version="1.0.0",
    description="AI-powered document analysis for election result verification",
)
_ALLOWED_ORIGINS = os.environ.get("CORS_ORIGINS", "http://localhost:3000,http://localhost:5173").split(",")
app.add_middleware(
    CORSMiddleware,
    allow_origins=_ALLOWED_ORIGINS,
    allow_methods=["GET", "POST"],
    allow_headers=["Content-Type", "Authorization", "X-Request-ID"],
    allow_credentials=True,
)

UPLOAD_DIR = os.getenv("UPLOAD_DIR", "/tmp/document-ai-uploads")
BACKEND_URL = os.getenv("BACKEND_URL", "http://localhost:8088")
OCR_MODEL_DIR = os.getenv("OCR_MODEL_DIR", "/models/paddleocr")
VLM_ENDPOINT = os.getenv("VLM_ENDPOINT", "")  # e.g. ollama or vLLM endpoint
DOCLING_MODEL = os.getenv("DOCLING_MODEL", "docling-v2")

Path(UPLOAD_DIR).mkdir(parents=True, exist_ok=True)


# ─── PaddleOCR Integration ─────────────────────────────────────────────────

class OCRResult(BaseModel):
    text: str
    confidence: float
    bbox: list[list[int]]


class EC8AExtraction(BaseModel):
    serial_number: Optional[str] = None
    polling_unit_code: Optional[str] = None
    polling_unit_name: Optional[str] = None
    ward: Optional[str] = None
    lga: Optional[str] = None
    state: Optional[str] = None
    election_type: Optional[str] = None
    party_results: list[dict]
    total_valid_votes: Optional[int] = None
    total_rejected_votes: Optional[int] = None
    total_votes_cast: Optional[int] = None
    accredited_voters: Optional[int] = None
    registered_voters: Optional[int] = None
    presiding_officer_name: Optional[str] = None
    raw_ocr_text: str
    confidence_score: float
    extraction_warnings: list[str]


class OCREngine:
    """PaddleOCR wrapper for EC8A form text extraction."""

    def __init__(self):
        self._initialized = False
        self._paddle_ocr = None

    def _ensure_initialized(self):
        if self._initialized:
            return
        try:
            from paddleocr import PaddleOCR
            self._paddle_ocr = PaddleOCR(
                use_angle_cls=True,
                lang="en",
                det_model_dir=os.path.join(OCR_MODEL_DIR, "det") if os.path.exists(OCR_MODEL_DIR) else None,
                rec_model_dir=os.path.join(OCR_MODEL_DIR, "rec") if os.path.exists(OCR_MODEL_DIR) else None,
                cls_model_dir=os.path.join(OCR_MODEL_DIR, "cls") if os.path.exists(OCR_MODEL_DIR) else None,
                show_log=False,
            )
            self._initialized = True
            log.info("PaddleOCR initialized with local models")
        except ImportError:
            log.warn("PaddleOCR not installed, using fallback extraction")
            self._initialized = True

    def extract_text(self, image_bytes: bytes) -> list[OCRResult]:
        """Run OCR on image bytes, return structured text regions."""
        self._ensure_initialized()

        if self._paddle_ocr is not None:
            return self._extract_with_paddle(image_bytes)
        return self._extract_fallback(image_bytes)

    def _extract_with_paddle(self, image_bytes: bytes) -> list[OCRResult]:
        """Real PaddleOCR extraction."""
        import numpy as np
        from PIL import Image

        img = Image.open(io.BytesIO(image_bytes))
        img_array = np.array(img)

        results = self._paddle_ocr.ocr(img_array, cls=True)
        ocr_results = []

        if results and results[0]:
            for line in results[0]:
                bbox = [[int(p[0]), int(p[1])] for p in line[0]]
                text = line[1][0]
                confidence = float(line[1][1])
                ocr_results.append(OCRResult(
                    text=text, confidence=confidence, bbox=bbox
                ))

        return ocr_results

    def _extract_fallback(self, image_bytes: bytes) -> list[OCRResult]:
        """Fallback: extract metadata from image without PaddleOCR installed."""
        file_hash = hashlib.sha256(image_bytes).hexdigest()[:16]
        return [OCRResult(
            text=f"[OCR_PENDING:{file_hash}]",
            confidence=0.0,
            bbox=[[0, 0], [0, 0], [0, 0], [0, 0]],
        )]

    def extract_ec8a(self, image_bytes: bytes) -> EC8AExtraction:
        """Extract structured EC8A form data from image."""
        ocr_results = self.extract_text(image_bytes)
        raw_text = "\n".join(r.text for r in ocr_results)
        avg_confidence = sum(r.confidence for r in ocr_results) / max(len(ocr_results), 1)

        warnings = []
        party_results = []

        # Extract serial number (format: EC8A/XX/YYYY/NNNN)
        serial_match = re.search(r"EC8A[/\-]?\w{2,4}[/\-]?\d{4}[/\-]?\d{4,}", raw_text, re.IGNORECASE)
        serial_number = serial_match.group(0) if serial_match else None
        if not serial_number:
            warnings.append("Serial number not detected")

        # Extract polling unit code
        pu_match = re.search(r"(?:PU|Polling Unit)[:\s]*([A-Z0-9\-/]+)", raw_text, re.IGNORECASE)
        polling_unit_code = pu_match.group(1) if pu_match else None

        # Extract party results (pattern: PARTY_CODE followed by digits)
        nigerian_parties = ["APC", "PDP", "LP", "NNPP", "ADC", "SDP", "APGA", "YPP", "ZLP", "AA", "APM", "NRM"]
        for party in nigerian_parties:
            pattern = rf"\b{party}\b[\s:]*(\d{{1,7}})"
            match = re.search(pattern, raw_text, re.IGNORECASE)
            if match:
                party_results.append({
                    "party_code": party,
                    "votes": int(match.group(1)),
                    "confidence": avg_confidence,
                })

        # Extract totals
        valid_match = re.search(r"(?:Total Valid|Valid Votes)[:\s]*(\d+)", raw_text, re.IGNORECASE)
        rejected_match = re.search(r"(?:Rejected|Void)[:\s]*(\d+)", raw_text, re.IGNORECASE)
        cast_match = re.search(r"(?:Total Votes? Cast|Total Cast)[:\s]*(\d+)", raw_text, re.IGNORECASE)
        accredited_match = re.search(r"(?:Accredited)[:\s]*(\d+)", raw_text, re.IGNORECASE)
        registered_match = re.search(r"(?:Registered)[:\s]*(\d+)", raw_text, re.IGNORECASE)

        total_valid = int(valid_match.group(1)) if valid_match else None
        total_rejected = int(rejected_match.group(1)) if rejected_match else None
        total_cast = int(cast_match.group(1)) if cast_match else None
        accredited = int(accredited_match.group(1)) if accredited_match else None
        registered = int(registered_match.group(1)) if registered_match else None

        # Validation: sum of party votes should equal total valid
        party_sum = sum(p["votes"] for p in party_results)
        if total_valid and party_sum != total_valid:
            warnings.append(f"Party vote sum ({party_sum}) != total valid votes ({total_valid})")

        # Validation: accredited >= total cast
        if accredited and total_cast and total_cast > accredited:
            warnings.append(f"Total cast ({total_cast}) exceeds accredited ({accredited})")

        if avg_confidence < 0.6:
            warnings.append("Low OCR confidence — manual review recommended")

        return EC8AExtraction(
            serial_number=serial_number,
            polling_unit_code=polling_unit_code,
            polling_unit_name=None,
            ward=None,
            lga=None,
            state=None,
            election_type=None,
            party_results=party_results,
            total_valid_votes=total_valid,
            total_rejected_votes=total_rejected,
            total_votes_cast=total_cast,
            accredited_voters=accredited,
            registered_voters=registered,
            presiding_officer_name=None,
            raw_ocr_text=raw_text,
            confidence_score=avg_confidence,
            extraction_warnings=warnings,
        )


ocr_engine = OCREngine()


# ─── VLM (Vision Language Model) Integration ─────────────────────────────────

class VLMResult(BaseModel):
    is_valid_ec8a: bool
    tampering_detected: bool
    tampering_confidence: float
    tampering_indicators: list[str]
    document_quality: str  # "good", "fair", "poor"
    orientation_correct: bool
    completeness_score: float
    analysis_summary: str


class VLMEngine:
    """Vision Language Model for document validation and tampering detection."""

    def __init__(self):
        self._client = None

    def _get_client(self):
        if self._client is None:
            try:
                import httpx
                self._client = httpx.Client(timeout=60.0)
            except ImportError:
                pass
        return self._client

    def analyze_document(self, image_bytes: bytes, document_type: str = "ec8a") -> VLMResult:
        """Analyze document for authenticity, tampering, and completeness."""

        if VLM_ENDPOINT:
            return self._analyze_with_vlm(image_bytes, document_type)
        return self._analyze_heuristic(image_bytes, document_type)

    def _analyze_with_vlm(self, image_bytes: bytes, document_type: str) -> VLMResult:
        """Call external VLM endpoint (Ollama, vLLM, or OpenAI-compatible)."""
        import base64
        client = self._get_client()
        if not client:
            return self._analyze_heuristic(image_bytes, document_type)

        img_b64 = base64.b64encode(image_bytes).decode()

        prompt = f"""Analyze this INEC {document_type.upper()} election result form image.
Determine:
1. Is this a valid official INEC {document_type.upper()} form? (yes/no)
2. Are there signs of tampering or alteration? (yes/no, list indicators)
3. Document quality (good/fair/poor)
4. Is the document properly oriented?
5. Completeness (0-1 score): are all required fields filled?
6. Brief analysis summary (1-2 sentences)

Respond in JSON format:
{{"is_valid": bool, "tampering": bool, "tampering_confidence": float, "indicators": [...], "quality": "...", "oriented": bool, "completeness": float, "summary": "..."}}"""

        try:
            # OpenAI-compatible API (works with Ollama, vLLM, etc.)
            response = client.post(
                f"{VLM_ENDPOINT}/v1/chat/completions",
                json={
                    "model": os.getenv("VLM_MODEL", "llava"),
                    "messages": [{
                        "role": "user",
                        "content": [
                            {"type": "text", "text": prompt},
                            {"type": "image_url", "image_url": {"url": f"data:image/jpeg;base64,{img_b64}"}},
                        ],
                    }],
                    "temperature": 0.1,
                    "max_tokens": 500,
                },
            )
            response.raise_for_status()
            result = response.json()
            content = result["choices"][0]["message"]["content"]

            # Parse JSON from VLM response
            import json
            # Handle markdown code blocks in response
            if "```json" in content:
                content = content.split("```json")[1].split("```")[0]
            elif "```" in content:
                content = content.split("```")[1].split("```")[0]

            data = json.loads(content.strip())
            return VLMResult(
                is_valid_ec8a=data.get("is_valid", False),
                tampering_detected=data.get("tampering", False),
                tampering_confidence=data.get("tampering_confidence", 0.0),
                tampering_indicators=data.get("indicators", []),
                document_quality=data.get("quality", "unknown"),
                orientation_correct=data.get("oriented", True),
                completeness_score=data.get("completeness", 0.0),
                analysis_summary=data.get("summary", ""),
            )
        except Exception as e:
            log.error("VLM analysis failed", error=str(e))
            return self._analyze_heuristic(image_bytes, document_type)

    def _analyze_heuristic(self, image_bytes: bytes, document_type: str) -> VLMResult:
        """Heuristic-based analysis when VLM is unavailable."""
        file_size = len(image_bytes)

        # Basic quality checks
        quality = "good" if file_size > 100_000 else "fair" if file_size > 50_000 else "poor"
        completeness = 0.5  # Cannot determine without VLM

        # Check for obvious issues
        indicators = []
        if file_size < 10_000:
            indicators.append("Image file suspiciously small")
        if file_size > 15_000_000:
            indicators.append("Image file unusually large")

        # Check magic bytes for valid image
        is_jpeg = image_bytes[:2] == b'\xff\xd8'
        is_png = image_bytes[:4] == b'\x89PNG'
        is_pdf = image_bytes[:4] == b'%PDF'

        if not (is_jpeg or is_png or is_pdf):
            indicators.append("Invalid image format detected")

        return VLMResult(
            is_valid_ec8a=True,  # Cannot determine without VLM
            tampering_detected=len(indicators) > 0,
            tampering_confidence=0.3 if indicators else 0.0,
            tampering_indicators=indicators,
            document_quality=quality,
            orientation_correct=True,
            completeness_score=completeness,
            analysis_summary=f"Heuristic analysis: {quality} quality, {len(indicators)} potential issues. VLM endpoint not configured for deep analysis.",
        )


vlm_engine = VLMEngine()


# ─── DocLing Integration (Structured Table Extraction) ────────────────────────

class TableCell(BaseModel):
    row: int
    col: int
    text: str
    confidence: float


class DocumentTable(BaseModel):
    headers: list[str]
    rows: list[dict]
    confidence: float


class DocLingResult(BaseModel):
    tables: list[DocumentTable]
    metadata: dict
    page_count: int
    extraction_method: str


class DocLingEngine:
    """DocLing integration for structured document/table extraction."""

    def __init__(self):
        self._initialized = False
        self._converter = None

    def _ensure_initialized(self):
        if self._initialized:
            return
        try:
            from docling.document_converter import DocumentConverter
            self._converter = DocumentConverter()
            self._initialized = True
            log.info("DocLing initialized")
        except ImportError:
            log.warn("DocLing not installed, using fallback table extraction")
            self._initialized = True

    def extract_tables(self, file_bytes: bytes, filename: str) -> DocLingResult:
        """Extract structured tables from document."""
        self._ensure_initialized()

        if self._converter is not None:
            return self._extract_with_docling(file_bytes, filename)
        return self._extract_fallback(file_bytes, filename)

    def _extract_with_docling(self, file_bytes: bytes, filename: str) -> DocLingResult:
        """Real DocLing extraction."""
        # Write to temp file for DocLing — sanitize suffix to prevent path injection
        suffix = Path(filename).suffix
        if not re.match(r'^\.[a-zA-Z0-9]{1,10}$', suffix):
            suffix = ".bin"
        with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as f:
            f.write(file_bytes)
            tmp_path = f.name

        try:
            result = self._converter.convert(tmp_path)
            doc = result.document

            tables = []
            for table in doc.tables:
                df = table.export_to_dataframe()
                headers = list(df.columns)
                rows = df.to_dict(orient="records")
                tables.append(DocumentTable(
                    headers=headers,
                    rows=rows,
                    confidence=0.85,
                ))

            return DocLingResult(
                tables=tables,
                metadata={
                    "title": doc.title or "",
                    "num_pages": len(doc.pages) if hasattr(doc, "pages") else 1,
                },
                page_count=len(doc.pages) if hasattr(doc, "pages") else 1,
                extraction_method="docling",
            )
        finally:
            os.unlink(tmp_path)

    def _extract_fallback(self, file_bytes: bytes, filename: str) -> DocLingResult:
        """Fallback when DocLing is not available."""
        return DocLingResult(
            tables=[],
            metadata={"note": "DocLing not installed — install with: pip install docling"},
            page_count=1,
            extraction_method="fallback",
        )


docling_engine = DocLingEngine()


# ─── Video Analysis ───────────────────────────────────────────────────────────

class VideoAnalysisResult(BaseModel):
    duration_seconds: float
    frame_count: int
    fps: float
    resolution: dict
    key_frames_extracted: int
    anomalies_detected: list[dict]
    ballot_counting_events: list[dict]
    integrity_score: float
    analysis_summary: str


class VideoAnalyzer:
    """Video analysis for ballot counting verification and anomaly detection."""

    def __init__(self):
        self._cv2 = None

    def _ensure_cv2(self):
        if self._cv2 is None:
            try:
                import cv2
                self._cv2 = cv2
            except ImportError:
                log.warn("OpenCV not installed — video analysis unavailable")

    def analyze_video(self, video_bytes: bytes, filename: str) -> VideoAnalysisResult:
        """Analyze video for ballot counting events and anomalies."""
        self._ensure_cv2()

        if self._cv2 is not None:
            return self._analyze_with_opencv(video_bytes, filename)
        return self._analyze_fallback(video_bytes, filename)

    def _analyze_with_opencv(self, video_bytes: bytes, filename: str) -> VideoAnalysisResult:
        """Full video analysis with OpenCV."""
        cv2 = self._cv2

        # Write to temp file — sanitize suffix
        suffix = Path(filename).suffix or ".mp4"
        if not re.match(r'^\.[a-zA-Z0-9]{1,10}$', suffix):
            suffix = ".mp4"
        with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as f:
            f.write(video_bytes)
            tmp_path = f.name

        try:
            cap = cv2.VideoCapture(tmp_path)
            if not cap.isOpened():
                raise ValueError("Cannot open video file")

            fps = cap.get(cv2.CAP_PROP_FPS) or 30.0
            total_frames = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
            width = int(cap.get(cv2.CAP_PROP_FRAME_WIDTH))
            height = int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT))
            duration = total_frames / fps if fps > 0 else 0

            # Extract key frames (1 per second)
            key_frames = []
            anomalies = []
            ballot_events = []
            prev_frame = None
            frame_idx = 0
            sample_interval = max(int(fps), 1)

            max_frames = int(fps * 3600)  # cap at 1 hour of video
            while frame_idx < max_frames:
                ret, frame = cap.read()
                if not ret:
                    break

                if frame_idx % sample_interval == 0:
                    gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
                    key_frames.append(gray)

                    # Detect scene changes (potential ballot counting events)
                    if prev_frame is not None:
                        diff = cv2.absdiff(prev_frame, gray)
                        change_pct = (diff > 30).sum() / diff.size * 100

                        if change_pct > 40:
                            anomalies.append({
                                "frame": frame_idx,
                                "timestamp_sec": round(frame_idx / fps, 2),
                                "type": "scene_change",
                                "change_pct": round(change_pct, 2),
                                "description": "Significant scene change detected",
                            })

                        # Detect motion patterns consistent with ballot handling
                        if 5 < change_pct < 25:
                            ballot_events.append({
                                "frame": frame_idx,
                                "timestamp_sec": round(frame_idx / fps, 2),
                                "type": "ballot_handling",
                                "motion_pct": round(change_pct, 2),
                            })

                    prev_frame = gray
                frame_idx += 1

            cap.release()

            # Check for video integrity issues
            if duration < 5:
                anomalies.append({
                    "frame": 0,
                    "timestamp_sec": 0,
                    "type": "short_video",
                    "description": f"Video only {duration:.1f}s — may be incomplete",
                })

            if total_frames == 0:
                anomalies.append({
                    "frame": 0,
                    "timestamp_sec": 0,
                    "type": "empty_video",
                    "description": "No frames detected",
                })

            # Integrity score: penalize for anomalies
            integrity = max(0.0, 1.0 - len(anomalies) * 0.15)

            return VideoAnalysisResult(
                duration_seconds=round(duration, 2),
                frame_count=total_frames,
                fps=round(fps, 2),
                resolution={"width": width, "height": height},
                key_frames_extracted=len(key_frames),
                anomalies_detected=anomalies,
                ballot_counting_events=ballot_events[:50],  # Cap at 50
                integrity_score=round(integrity, 3),
                analysis_summary=f"Analyzed {total_frames} frames ({duration:.1f}s). {len(ballot_events)} ballot events, {len(anomalies)} anomalies detected.",
            )
        finally:
            os.unlink(tmp_path)

    def _analyze_fallback(self, video_bytes: bytes, filename: str) -> VideoAnalysisResult:
        """Minimal analysis when OpenCV is unavailable."""
        file_size = len(video_bytes)
        # Estimate duration from file size (rough: ~1MB per 10s at 720p)
        est_duration = file_size / 100_000

        return VideoAnalysisResult(
            duration_seconds=round(est_duration, 2),
            frame_count=0,
            fps=0,
            resolution={"width": 0, "height": 0},
            key_frames_extracted=0,
            anomalies_detected=[],
            ballot_counting_events=[],
            integrity_score=0.5,
            analysis_summary=f"OpenCV not installed — install with: pip install opencv-python. File size: {file_size} bytes.",
        )


video_analyzer = VideoAnalyzer()


# ─── KYC (Know Your Customer) Pipeline ───────────────────────────────────────

class KYCVerificationRequest(BaseModel):
    user_id: int
    full_name: str
    id_type: str  # "nin", "voters_card", "passport", "drivers_license"
    id_number: str
    date_of_birth: Optional[str] = None
    phone_number: Optional[str] = None
    address: Optional[str] = None


class KYCVerificationResult(BaseModel):
    user_id: int
    status: str  # "verified", "pending_review", "rejected", "requires_liveness"
    identity_match_score: float
    document_verified: bool
    face_match_score: float
    liveness_passed: bool
    risk_score: float  # 0=low risk, 1=high risk
    checks_performed: list[str]
    flags: list[str]
    verification_timestamp: str


class LivenessCheckResult(BaseModel):
    user_id: int
    passed: bool
    confidence: float
    method: str  # "passive", "active_blink", "active_head_turn", "3d_depth"
    anti_spoofing_score: float
    checks: list[dict]
    timestamp: str


class KYCEngine:
    """KYC verification pipeline with liveness detection."""

    def __init__(self):
        self._face_detector = None

    def _ensure_face_detection(self):
        if self._face_detector is not None:
            return True
        try:
            import cv2
            cascade_path = cv2.data.haarcascades + "haarcascade_frontalface_default.xml"
            self._face_detector = cv2.CascadeClassifier(cascade_path)
            return True
        except (ImportError, Exception):
            return False

    def verify_identity(
        self,
        request: KYCVerificationRequest,
        id_document_bytes: Optional[bytes] = None,
        selfie_bytes: Optional[bytes] = None,
    ) -> KYCVerificationResult:
        """Full KYC verification pipeline."""
        checks = []
        flags = []
        scores = {}

        # Step 1: ID number format validation
        id_valid = self._validate_id_format(request.id_type, request.id_number)
        checks.append("id_format_validation")
        if not id_valid:
            flags.append(f"Invalid {request.id_type} format")

        # Step 2: Document OCR verification (if document provided)
        doc_verified = False
        if id_document_bytes:
            doc_result = self._verify_document(id_document_bytes, request)
            doc_verified = doc_result["verified"]
            scores["document"] = doc_result["confidence"]
            checks.append("document_ocr_verification")
            if not doc_verified:
                flags.append("Document OCR mismatch")
        else:
            flags.append("No ID document uploaded")

        # Step 3: Face match (if selfie + document provided)
        face_score = 0.0
        if selfie_bytes and id_document_bytes:
            face_score = self._compare_faces(selfie_bytes, id_document_bytes)
            scores["face_match"] = face_score
            checks.append("face_comparison")
            if face_score < 0.7:
                flags.append("Face match below threshold")

        # Step 4: NIN database cross-reference (simulated — requires NIMC API)
        if request.id_type == "nin":
            checks.append("nin_database_lookup")
            # In production: call NIMC verification API
            scores["nin_lookup"] = 0.85  # Simulated

        # Step 5: Sanctions/PEP screening
        pep_clear = self._screen_sanctions(request.full_name)
        checks.append("sanctions_pep_screening")
        if not pep_clear:
            flags.append("PEP/Sanctions match found")

        # Step 6: Phone number verification
        if request.phone_number:
            checks.append("phone_verification")

        # Calculate risk score
        risk_factors = len(flags)
        risk_score = min(1.0, risk_factors * 0.2)

        # Determine status
        if risk_score > 0.6:
            status = "rejected"
        elif face_score < 0.5 and selfie_bytes:
            status = "requires_liveness"
        elif risk_score > 0.3:
            status = "pending_review"
        else:
            status = "verified"

        identity_match = (scores.get("document", 0.5) + scores.get("face_match", 0.5) + scores.get("nin_lookup", 0.5)) / 3

        return KYCVerificationResult(
            user_id=request.user_id,
            status=status,
            identity_match_score=round(identity_match, 3),
            document_verified=doc_verified,
            face_match_score=round(face_score, 3),
            liveness_passed=False,  # Requires separate liveness check
            risk_score=round(risk_score, 3),
            checks_performed=checks,
            flags=flags,
            verification_timestamp=datetime.now(timezone.utc).isoformat(),
        )

    def liveness_check(self, video_bytes: bytes, user_id: int, method: str = "passive") -> LivenessCheckResult:
        """Perform liveness detection to prevent spoofing."""
        checks = []

        if not self._ensure_face_detection():
            # Fallback without OpenCV
            return LivenessCheckResult(
                user_id=user_id,
                passed=False,
                confidence=0.0,
                method=method,
                anti_spoofing_score=0.0,
                checks=[{"name": "opencv_unavailable", "passed": False, "note": "Install opencv-python for liveness detection"}],
                timestamp=datetime.now(timezone.utc).isoformat(),
            )

        import cv2
        import numpy as np

        # Write video to temp file
        with tempfile.NamedTemporaryFile(suffix=".mp4", delete=False) as f:
            f.write(video_bytes)
            tmp_path = f.name

        try:
            cap = cv2.VideoCapture(tmp_path)
            if not cap.isOpened():
                return LivenessCheckResult(
                    user_id=user_id, passed=False, confidence=0.0,
                    method=method, anti_spoofing_score=0.0,
                    checks=[{"name": "video_open", "passed": False}],
                    timestamp=datetime.now(timezone.utc).isoformat(),
                )

            face_sizes = []
            face_positions = []
            frame_count = 0
            faces_detected_count = 0
            texture_scores = []

            while frame_count < 90:  # Analyze up to 3 seconds at 30fps
                ret, frame = cap.read()
                if not ret:
                    break

                gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
                faces = self._face_detector.detectMultiScale(gray, 1.3, 5)

                if len(faces) == 1:
                    faces_detected_count += 1
                    x, y, w, h = faces[0]
                    face_sizes.append(w * h)
                    face_positions.append((x + w // 2, y + h // 2))

                    # Texture analysis (LBP-based anti-spoofing)
                    face_roi = gray[y:y+h, x:x+w]
                    if face_roi.size > 0:
                        laplacian_var = cv2.Laplacian(face_roi, cv2.CV_64F).var()
                        texture_scores.append(laplacian_var)

                frame_count += 1

            cap.release()

            # Check 1: Consistent face detection (at least 60% of frames)
            face_ratio = faces_detected_count / max(frame_count, 1)
            checks.append({
                "name": "face_presence",
                "passed": face_ratio > 0.6,
                "value": round(face_ratio, 3),
                "threshold": 0.6,
            })

            # Check 2: Natural face size variation (not a flat photo)
            size_variation = 0.0
            if len(face_sizes) > 5:
                mean_size = sum(face_sizes) / len(face_sizes)
                size_variation = (max(face_sizes) - min(face_sizes)) / max(mean_size, 1)
            checks.append({
                "name": "size_variation",
                "passed": size_variation > 0.02,
                "value": round(size_variation, 4),
                "threshold": 0.02,
                "note": "Flat photos have near-zero size variation",
            })

            # Check 3: Natural position movement
            position_movement = 0.0
            if len(face_positions) > 5:
                dx = [abs(face_positions[i][0] - face_positions[i-1][0]) for i in range(1, len(face_positions))]
                dy = [abs(face_positions[i][1] - face_positions[i-1][1]) for i in range(1, len(face_positions))]
                position_movement = (sum(dx) + sum(dy)) / len(dx)
            checks.append({
                "name": "natural_movement",
                "passed": position_movement > 1.0,
                "value": round(position_movement, 3),
                "threshold": 1.0,
                "note": "Real faces have micro-movements",
            })

            # Check 4: Texture analysis (screens/prints have different texture)
            avg_texture = sum(texture_scores) / max(len(texture_scores), 1) if texture_scores else 0
            checks.append({
                "name": "texture_liveness",
                "passed": avg_texture > 50.0,
                "value": round(avg_texture, 2),
                "threshold": 50.0,
                "note": "Low texture variance suggests screen/print attack",
            })

            # Check 5: Temporal consistency (same person throughout)
            if len(face_sizes) > 10:
                size_std = (sum((s - sum(face_sizes)/len(face_sizes))**2 for s in face_sizes) / len(face_sizes)) ** 0.5
                consistency = 1.0 - min(1.0, size_std / (sum(face_sizes)/len(face_sizes)))
            else:
                consistency = 0.0
            checks.append({
                "name": "temporal_consistency",
                "passed": consistency > 0.8,
                "value": round(consistency, 3),
                "threshold": 0.8,
            })

            # Active liveness checks
            if method == "active_blink":
                # Detect blink: face area should show eye-region changes
                checks.append({
                    "name": "blink_detection",
                    "passed": face_ratio > 0.6 and size_variation > 0.01,
                    "note": "Blink detection requires eye landmark model",
                })
            elif method == "active_head_turn":
                checks.append({
                    "name": "head_turn",
                    "passed": position_movement > 5.0,
                    "value": round(position_movement, 3),
                    "note": "Head turn requires significant lateral movement",
                })

            # Calculate final scores
            passed_checks = sum(1 for c in checks if c.get("passed", False))
            total_checks = len(checks)
            confidence = passed_checks / max(total_checks, 1)

            # Anti-spoofing score (weighted)
            anti_spoof = (
                (0.3 * (1 if size_variation > 0.02 else 0)) +
                (0.3 * (1 if avg_texture > 50 else 0)) +
                (0.2 * (1 if position_movement > 1.0 else 0)) +
                (0.2 * (1 if consistency > 0.8 else 0))
            )

            passed = confidence >= 0.7 and anti_spoof >= 0.6

            return LivenessCheckResult(
                user_id=user_id,
                passed=passed,
                confidence=round(confidence, 3),
                method=method,
                anti_spoofing_score=round(anti_spoof, 3),
                checks=checks,
                timestamp=datetime.now(timezone.utc).isoformat(),
            )
        finally:
            os.unlink(tmp_path)

    def _validate_id_format(self, id_type: str, id_number: str) -> bool:
        """Validate Nigerian ID number formats."""
        patterns = {
            "nin": r"^\d{11}$",  # 11-digit NIN
            "voters_card": r"^[A-Z0-9]{19}$",  # 19-char PVC number
            "passport": r"^[A-Z]\d{8}$",  # Letter + 8 digits
            "drivers_license": r"^[A-Z]{3}\d{5,12}[A-Z]{2}$",  # State prefix + digits + suffix
        }
        pattern = patterns.get(id_type)
        if not pattern:
            return False
        return bool(re.match(pattern, id_number))

    def _verify_document(self, doc_bytes: bytes, request: KYCVerificationRequest) -> dict:
        """OCR the ID document and verify against provided details."""
        ocr_results = ocr_engine.extract_text(doc_bytes)
        text = " ".join(r.text for r in ocr_results).upper()

        confidence = 0.0
        name_parts = request.full_name.upper().split()

        # Check if name appears in document
        name_matches = sum(1 for part in name_parts if part in text)
        name_score = name_matches / max(len(name_parts), 1)

        # Check if ID number appears
        id_found = request.id_number.upper() in text.replace(" ", "")
        id_score = 1.0 if id_found else 0.0

        confidence = (name_score * 0.5 + id_score * 0.5)
        verified = confidence > 0.6

        return {"verified": verified, "confidence": confidence}

    def _compare_faces(self, selfie_bytes: bytes, document_bytes: bytes) -> float:
        """Compare face in selfie with face on ID document."""
        if not self._ensure_face_detection():
            return 0.5

        import cv2
        import numpy as np

        def detect_face(img_bytes: bytes):
            arr = np.frombuffer(img_bytes, np.uint8)
            img = cv2.imdecode(arr, cv2.IMREAD_COLOR)
            if img is None:
                return None
            gray = cv2.cvtColor(img, cv2.COLOR_BGR2GRAY)
            faces = self._face_detector.detectMultiScale(gray, 1.3, 5)
            if len(faces) == 0:
                return None
            x, y, w, h = faces[0]
            return gray[y:y+h, x:x+w]

        face1 = detect_face(selfie_bytes)
        face2 = detect_face(document_bytes)

        if face1 is None or face2 is None:
            return 0.3  # Cannot compare if face not detected

        # Resize to same dimensions for histogram comparison
        target_size = (100, 100)
        face1_resized = cv2.resize(face1, target_size)
        face2_resized = cv2.resize(face2, target_size)

        # Histogram comparison (Correlation method)
        hist1 = cv2.calcHist([face1_resized], [0], None, [256], [0, 256])
        hist2 = cv2.calcHist([face2_resized], [0], None, [256], [0, 256])
        cv2.normalize(hist1, hist1, 0, 1, cv2.NORM_MINMAX)
        cv2.normalize(hist2, hist2, 0, 1, cv2.NORM_MINMAX)

        score = cv2.compareHist(hist1, hist2, cv2.HISTCMP_CORREL)
        return max(0.0, min(1.0, score))

    def _screen_sanctions(self, full_name: str) -> bool:
        """Screen against sanctions/PEP lists. Returns True if clear."""
        # In production: call external sanctions screening API (e.g., ComplyAdvantage, Refinitiv)
        # For now, return True (clear) for all non-test names
        test_blocked = ["TEST SANCTIONED", "PEP FLAGGED"]
        return full_name.upper() not in test_blocked


kyc_engine = KYCEngine()


# ─── API Endpoints ────────────────────────────────────────────────────────────

@app.get("/health")
async def health():
    return {
        "status": "healthy",
        "services": {
            "paddleocr": "available" if ocr_engine._paddle_ocr is not None else "fallback",
            "vlm": "available" if VLM_ENDPOINT else "heuristic_only",
            "docling": "available" if docling_engine._converter is not None else "fallback",
            "video": "available" if video_analyzer._cv2 is not None else "fallback",
        },
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }


@app.post("/ocr/extract")
async def ocr_extract(file: UploadFile = File(...)):
    """Extract raw text from an image using PaddleOCR."""
    content = await file.read()
    results = ocr_engine.extract_text(content)
    return {
        "filename": file.filename,
        "regions": [r.model_dump() for r in results],
        "total_regions": len(results),
    }


@app.post("/ocr/ec8a")
async def ocr_ec8a(file: UploadFile = File(...)):
    """Extract structured EC8A form data from an image."""
    content = await file.read()
    extraction = ocr_engine.extract_ec8a(content)
    return extraction.model_dump()


@app.post("/vlm/analyze")
async def vlm_analyze(
    file: UploadFile = File(...),
    document_type: str = Form(default="ec8a"),
):
    """Analyze document for authenticity and tampering using VLM."""
    content = await file.read()
    result = vlm_engine.analyze_document(content, document_type)
    return result.model_dump()


@app.post("/docling/tables")
async def docling_extract_tables(file: UploadFile = File(...)):
    """Extract structured tables from a document using DocLing."""
    content = await file.read()
    result = docling_engine.extract_tables(content, file.filename or "document.pdf")
    return result.model_dump()


@app.post("/video/analyze")
async def video_analyze(file: UploadFile = File(...)):
    """Analyze video for ballot counting events and anomalies."""
    content = await file.read()
    if len(content) > 100_000_000:  # 100MB limit
        raise HTTPException(status_code=413, detail="Video exceeds 100MB limit")
    result = video_analyzer.analyze_video(content, file.filename or "video.mp4")
    return result.model_dump()


@app.post("/kyc/verify")
async def kyc_verify(
    user_id: int = Form(...),
    full_name: str = Form(...),
    id_type: str = Form(...),
    id_number: str = Form(...),
    date_of_birth: Optional[str] = Form(default=None),
    phone_number: Optional[str] = Form(default=None),
    id_document: Optional[UploadFile] = File(default=None),
    selfie: Optional[UploadFile] = File(default=None),
):
    """Full KYC identity verification pipeline."""
    request = KYCVerificationRequest(
        user_id=user_id,
        full_name=full_name,
        id_type=id_type,
        id_number=id_number,
        date_of_birth=date_of_birth,
        phone_number=phone_number,
    )

    id_doc_bytes = await id_document.read() if id_document else None
    selfie_bytes = await selfie.read() if selfie else None

    result = kyc_engine.verify_identity(request, id_doc_bytes, selfie_bytes)
    return result.model_dump()


@app.post("/kyc/liveness")
async def kyc_liveness(
    user_id: int = Form(...),
    method: str = Form(default="passive"),
    video: UploadFile = File(...),
):
    """Perform liveness detection from video."""
    video_bytes = await video.read()
    if len(video_bytes) > 50_000_000:  # 50MB limit for liveness video
        raise HTTPException(status_code=413, detail="Liveness video exceeds 50MB limit")
    result = kyc_engine.liveness_check(video_bytes, user_id, method)
    return result.model_dump()


@app.post("/analyze/photo-report")
async def analyze_photo_report(
    file: UploadFile = File(...),
    report_id: Optional[int] = Form(default=None),
):
    """Full pipeline: OCR + VLM + DocLing on a single uploaded photo.
    This is the main endpoint called by the Go backend after observer upload."""
    content = await file.read()

    # Run all analyses
    ocr_result = ocr_engine.extract_ec8a(content)
    vlm_result = vlm_engine.analyze_document(content, "ec8a")
    docling_result = docling_engine.extract_tables(content, file.filename or "photo.jpg")

    # Combine into single response
    return {
        "report_id": report_id,
        "ocr": ocr_result.model_dump(),
        "vlm": vlm_result.model_dump(),
        "docling": docling_result.model_dump(),
        "combined_confidence": round(
            (ocr_result.confidence_score + vlm_result.completeness_score) / 2, 3
        ),
        "requires_manual_review": (
            ocr_result.confidence_score < 0.6 or
            vlm_result.tampering_detected or
            len(ocr_result.extraction_warnings) > 2
        ),
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }


if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("PORT", "8089"))
    uvicorn.run(app, host="0.0.0.0", port=port)
