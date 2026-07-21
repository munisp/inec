import { useState, useRef, useCallback, useEffect } from 'react';
import {
  CameraCapture,
  FingerprintScanner,
  BiometricAPIClient,
} from '../lib/biometric/capture';
import type { CaptureResult, QualityFeedback } from '../lib/biometric/capture';

type KioskStep =
  | 'welcome'
  | 'identity'
  | 'fingerprint'
  | 'face'
  | 'iris'
  | 'liveness'
  | 'review'
  | 'complete';

interface EnrollmentData {
  voterVin: string;
  deviceId: string;
  firstName: string;
  lastName: string;
  dateOfBirth: string;
  stateCode: string;
  fingerprint?: CaptureResult;
  face?: CaptureResult;
  iris?: CaptureResult;
  livenessResult?: { decision: string; score: number };
  qualityScores: { fingerprint?: number; face?: number; iris?: number };
}

const STEPS: { key: KioskStep; label: string }[] = [
  { key: 'welcome', label: 'Welcome' },
  { key: 'identity', label: 'Identity' },
  { key: 'fingerprint', label: 'Fingerprint' },
  { key: 'face', label: 'Face Capture' },
  { key: 'iris', label: 'Iris Scan' },
  { key: 'liveness', label: 'Liveness' },
  { key: 'review', label: 'Review' },
  { key: 'complete', label: 'Complete' },
];

const api = new BiometricAPIClient();
const camera = new CameraCapture();
// const _webauthn = new WebAuthnBiometric();

