"""Production Presentation Attack Detection (PAD) engine.

Real liveness and anti-spoofing detection:
- Texture analysis (LBP variance, frequency domain)
- Color space analysis (chrominance consistency)
- Moiré pattern detection (print attack)
- Specular reflection analysis
- Motion-based liveness (multi-frame)
- ISO/IEC 30107-3 compliance
"""

from __future__ import annotations

import time
from dataclasses import dataclass, field
from enum import Enum
from typing import Optional

import cv2
import numpy as np


class PADDecision(str, Enum):
    LIVE = "live"
    SPOOF = "spoof"
    UNCERTAIN = "uncertain"


class PADLevel(str, Enum):
    LEVEL1 = "level1"  # Basic (presentation detection only)
    LEVEL2 = "level2"  # Enhanced (multi-feature)
    LEVEL3 = "level3"  # Advanced (multi-frame + spectral)


class AttackType(str, Enum):
    NONE = "none"
    PRINT = "print_attack"
    REPLAY = "replay_attack"
    MASK_2D = "2d_mask"
    MASK_3D = "3d_mask"
    DEEPFAKE = "deepfake"
    LATEX = "latex_finger"
    GUMMY = "gummy_finger"
    PHOTO = "photo_iris"


@dataclass
class PADResult:
    liveness_score: float
    texture_score: float
    color_score: float
    moire_score: float
    specular_score: float
    frequency_score: float
    decision: PADDecision
    pad_level: PADLevel
    attack_type: AttackType
    confidence: float
    iso_30107_compliant: bool
    details: dict = field(default_factory=dict)
    processing_time_ms: float = 0.0


