import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import NotFound from "@/pages/NotFound";
import { Route, Switch } from "wouter";
import ErrorBoundary from "./components/ErrorBoundary";
import { ThemeProvider } from "./contexts/ThemeContext";
import Home from "./pages/Home";
import JoinCampaign from "./pages/JoinCampaign";
import StakeholdersPage from "./pages/Stakeholders";
import EndorsementTracker from "./pages/EndorsementTracker";
import OfflineBanner from "./components/OfflineBanner";
import CampaignTimeline from "./pages/CampaignTimeline";
import VoterRegistration from "./pages/VoterRegistration";
import PollingUnitLocator from "./pages/PollingUnitLocator";
import VolunteerPortal from "./pages/VolunteerPortal";
import PressReleaseGenerator from "./pages/PressReleaseGenerator";
import SocialMediaCenter from "./pages/SocialMediaCenter";
import LegalCompliance from "./pages/LegalCompliance";
import OppositionResearch from "./pages/OppositionResearch";
import ElectionDayWarRoom from "./pages/ElectionDayWarRoom";
import ResultsProjection from "./pages/ResultsProjection";
import ManifestoBuilder from "./pages/ManifestoBuilder";
import PetitionDrive from "./pages/PetitionDrive";
import DiasporaOutreach from "./pages/DiasporaOutreach";
import PostElectionAnalytics from "./pages/PostElectionAnalytics";
import CandidateWebsite from "./pages/CandidateWebsite";
import MediaMonitoring from "./pages/MediaMonitoring";
import DebateCoach from "./pages/DebateCoach";
import FundraisingTracker from "./pages/FundraisingTracker";
import BudgetPlanner from "./pages/BudgetPlanner";
import CandidateProfilePage from "./pages/CandidateProfilePage";
import CampaignTeam from "./pages/CampaignTeam";
import MobileNav from "./components/MobileNav";
import Dashboard from "./pages/Dashboard";
import PetitionSignPage from "./pages/PetitionSignPage";
function Router() {
  // make sure to consider if you need authentication for certain routes
  return (
    <Switch>
      <Route path={"/"} component={Home} />
      <Route path={"/join"} component={JoinCampaign} />
      <Route path={"/stakeholders"} component={StakeholdersPage} />
      <Route path={"/endorsements"} component={EndorsementTracker} />
      <Route path={"/timeline"} component={CampaignTimeline} />
      <Route path={"/registration"} component={VoterRegistration} />
      <Route path={"/polling-units"} component={PollingUnitLocator} />
      <Route path={"/volunteers"} component={VolunteerPortal} />
      <Route path={"/press-release"} component={PressReleaseGenerator} />
      <Route path={"/social-media"} component={SocialMediaCenter} />
      <Route path={"/legal-compliance"} component={LegalCompliance} />
      <Route path={"/opposition-research"} component={OppositionResearch} />
      <Route path={"/war-room"} component={ElectionDayWarRoom} />
      <Route path={"/results"} component={ResultsProjection} />
      <Route path={"/manifesto"} component={ManifestoBuilder} />
      <Route path={"/petition"} component={PetitionDrive} />
      <Route path={"/diaspora"} component={DiasporaOutreach} />
      <Route path={"/post-election"} component={PostElectionAnalytics} />
      <Route path={"/candidate-website"} component={CandidateWebsite} />
      <Route path={"/media-monitoring"} component={MediaMonitoring} />
      <Route path={"/debate-coach"} component={DebateCoach} />
      <Route path={"/fundraising"} component={FundraisingTracker} />
      <Route path={"/budget"} component={BudgetPlanner} />
      <Route path={"/profile"} component={CandidateProfilePage} />
      <Route path={"/team"} component={CampaignTeam} />
      <Route path={"/dashboard"} component={Dashboard} />
      <Route path={"/sign/:petitionId"} component={PetitionSignPage} />
      <Route path={"/404"} component={NotFound} />
      {/* Final fallback route */}
      <Route component={NotFound} />
    </Switch>
  );
}

// NOTE: About Theme
// - First choose a default theme according to your design style (dark or light bg), than change color palette in index.css
//   to keep consistent foreground/background color across components
// - If you want to make theme switchable, pass `switchable` ThemeProvider and use `useTheme` hook

function App() {
  return (
    <ErrorBoundary>
      <ThemeProvider
        defaultTheme="light"
        // switchable
      >
        <TooltipProvider>
          <Toaster />
          <OfflineBanner />
          <Router />
          <MobileNav />
        </TooltipProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}

export default App;