export default function EnrollmentKioskPage() {
  const [step, setStep] = useState<KioskStep>('welcome');
  const [data, setData] = useState<EnrollmentData>({
    voterVin: '',
    deviceId: '',
    firstName: '',
    lastName: '',
    dateOfBirth: '',
    stateCode: '',
    qualityScores: {},
  });
  const [quality, setQuality] = useState<QualityFeedback | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [processing, setProcessing] = useState(false);

  const videoRef = useRef<HTMLVideoElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);

  const currentStepIndex = STEPS.findIndex(s => s.key === step);

  const goNext = useCallback(() => {
    if (currentStepIndex < STEPS.length - 1) {
      setStep(STEPS[currentStepIndex + 1].key);
      setError(null);
      setQuality(null);
    }
  }, [currentStepIndex]);

  const goBack = useCallback(() => {
    if (currentStepIndex > 0) {
      setStep(STEPS[currentStepIndex - 1].key);
      setError(null);
      setQuality(null);
    }
  }, [currentStepIndex]);

  // Cleanup camera on step change
  useEffect(() => {
    return () => { camera.stop(); };
  }, [step]);

  const startCamera = useCallback(async (modality: 'face' | 'iris') => {
    if (videoRef.current && canvasRef.current) {
      await camera.start(videoRef.current, canvasRef.current, modality, setQuality);
    }
  }, []);

  const captureImage = useCallback(async (modality: 'face' | 'iris') => {
    setProcessing(true);
    setError(null);
    try {
      const result = await camera.capture(modality);
      camera.stop();
      setData(d => ({
        ...d,
        [modality]: result,
        qualityScores: { ...d.qualityScores, [modality]: result.quality },
      }));
      goNext();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Capture failed');
    } finally {
      setProcessing(false);
    }
  }, [goNext]);

  const captureFingerprint = useCallback(async () => {
    setProcessing(true);
    setError(null);
    try {
      if (FingerprintScanner.isSupported()) {
        const scanner = new FingerprintScanner();
        await scanner.connect();
        const result = await scanner.capture();
        await scanner.disconnect();
        setData(d => ({
          ...d,
          fingerprint: result,
          qualityScores: { ...d.qualityScores, fingerprint: result.quality },
        }));
      } else {
        throw new Error('A registered fingerprint scanner is required; camera images cannot be used as fingerprint evidence.');
      }
      goNext();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Fingerprint capture failed');
    } finally {
      setProcessing(false);
    }
  }, [goNext]);

  const performLiveness = useCallback(async () => {
    setProcessing(true);
    setError(null);
    try {
      if (data.face?.imageData) {
        const result = await api.checkPAD(data.face.imageData, 'face');
        setData(d => ({
          ...d,
          livenessResult: { decision: result.decision, score: result.liveness_score },
        }));
      }
      goNext();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Liveness verification failed');
    } finally {
      setProcessing(false);
    }
  }, [data.face, goNext]);

  const submitEnrollment = useCallback(async () => {
    setProcessing(true);
    setError(null);
    try {
      if (data.fingerprint?.imageData) {
        await api.enroll(data.voterVin, 'fingerprint', data.fingerprint.imageData, data.deviceId);
      }
      if (data.face?.imageData) {
        await api.enroll(data.voterVin, 'face', data.face.imageData, data.deviceId);
      }
      if (data.iris?.imageData) {
        await api.enroll(data.voterVin, 'iris', data.iris.imageData, data.deviceId);
      }
      goNext();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Enrollment failed');
    } finally {
      setProcessing(false);
    }
  }, [data, goNext]);

  return (
    <div className="min-h-screen bg-gray-50 flex flex-col">
      {/* Progress Bar */}
      <div className="bg-white shadow px-6 py-4">
        <div className="flex items-center justify-between max-w-4xl mx-auto">
          {STEPS.map((s, i) => (
            <div key={s.key} className="flex items-center">
              <div className={`w-8 h-8 rounded-full flex items-center justify-center text-xs font-bold
                ${i < currentStepIndex ? 'bg-green-500 text-white' :
                  i === currentStepIndex ? 'bg-green-600 text-white ring-2 ring-green-300' :
                  'bg-gray-200 text-gray-500'}`}>
                {i < currentStepIndex ? '\u2713' : i + 1}
              </div>
              {i < STEPS.length - 1 && (
                <div className={`w-12 h-0.5 ${i < currentStepIndex ? 'bg-green-500' : 'bg-gray-200'}`} />
              )}
            </div>
          ))}
        </div>
        <div className="text-center mt-2 text-sm font-medium text-gray-600">
          Step {currentStepIndex + 1} of {STEPS.length}: {STEPS[currentStepIndex].label}
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 flex items-center justify-center p-6">
        <div className="bg-white rounded-xl shadow-lg p-8 max-w-2xl w-full">
          {error && (
            <div className="mb-4 p-3 bg-red-50 border border-red-200 text-red-700 rounded-lg text-sm">
              {error}
            </div>
          )}

          {/* Welcome */}
          {step === 'welcome' && (
            <div className="text-center">
              <div className="text-6xl mb-4">🗳️</div>
              <h1 className="text-2xl font-bold text-gray-900 mb-4">
                INEC Biometric Enrollment
              </h1>
              <p className="text-gray-600 mb-8">
                Welcome to the voter biometric enrollment kiosk. This process will capture your
                fingerprint, facial image, and iris scan for secure voter identification.
              </p>
              <ul className="text-left text-sm text-gray-500 mb-8 space-y-2 max-w-md mx-auto">
                <li>• Fingerprint scan (USB scanner or camera)</li>
                <li>• Facial photograph (front-facing camera)</li>
                <li>• Iris scan (high-resolution camera)</li>
                <li>• Liveness verification</li>
              </ul>
              <button
                onClick={goNext}
                className="px-8 py-3 bg-green-600 text-white rounded-lg font-medium hover:bg-green-700 transition"
              >
                Begin Enrollment
              </button>
            </div>
          )}

          {/* Identity */}
          {step === 'identity' && (
            <div>
              <h2 className="text-xl font-bold text-gray-900 mb-6">Personal Information</h2>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">VIN (Voter Identification Number)</label>
                  <input
                    type="text"
                    value={data.voterVin}
                    onChange={e => setData(d => ({ ...d, voterVin: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-green-500"
                    placeholder="VIN..."
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Registered BVAS Device ID</label>
                  <input
                    type="text"
                    value={data.deviceId}
                    onChange={e => setData(d => ({ ...d, deviceId: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-green-500"
                    placeholder="Enter the registered device ID"
                    autoComplete="off"
                  />
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-medium text-gray-700 mb-1">First Name</label>
                    <input
                      type="text"
                      value={data.firstName}
                      onChange={e => setData(d => ({ ...d, firstName: e.target.value }))}
                      className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-green-500"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 mb-1">Last Name</label>
                    <input
                      type="text"
                      value={data.lastName}
                      onChange={e => setData(d => ({ ...d, lastName: e.target.value }))}
                      className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-green-500"
                    />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-medium text-gray-700 mb-1">Date of Birth</label>
                    <input
                      type="date"
                      value={data.dateOfBirth}
                      onChange={e => setData(d => ({ ...d, dateOfBirth: e.target.value }))}
                      className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-green-500"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 mb-1">State</label>
                    <select
                      value={data.stateCode}
                      onChange={e => setData(d => ({ ...d, stateCode: e.target.value }))}
                      className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-green-500"
                    >
                      <option value="">Select state...</option>
                      {['AB','AD','AK','AN','BA','BY','BE','BO','CR','DE','EB','ED','EK','EN','GO','IM','JI','KD','KN','KT','KE','KO','KW','LA','NA','NI','OG','ON','OS','OY','PL','RI','SO','TA','YO','ZA','FC'].map(s => (
                        <option key={s} value={s}>{s}</option>
                      ))}
                    </select>
                  </div>
                </div>
              </div>
              <div className="flex justify-between mt-8">
                <button onClick={goBack} className="px-6 py-2 text-gray-600 hover:text-gray-800">Back</button>
                <button
                  onClick={goNext}
                  disabled={!data.voterVin || !data.deviceId || !data.firstName || !data.lastName}
                  className="px-8 py-3 bg-green-600 text-white rounded-lg font-medium hover:bg-green-700 disabled:opacity-40 disabled:cursor-not-allowed transition"
                >
                  Next
                </button>
              </div>
            </div>
          )}

          {/* Fingerprint */}
          {step === 'fingerprint' && (
            <div className="text-center">
              <h2 className="text-xl font-bold text-gray-900 mb-4">Fingerprint Capture</h2>
              <p className="text-gray-600 mb-6">
                {FingerprintScanner.isSupported()
                  ? 'Place your finger on the scanner and press Capture.'
                  : 'No USB scanner detected. Camera capture will be used as fallback.'}
              </p>
              <div className="relative mx-auto mb-6" style={{ width: 320, height: 240 }}>
                <video ref={videoRef} className="w-full h-full bg-gray-900 rounded-lg" playsInline muted />
                <canvas ref={canvasRef} className="absolute inset-0 w-full h-full" />
              </div>
              {quality && (
                <div className="mb-4 flex justify-center gap-4 text-xs">
                  <span>Quality: <b className={quality.overall > 0.6 ? 'text-green-600' : 'text-yellow-600'}>{(quality.overall * 100).toFixed(0)}%</b></span>
                  <span>Sharp: {(quality.sharpness * 100).toFixed(0)}%</span>
                  <span>Bright: {(quality.brightness * 100).toFixed(0)}%</span>
                </div>
              )}
              {data.fingerprint && (
                <p className="text-green-600 mb-4 text-sm font-medium">Fingerprint captured (quality: {(data.qualityScores.fingerprint! * 100).toFixed(0)}%)</p>
              )}
              <div className="flex justify-between mt-6">
                <button onClick={goBack} className="px-6 py-2 text-gray-600 hover:text-gray-800">Back</button>
                <div className="flex gap-3">
                  <button
                    onClick={captureFingerprint}
                    disabled={processing}
                    className="px-8 py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-40 transition"
                  >
                    {processing ? 'Capturing...' : 'Capture'}
                  </button>
                  {data.fingerprint && (
                    <button onClick={goNext} className="px-8 py-3 bg-green-600 text-white rounded-lg font-medium hover:bg-green-700 transition">
                      Next
                    </button>
                  )}
                </div>
              </div>
            </div>
          )}

          {/* Face */}
          {step === 'face' && (
            <div className="text-center">
              <h2 className="text-xl font-bold text-gray-900 mb-4">Face Capture</h2>
              <p className="text-gray-600 mb-6">
                Look directly at the camera. Ensure even lighting and remove glasses if possible.
              </p>
              <div className="relative mx-auto mb-6" style={{ width: 480, height: 360 }}>
                <video ref={videoRef} className="w-full h-full bg-gray-900 rounded-lg" playsInline muted />
                <canvas ref={canvasRef} className="absolute inset-0 w-full h-full" />
              </div>
              {quality && (
                <div className="mb-4 flex justify-center gap-4 text-xs">
                  <span>Quality: <b className={quality.overall > 0.6 ? 'text-green-600' : 'text-yellow-600'}>{(quality.overall * 100).toFixed(0)}%</b></span>
                  <span>Sharp: {(quality.sharpness * 100).toFixed(0)}%</span>
                  <span>Center: {(quality.centering * 100).toFixed(0)}%</span>
                  {quality.issues.map((issue, i) => <span key={i} className="text-red-500">{issue}</span>)}
                </div>
              )}
              {!data.face && !processing && (
                <button
                  onClick={() => startCamera('face')}
                  className="px-8 py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 mb-4 transition"
                >
                  Start Camera
                </button>
              )}
              {data.face && (
                <p className="text-green-600 mb-4 text-sm font-medium">Face captured (quality: {(data.qualityScores.face! * 100).toFixed(0)}%)</p>
              )}
              <div className="flex justify-between mt-6">
                <button onClick={goBack} className="px-6 py-2 text-gray-600 hover:text-gray-800">Back</button>
                <div className="flex gap-3">
                  {!data.face && (
                    <button
                      onClick={() => captureImage('face')}
                      disabled={processing}
                      className="px-8 py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-40 transition"
                    >
                      {processing ? 'Capturing...' : 'Capture'}
                    </button>
                  )}
                  {data.face && (
                    <button onClick={goNext} className="px-8 py-3 bg-green-600 text-white rounded-lg font-medium hover:bg-green-700 transition">
                      Next
                    </button>
                  )}
                </div>
              </div>
            </div>
          )}

          {/* Iris */}
          {step === 'iris' && (
            <div className="text-center">
              <h2 className="text-xl font-bold text-gray-900 mb-4">Iris Scan</h2>
              <p className="text-gray-600 mb-6">
                Hold the camera 10-15cm from your eye. Keep your eye wide open and look at the center dot.
              </p>
              <div className="relative mx-auto mb-6" style={{ width: 480, height: 360 }}>
                <video ref={videoRef} className="w-full h-full bg-gray-900 rounded-lg" playsInline muted />
                <canvas ref={canvasRef} className="absolute inset-0 w-full h-full" />
              </div>
              {quality && (
                <div className="mb-4 flex justify-center gap-4 text-xs">
                  <span>Quality: <b className={quality.overall > 0.6 ? 'text-green-600' : 'text-yellow-600'}>{(quality.overall * 100).toFixed(0)}%</b></span>
                  <span>Focus: {(quality.sharpness * 100).toFixed(0)}%</span>
                  {quality.issues.map((issue, i) => <span key={i} className="text-red-500">{issue}</span>)}
                </div>
              )}
              {!data.iris && !processing && (
                <button
                  onClick={() => startCamera('iris')}
                  className="px-8 py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 mb-4 transition"
                >
                  Start Camera
                </button>
              )}
              <div className="flex justify-between mt-6">
                <button onClick={goBack} className="px-6 py-2 text-gray-600 hover:text-gray-800">Back</button>
                <div className="flex gap-3">
                  {!data.iris && (
                    <button
                      onClick={() => captureImage('iris')}
                      disabled={processing}
                      className="px-8 py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-40 transition"
                    >
                      {processing ? 'Capturing...' : 'Capture'}
                    </button>
                  )}
                  {data.iris && (
                    <button onClick={goNext} className="px-8 py-3 bg-green-600 text-white rounded-lg font-medium hover:bg-green-700 transition">
                      Next
                    </button>
                  )}
                  <button onClick={goNext} className="px-6 py-2 text-gray-500 hover:text-gray-700">
                    Skip Iris
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* Liveness */}
          {step === 'liveness' && (
            <div className="text-center">
              <h2 className="text-xl font-bold text-gray-900 mb-4">Liveness Verification</h2>
              <p className="text-gray-600 mb-8">
                Verifying that the captured biometric data is from a live person (anti-spoofing check).
              </p>
              {data.livenessResult ? (
                <div className={`p-4 rounded-lg mb-6 ${
                  data.livenessResult.decision === 'live' ? 'bg-green-50 text-green-700' :
                  data.livenessResult.decision === 'unavailable' ? 'bg-yellow-50 text-yellow-700' :
                  'bg-red-50 text-red-700'
                }`}>
                  <p className="font-medium">
                    {data.livenessResult.decision === 'live' ? 'Liveness confirmed' :
                     data.livenessResult.decision === 'unavailable' ? 'PAD service unavailable — manual verification required' :
                     'Liveness check failed — potential spoofing detected'}
                  </p>
                  <p className="text-sm mt-1">Score: {(data.livenessResult.score * 100).toFixed(1)}%</p>
                </div>
              ) : (
                <button
                  onClick={performLiveness}
                  disabled={processing}
                  className="px-8 py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-40 transition"
                >
                  {processing ? 'Checking...' : 'Run Liveness Check'}
                </button>
              )}
              <div className="flex justify-between mt-8">
                <button onClick={goBack} className="px-6 py-2 text-gray-600 hover:text-gray-800">Back</button>
                {data.livenessResult && (
                  <button onClick={goNext} className="px-8 py-3 bg-green-600 text-white rounded-lg font-medium hover:bg-green-700 transition">
                    Next
                  </button>
                )}
              </div>
            </div>
          )}

          {/* Review */}
          {step === 'review' && (
            <div>
              <h2 className="text-xl font-bold text-gray-900 mb-6">Enrollment Review</h2>
              <div className="space-y-4 mb-8">
                <div className="grid grid-cols-2 gap-4 text-sm">
                  <div className="bg-gray-50 p-3 rounded"><b>VIN:</b> {data.voterVin}</div>
                  <div className="bg-gray-50 p-3 rounded"><b>Name:</b> {data.firstName} {data.lastName}</div>
                  <div className="bg-gray-50 p-3 rounded"><b>DOB:</b> {data.dateOfBirth}</div>
                  <div className="bg-gray-50 p-3 rounded"><b>State:</b> {data.stateCode}</div>
                </div>
                <div className="grid grid-cols-3 gap-4 text-sm">
                  <div className={`p-3 rounded border ${data.fingerprint ? 'border-green-300 bg-green-50' : 'border-gray-200'}`}>
                    <b>Fingerprint:</b> {data.fingerprint ? `Captured (${(data.qualityScores.fingerprint! * 100).toFixed(0)}%)` : 'Not captured'}
                  </div>
                  <div className={`p-3 rounded border ${data.face ? 'border-green-300 bg-green-50' : 'border-gray-200'}`}>
                    <b>Face:</b> {data.face ? `Captured (${(data.qualityScores.face! * 100).toFixed(0)}%)` : 'Not captured'}
                  </div>
                  <div className={`p-3 rounded border ${data.iris ? 'border-green-300 bg-green-50' : 'border-gray-200'}`}>
                    <b>Iris:</b> {data.iris ? `Captured (${(data.qualityScores.iris! * 100).toFixed(0)}%)` : 'Skipped'}
                  </div>
                </div>
                <div className={`p-3 rounded border text-sm ${
                  data.livenessResult?.decision === 'live' ? 'border-green-300 bg-green-50' : 'border-yellow-300 bg-yellow-50'
                }`}>
                  <b>Liveness:</b> {data.livenessResult?.decision || 'Not checked'}
                  {data.livenessResult?.score ? ` (${(data.livenessResult.score * 100).toFixed(1)}%)` : ''}
                </div>
              </div>
              <div className="flex justify-between">
                <button onClick={goBack} className="px-6 py-2 text-gray-600 hover:text-gray-800">Back</button>
                <button
                  onClick={submitEnrollment}
                  disabled={processing || (!data.fingerprint && !data.face)}
                  className="px-8 py-3 bg-green-600 text-white rounded-lg font-medium hover:bg-green-700 disabled:opacity-40 transition"
                >
                  {processing ? 'Enrolling...' : 'Submit Enrollment'}
                </button>
              </div>
            </div>
          )}

          {/* Complete */}
          {step === 'complete' && (
            <div className="text-center">
              <div className="text-6xl mb-4">✅</div>
              <h2 className="text-2xl font-bold text-green-700 mb-4">Enrollment Complete</h2>
              <p className="text-gray-600 mb-8">
                Biometric data for <b>{data.firstName} {data.lastName}</b> (VIN: {data.voterVin})
                has been securely enrolled and encrypted in the biometric vault.
              </p>
              <div className="bg-green-50 rounded-lg p-4 mb-8 text-sm text-left max-w-md mx-auto">
                <p><b>Modalities enrolled:</b></p>
                <ul className="list-disc list-inside ml-2 mt-1">
                  {data.fingerprint && <li>Fingerprint (quality: {(data.qualityScores.fingerprint! * 100).toFixed(0)}%)</li>}
                  {data.face && <li>Face (quality: {(data.qualityScores.face! * 100).toFixed(0)}%)</li>}
                  {data.iris && <li>Iris (quality: {(data.qualityScores.iris! * 100).toFixed(0)}%)</li>}
                </ul>
              </div>
              <button
                onClick={() => {
                  setStep('welcome');
                  setData({
                    voterVin: '', deviceId: '', firstName: '', lastName: '', dateOfBirth: '', stateCode: '',
                    qualityScores: {},
                  });
                }}
                className="px-8 py-3 bg-green-600 text-white rounded-lg font-medium hover:bg-green-700 transition"
              >
                Enroll Next Voter
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
