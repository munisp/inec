import { useState } from 'react';
import { AuthProvider, useAuth } from '@/lib/auth';
import { I18nProvider } from '@/lib/i18n';
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
  };

  return (
    <Layout currentPage={currentPage} onNavigate={setCurrentPage}>
      {pages[currentPage] || <DashboardPage />}
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