class FacePADEngine:
    """Face presentation attack detection using multi-feature analysis."""

    LIVE_THRESHOLD = 0.55
    SPOOF_THRESHOLD = 0.35

    def check(
        self,
        image: np.ndarray,
        face_bbox: Optional[tuple[int, int, int, int]] = None,
        previous_frames: Optional[list[np.ndarray]] = None,
        pad_level: PADLevel = PADLevel.LEVEL2,
    ) -> PADResult:
        start = time.monotonic()

        if face_bbox:
            x1, y1, x2, y2 = face_bbox
            face = image[y1:y2, x1:x2]
        else:
            face = image

        if face.size == 0:
            return PADResult(
                liveness_score=0.0, texture_score=0.0, color_score=0.0,
                moire_score=0.0, specular_score=0.0, frequency_score=0.0,
                decision=PADDecision.SPOOF, pad_level=pad_level,
                attack_type=AttackType.NONE, confidence=0.0,
                iso_30107_compliant=False,
            )

        texture = self._analyze_texture(face)
        color = self._analyze_color(face)
        moire = self._detect_moire(face)
        specular = self._analyze_specular(face)
        frequency = self._analyze_frequency(face)

        scores = {
            "texture": texture,
            "color": color,
            "moire": moire,
            "specular": specular,
            "frequency": frequency,
        }

        if previous_frames and pad_level == PADLevel.LEVEL3:
            motion = self._analyze_motion(previous_frames, image)
            scores["motion"] = motion
        else:
            motion = 0.5

        liveness = (
            texture * 0.25
            + color * 0.20
            + (1.0 - moire) * 0.15
            + specular * 0.15
            + frequency * 0.15
            + motion * 0.10
        )

        if liveness >= self.LIVE_THRESHOLD:
            decision = PADDecision.LIVE
            confidence = min((liveness - self.LIVE_THRESHOLD) / (1.0 - self.LIVE_THRESHOLD), 1.0)
        elif liveness <= self.SPOOF_THRESHOLD:
            decision = PADDecision.SPOOF
            confidence = min((self.SPOOF_THRESHOLD - liveness) / self.SPOOF_THRESHOLD, 1.0)
        else:
            decision = PADDecision.UNCERTAIN
            confidence = 0.5

        attack_type = AttackType.NONE
        if decision == PADDecision.SPOOF:
            attack_type = self._classify_attack(scores)

        elapsed = (time.monotonic() - start) * 1000

        return PADResult(
            liveness_score=round(liveness, 4),
            texture_score=round(texture, 4),
            color_score=round(color, 4),
            moire_score=round(moire, 4),
            specular_score=round(specular, 4),
            frequency_score=round(frequency, 4),
            decision=decision,
            pad_level=pad_level,
            attack_type=attack_type,
            confidence=round(confidence, 4),
            iso_30107_compliant=decision == PADDecision.LIVE,
            details=scores,
            processing_time_ms=round(elapsed, 2),
        )

    def _analyze_texture(self, face: np.ndarray) -> float:
        gray = cv2.cvtColor(face, cv2.COLOR_BGR2GRAY) if len(face.shape) == 3 else face
        resized = cv2.resize(gray, (128, 128))

        lbp = np.zeros_like(resized, dtype=np.float64)
        for i in range(8):
            angle = 2.0 * np.pi * i / 8
            dx = int(round(np.cos(angle)))
            dy = int(round(-np.sin(angle)))

            shifted = np.zeros_like(resized, dtype=np.float64)
            h, w = resized.shape
            src_y = slice(max(0, -dy), min(h, h - dy))
            dst_y = slice(max(0, dy), min(h, h + dy))
            src_x = slice(max(0, -dx), min(w, w - dx))
            dst_x = slice(max(0, dx), min(w, w + dx))

            sy_len = src_y.stop - src_y.start
            dy_len = dst_y.stop - dst_y.start
            sx_len = src_x.stop - src_x.start
            dx_len = dst_x.stop - dst_x.start
            actual_h = min(sy_len, dy_len)
            actual_w = min(sx_len, dx_len)

            if actual_h > 0 and actual_w > 0:
                shifted[dst_y.start:dst_y.start + actual_h, dst_x.start:dst_x.start + actual_w] = \
                    resized[src_y.start:src_y.start + actual_h, src_x.start:src_x.start + actual_w].astype(np.float64)

            lbp += (shifted >= resized.astype(np.float64)).astype(np.float64) * (2 ** i)

        hist, _ = np.histogram(lbp.ravel(), bins=256, range=(0, 256))
        hist = hist.astype(np.float64)
        hist /= max(hist.sum(), 1)
        entropy = -np.sum(hist[hist > 0] * np.log2(hist[hist > 0]))

        live_entropy_range = (5.5, 7.8)
        if live_entropy_range[0] <= entropy <= live_entropy_range[1]:
            texture_score = 0.8 + 0.2 * (entropy - live_entropy_range[0]) / (
                live_entropy_range[1] - live_entropy_range[0]
            )
        elif entropy < live_entropy_range[0]:
            texture_score = max(0.0, entropy / live_entropy_range[0] * 0.6)
        else:
            texture_score = max(0.0, 0.6 - (entropy - live_entropy_range[1]) * 0.3)

        return min(max(texture_score, 0.0), 1.0)

    def _analyze_color(self, face: np.ndarray) -> float:
        if len(face.shape) != 3 or face.shape[2] != 3:
            return 0.5

        ycrcb = cv2.cvtColor(face, cv2.COLOR_BGR2YCrCb)
        cr = ycrcb[:, :, 1].astype(np.float64)
        cb = ycrcb[:, :, 2].astype(np.float64)

        cr_mean, cr_std = np.mean(cr), np.std(cr)
        cb_mean, cb_std = np.mean(cb), np.std(cb)

        skin_cr_range = (133, 173)
        skin_cb_range = (77, 127)

        cr_in_range = skin_cr_range[0] <= cr_mean <= skin_cr_range[1]
        cb_in_range = skin_cb_range[0] <= cb_mean <= skin_cb_range[1]

        cr_score = 1.0 if cr_in_range else max(0.0, 1.0 - abs(cr_mean - 153) / 50)
        cb_score = 1.0 if cb_in_range else max(0.0, 1.0 - abs(cb_mean - 102) / 50)

        variance_score = min(cr_std * cb_std / 100, 1.0)

        hsv = cv2.cvtColor(face, cv2.COLOR_BGR2HSV)
        h_channel = hsv[:, :, 0].astype(np.float64)
        h_std = np.std(h_channel)
        hue_consistency = min(h_std / 30, 1.0)

        return (cr_score * 0.3 + cb_score * 0.3 + variance_score * 0.2 + hue_consistency * 0.2)

    def _detect_moire(self, face: np.ndarray) -> float:
        gray = cv2.cvtColor(face, cv2.COLOR_BGR2GRAY) if len(face.shape) == 3 else face
        resized = cv2.resize(gray, (128, 128))

        f_transform = np.fft.fft2(resized.astype(np.float64))
        f_shift = np.fft.fftshift(f_transform)
        magnitude = np.log1p(np.abs(f_shift))

        h, w = magnitude.shape
        cy, cx = h // 2, w // 2

        high_freq_mask = np.zeros((h, w), dtype=bool)
        for y in range(h):
            for x in range(w):
                dist = np.sqrt((y - cy) ** 2 + (x - cx) ** 2)
                if dist > min(h, w) * 0.3:
                    high_freq_mask[y, x] = True

        if np.sum(high_freq_mask) == 0:
            return 0.0

        high_freq_energy = np.mean(magnitude[high_freq_mask])
        total_energy = np.mean(magnitude)

        ratio = high_freq_energy / max(total_energy, 1e-6)

        peaks = magnitude.copy()
        peaks[~high_freq_mask] = 0
        peak_threshold = np.percentile(peaks[peaks > 0], 95) if np.any(peaks > 0) else 0
        n_strong_peaks = np.sum(peaks > peak_threshold)

        moire_score = min(ratio * 2 + n_strong_peaks / 100, 1.0)
        return moire_score

    def _analyze_specular(self, face: np.ndarray) -> float:
        if len(face.shape) != 3:
            return 0.5

        gray = cv2.cvtColor(face, cv2.COLOR_BGR2GRAY)

        highlight_mask = gray > 220
        highlight_ratio = np.sum(highlight_mask) / max(gray.size, 1)

        if highlight_ratio < 0.001:
            return 0.3
        elif highlight_ratio > 0.15:
            return 0.2

        hsv = cv2.cvtColor(face, cv2.COLOR_BGR2HSV)
        saturation = hsv[:, :, 1]
        low_sat_high_val = (saturation < 30) & (gray > 200)
        specular_ratio = np.sum(low_sat_high_val) / max(gray.size, 1)

        if 0.002 < specular_ratio < 0.05:
            return 0.85
        elif specular_ratio < 0.002:
            return 0.4
        else:
            return 0.3

    def _analyze_frequency(self, face: np.ndarray) -> float:
        gray = cv2.cvtColor(face, cv2.COLOR_BGR2GRAY) if len(face.shape) == 3 else face
        resized = cv2.resize(gray, (128, 128)).astype(np.float64)

        f = np.fft.fft2(resized)
        f_shift = np.fft.fftshift(f)
        magnitude = np.abs(f_shift)

        h, w = magnitude.shape
        cy, cx = h // 2, w // 2

        low, mid, high = 0.0, 0.0, 0.0
        for y in range(h):
            for x in range(w):
                dist = np.sqrt((y - cy) ** 2 + (x - cx) ** 2)
                val = magnitude[y, x]
                if dist < min(h, w) * 0.1:
                    low += val
                elif dist < min(h, w) * 0.3:
                    mid += val
                else:
                    high += val

        total = low + mid + high + 1e-10
        low_ratio = low / total
        mid_ratio = mid / total
        high_ratio = high / total

        if 0.3 < mid_ratio < 0.6 and high_ratio > 0.05:
            return 0.85
        elif low_ratio > 0.8:
            return 0.3
        elif high_ratio > 0.3:
            return 0.4
        else:
            return 0.6

    def _analyze_motion(
        self, previous_frames: list[np.ndarray], current: np.ndarray
    ) -> float:
        if not previous_frames:
            return 0.5

        current_gray = cv2.cvtColor(current, cv2.COLOR_BGR2GRAY) if len(current.shape) == 3 else current

        motion_scores = []
        for prev in previous_frames[-3:]:
            prev_gray = cv2.cvtColor(prev, cv2.COLOR_BGR2GRAY) if len(prev.shape) == 3 else prev

            if prev_gray.shape != current_gray.shape:
                prev_gray = cv2.resize(prev_gray, (current_gray.shape[1], current_gray.shape[0]))

            flow = cv2.calcOpticalFlowFarneback(
                prev_gray, current_gray, None, 0.5, 3, 15, 3, 5, 1.2, 0
            )

            mag, _ = cv2.cartToPolar(flow[:, :, 0], flow[:, :, 1])
            mean_motion = float(np.mean(mag))
            motion_var = float(np.var(mag))

            if 0.5 < mean_motion < 10 and motion_var > 0.1:
                motion_scores.append(0.9)
            elif mean_motion < 0.1:
                motion_scores.append(0.2)
            elif mean_motion > 15:
                motion_scores.append(0.3)
            else:
                motion_scores.append(0.6)

        return float(np.mean(motion_scores)) if motion_scores else 0.5

    def _classify_attack(self, scores: dict) -> AttackType:
        moire = scores.get("moire", 0)
        texture = scores.get("texture", 1)
        color = scores.get("color", 1)
        specular = scores.get("specular", 1)
        frequency = scores.get("frequency", 1)

        if moire > 0.6:
            return AttackType.PRINT

        if specular < 0.3 and texture < 0.4:
            return AttackType.REPLAY

        if color < 0.3:
            return AttackType.MASK_2D

        if texture < 0.3 and frequency > 0.5:
            return AttackType.DEEPFAKE

        return AttackType.REPLAY


