import { useState } from 'react';
import { AuthProvider, useAuth } from '@/lib/auth';
import { I18nProvider } from '@/lib/i18n';
import ErrorBoundary from '@/components/ErrorBoundary';
import Layout from '@/components/Layout';
import LoginPage from '@/pages/LoginPage';
import DashboardPage from '@/pages/DashboardPage';
import ElectionsPage from '@/pages/ElectionsPage';
import ResultsPage from '@/pages/ResultsPage';
import CollationPage from '@/pages/CollationPage';
import PollingUnitsPage from '@/pages/PollingUnitsPage';
import AuditPage from '@/pages/AuditPage';
import IncidentsPage from '@/pages/IncidentsPage';
import MapPage from '@/pages/MapPage';
import MiddlewarePage from '@/pages/MiddlewarePage';
import BVASPage from '@/pages/BVASPage';
import AnomalyDetectionPage from '@/pages/AnomalyDetectionPage';
import SMSVerificationPage from '@/pages/SMSVerificationPage';
import PublicAPIPage from '@/pages/PublicAPIPage';
import VoterRegistrationPage from '@/pages/VoterRegistrationPage';
import WorkflowEnginePage from '@/pages/WorkflowEnginePage';
import BVASSyncPage from '@/pages/BVASSyncPage';
import PortalIntegrationPage from '@/pages/PortalIntegrationPage';
import DataValidationPage from '@/pages/DataValidationPage';
import AdminConsolePage from '@/pages/AdminConsolePage';
import BiometricPage from '@/pages/BiometricPage';
import BlockchainPage from '@/pages/BlockchainPage';
import TrainingPage from '@/pages/TrainingPage';
import StakeholderPage from '@/pages/StakeholderPage';
import AIMonitoringPage from '@/pages/AIMonitoringPage';
import ProductionPage from '@/pages/ProductionPage';
import ObserverMonitoringPage from '@/pages/ObserverMonitoringPage';

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
  };

  return (
    <Layout currentPage={currentPage} onNavigate={setCurrentPage}>
      <ErrorBoundary>
        {pages[currentPage] || <DashboardPage />}
      </ErrorBoundary>
    </Layout>
  );
}

function App() {
  return (
    <AuthProvider>
      <I18nProvider>
        <AppContent />
      </I18nProvider>
    </AuthProvider>
  );
}

export default App;
