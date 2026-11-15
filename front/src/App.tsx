import { Route, Routes } from "react-router-dom";
import { Fragment, lazy, Suspense } from "react";
import { Header } from "./components/layout/Header/Header";
import { Footer } from "./components/layout/Footer/Footer";
import { HomePage } from "./pages/HomePage";
import { ConferencesPage } from "./pages/ConferencesPage";
import { AboutPage } from "./pages/AboutPage";
import { SupportPage } from "./pages/SupportPage";
import { ConferenceDetailPage } from "./pages/ConferenceDetailPage";
import { ApplicationPage } from "./pages/ApplicationPage";
import { ApplicationDetailPage } from "./pages/ApplicationDetailPage";
import { LoginPage } from "./pages/LoginPage";
import { RegisterPage } from "./pages/RegisterPage";
import { PasswordPage } from "./pages/PasswordPage";
import { VerifyPage } from "./pages/VerifyPage";
import { ProfilePage } from "./pages/ProfilePage";
import { NotFoundPage } from "./pages/NotFoundPage";
import { OrganizerPage } from "./pages/OrganizerPage";
import { VerifyDocumentPage } from "./pages/VerifyDocumentPage";
import { UnsubscribedPage } from "./pages/UnsubscribedPage";
import { RequireAuth } from "./auth/RequireAuth";
import styles from "./App.module.css";
import { useLocale } from "./i18n/useLocale";

const ApiDocsPage = lazy(() => import("./pages/ApiDocsPage").then((module) => ({ default: module.ApiDocsPage })));

function App() {
  const { locale } = useLocale();
  return (
    <Fragment key={locale}>
      <Header />
      <main className={styles.main}>
        <div className={styles.container}>
          <Routes>
            <Route path="/" element={<HomePage />} />
            <Route path="/conferences" element={<ConferencesPage />} />
            <Route path="/conferences/:idOrSlug" element={<ConferenceDetailPage />} />
            <Route path="/conferences/:conferenceId/apply" element={<RequireAuth><ApplicationPage /></RequireAuth>} />
            <Route path="/applications/:applicationId" element={<RequireAuth><ApplicationDetailPage /></RequireAuth>} />
            <Route path="/about" element={<AboutPage />} />
            <Route path="/support" element={<SupportPage />} />
            <Route path="/docs" element={<Suspense fallback={<p>OpenAPI…</p>}><ApiDocsPage /></Suspense>} />
            <Route path="/login" element={<LoginPage />} />
            <Route path="/register" element={<RegisterPage />} />
            <Route path="/forgot-password" element={<PasswordPage />} />
            <Route path="/reset-password" element={<PasswordPage />} />
            <Route path="/verify" element={<VerifyPage />} />
            <Route path="/verify-document" element={<VerifyDocumentPage />} />
            <Route path="/unsubscribed" element={<UnsubscribedPage />} />
            <Route path="/profile/:section?" element={<RequireAuth><ProfilePage /></RequireAuth>} />
            <Route path="/organizer" element={<RequireAuth><OrganizerPage /></RequireAuth>} />
            <Route path="*" element={<NotFoundPage />} />
          </Routes>
        </div>
      </main>
      <Footer />
    </Fragment>
  );
}

export default App;