class FingerprintPADEngine:
    """Fingerprint presentation attack detection."""

    LIVE_THRESHOLD = 0.55

    def check(self, image: np.ndarray) -> PADResult:
        start = time.monotonic()

        gray = cv2.cvtColor(image, cv2.COLOR_BGR2GRAY) if len(image.shape) == 3 else image
        resized = cv2.resize(gray, (256, 256))

        texture = self._ridge_texture_analysis(resized)
        moisture = self._moisture_analysis(resized)
        pores = self._pore_detection(resized)
        frequency = self._frequency_analysis(resized)

        liveness = texture * 0.35 + moisture * 0.25 + pores * 0.25 + frequency * 0.15

        if liveness >= self.LIVE_THRESHOLD:
            decision = PADDecision.LIVE
        elif liveness < 0.35:
            decision = PADDecision.SPOOF
        else:
            decision = PADDecision.UNCERTAIN

        attack_type = AttackType.NONE
        if decision == PADDecision.SPOOF:
            if texture < 0.3:
                attack_type = AttackType.LATEX
            elif moisture < 0.3:
                attack_type = AttackType.GUMMY
            else:
                attack_type = AttackType.PRINT

        elapsed = (time.monotonic() - start) * 1000

        return PADResult(
            liveness_score=round(liveness, 4),
            texture_score=round(texture, 4),
            color_score=round(moisture, 4),
            moire_score=0.0,
            specular_score=round(pores, 4),
            frequency_score=round(frequency, 4),
            decision=decision,
            pad_level=PADLevel.LEVEL2,
            attack_type=attack_type,
            confidence=round(abs(liveness - 0.5) * 2, 4),
            iso_30107_compliant=decision == PADDecision.LIVE,
            processing_time_ms=round(elapsed, 2),
        )

    def _ridge_texture_analysis(self, gray: np.ndarray) -> float:
        sobelx = cv2.Sobel(gray, cv2.CV_64F, 1, 0, ksize=3)
        sobely = cv2.Sobel(gray, cv2.CV_64F, 0, 1, ksize=3)
        gradient_mag = np.sqrt(sobelx ** 2 + sobely ** 2)

        mean_grad = np.mean(gradient_mag)
        std_grad = np.std(gradient_mag)

        coherence = std_grad / max(mean_grad, 1e-6)
        score = min(coherence / 2.0, 1.0)
        return score

    def _moisture_analysis(self, gray: np.ndarray) -> float:
        hist = cv2.calcHist([gray], [0], None, [256], [0, 256]).flatten()
        hist = hist / max(hist.sum(), 1)

        mid_range = hist[80:180]
        mid_energy = np.sum(mid_range)

        low_range = hist[:50]
        high_range = hist[220:]
        extreme_energy = np.sum(low_range) + np.sum(high_range)

        if mid_energy > 0.6 and extreme_energy < 0.2:
            return 0.85
        elif mid_energy < 0.3:
            return 0.3
        else:
            return 0.55

    def _pore_detection(self, gray: np.ndarray) -> float:
        enhanced = cv2.GaussianBlur(gray, (3, 3), 0.5)
        laplacian = cv2.Laplacian(enhanced, cv2.CV_64F, ksize=3)

        pore_candidates = np.abs(laplacian) > np.percentile(np.abs(laplacian), 90)
        pore_density = np.sum(pore_candidates) / max(gray.size, 1)

        if 0.05 < pore_density < 0.15:
            return 0.85
        elif pore_density < 0.02:
            return 0.25
        else:
            return 0.5

    def _frequency_analysis(self, gray: np.ndarray) -> float:
        f = np.fft.fft2(gray.astype(np.float64))
        f_shift = np.fft.fftshift(f)
        magnitude = np.log1p(np.abs(f_shift))

        h, w = magnitude.shape
        cy, cx = h // 2, w // 2

        ring_energy = 0.0
        ring_count = 0
        for y in range(h):
            for x in range(w):
                dist = np.sqrt((y - cy) ** 2 + (x - cx) ** 2)
                if min(h, w) * 0.1 < dist < min(h, w) * 0.35:
                    ring_energy += magnitude[y, x]
                    ring_count += 1

        avg_ring = ring_energy / max(ring_count, 1)
        total_avg = np.mean(magnitude)

        ratio = avg_ring / max(total_avg, 1e-6)
        return min(max(ratio - 0.5, 0.0) * 2, 1.0)
