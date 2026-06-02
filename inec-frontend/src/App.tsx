import { useState, useEffect, Suspense, lazy } from 'react';
import { AuthProvider, useAuth } from '@/lib/auth';
import { I18nProvider } from '@/lib/i18n';
import { ThemeProvider } from '@/components/ThemeProvider';
import { ToastProvider } from '@/components/Toast';
import { OfflineBanner } from '@/components/OfflineBanner';
import { InstallPrompt } from '@/components/InstallPrompt';
import ErrorBoundary from '@/components/ErrorBoundary';
import Layout from '@/components/Layout';
import LoginPage from '@/pages/LoginPage';
import { DashboardSkeleton } from '@/components/Skeleton';

const DashboardPage = lazy(() => import('@/pages/DashboardPage'));
const ElectionsPage = lazy(() => import('@/pages/ElectionsPage'));
const ResultsPage = lazy(() => import('@/pages/ResultsPage'));
const CollationPage = lazy(() => import('@/pages/CollationPage'));
const PollingUnitsPage = lazy(() => import('@/pages/PollingUnitsPage'));
const AuditPage = lazy(() => import('@/pages/AuditPage'));
const IncidentsPage = lazy(() => import('@/pages/IncidentsPage'));
const MapPage = lazy(() => import('@/pages/MapPage'));
const MiddlewarePage = lazy(() => import('@/pages/MiddlewarePage'));
const BVASPage = lazy(() => import('@/pages/BVASPage'));
const AnomalyDetectionPage = lazy(() => import('@/pages/AnomalyDetectionPage'));
const SMSVerificationPage = lazy(() => import('@/pages/SMSVerificationPage'));
const PublicAPIPage = lazy(() => import('@/pages/PublicAPIPage'));
const VoterRegistrationPage = lazy(() => import('@/pages/VoterRegistrationPage'));
const WorkflowEnginePage = lazy(() => import('@/pages/WorkflowEnginePage'));
const BVASSyncPage = lazy(() => import('@/pages/BVASSyncPage'));
const PortalIntegrationPage = lazy(() => import('@/pages/PortalIntegrationPage'));
const DataValidationPage = lazy(() => import('@/pages/DataValidationPage'));
const AdminConsolePage = lazy(() => import('@/pages/AdminConsolePage'));
const BiometricPage = lazy(() => import('@/pages/BiometricPage'));
const BlockchainPage = lazy(() => import('@/pages/BlockchainPage'));
const TrainingPage = lazy(() => import('@/pages/TrainingPage'));
const StakeholderPage = lazy(() => import('@/pages/StakeholderPage'));
const AIMonitoringPage = lazy(() => import('@/pages/AIMonitoringPage'));
const ProductionPage = lazy(() => import('@/pages/ProductionPage'));
const ObserverMonitoringPage = lazy(() => import('@/pages/ObserverMonitoringPage'));
const DisputeResolutionPage = lazy(() => import('@/pages/DisputeResolutionPage'));
const KYCVerificationPage = lazy(() => import('@/pages/KYCVerificationPage'));
const ScaleHealthPage = lazy(() => import('@/pages/ScaleHealthPage'));

function PageTransition({ page, children }: { page: string; children: React.ReactNode }) {
  const [show, setShow] = useState(false);

  useEffect(() => {
    setShow(false);
    const frame = requestAnimationFrame(() => setShow(true));
    return () => cancelAnimationFrame(frame);
  }, [page]);

  return (
    <div className={`transition-all duration-200 ease-out ${show ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-1'}`}>
      {children}
    </div>
  );
}

function AppContent() {
  const { isAuthenticated } = useAuth();
  const [currentPage, setCurrentPage] = useState('dashboard');

  if (!isAuthenticated) return <LoginPage />;

  const pages: Record<string, React.ReactNode> = {
    dashboard: <DashboardPage />,
    map: <MapPage />,
    elections: <ElectionsPage />,
    results: <ResultsPage />,
    collation: <CollationPage />,
    'polling-units': <PollingUnitsPage />,
    audit: <AuditPage />,
    incidents: <IncidentsPage />,
    middleware: <MiddlewarePage />,
    bvas: <BVASPage />,
    'anomaly-detection': <AnomalyDetectionPage />,
    'sms-verification': <SMSVerificationPage />,
    'public-api': <PublicAPIPage />,
    'voter-registration': <VoterRegistrationPage />,
    'workflow-engine': <WorkflowEnginePage />,
    'bvas-sync': <BVASSyncPage />,
    'portal-integration': <PortalIntegrationPage />,
    'data-validation': <DataValidationPage />,
    'admin-console': <AdminConsolePage />,
    'biometric': <BiometricPage />,
    'blockchain': <BlockchainPage />,
    'training': <TrainingPage />,
    'stakeholders': <StakeholderPage />,
    'ai-monitoring': <AIMonitoringPage />,
    'production': <ProductionPage />,
    'observer-monitoring': <ObserverMonitoringPage />,
    'dispute-resolution': <DisputeResolutionPage />,
    'kyc-verification': <KYCVerificationPage />,
    'scale-health': <ScaleHealthPage />,
  };

  return (
    <Layout currentPage={currentPage} onNavigate={setCurrentPage}>
      <ErrorBoundary>
        <PageTransition page={currentPage}>
          <Suspense fallback={<DashboardSkeleton />}>
            {pages[currentPage] || <DashboardPage />}
          </Suspense>
        </PageTransition>
      </ErrorBoundary>
    </Layout>
  );
}

function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <I18nProvider>
          <ToastProvider>
            <OfflineBanner />
            <AppContent />
            <InstallPrompt />
          </ToastProvider>
        </I18nProvider>
      </AuthProvider>
    </ThemeProvider>
  );
}

export default App;
