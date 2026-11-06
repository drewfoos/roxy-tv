import { Navigate, Route, Routes, useLocation } from "react-router";
import { useSession } from "./useSession";
import { Login } from "../pages/Login";
import { Profiles } from "../pages/Profiles";
import { Home } from "../pages/Home";
import { Browse } from "../pages/Browse";
import { Discover } from "../pages/Discover";
import { DiscoverTitle } from "../pages/DiscoverTitle";
import { Lists } from "../pages/Lists";
import { AccountSection } from "../pages/account/AccountLayout";
import { Series } from "../pages/Series";
import { Movie } from "../pages/Movie";
import { Watch } from "../pages/Watch";
import { Legal } from "../pages/Legal";
import { LinkEnter, LinkTv } from "../pages/LinkTv";
import { SetupWizard, setupInProgressFor } from "../pages/Setup";
import { useEffect, useState } from "react";
import { api } from "../api/client";
import { session } from "../api/session";
import { Spinner } from "../components/Skeleton";
import { useScrollVar } from "../hooks/useScrollVar";
import { PageErrorBoundary } from "../components/PageErrorBoundary";

/** Signed out -> login; signed in without a profile -> "Who's watching?"; else the app. */
export default function App() {
  const s = useSession();
  const loc = useLocation();
  useScrollVar();
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [loc.pathname]);
  // Browser-loaded art and video authenticate by cookie: get it before the first page
  // asks for a poster, or the first paint would be a row of broken images.
  const [cookieFor, setCookieFor] = useState<string | null>(null);
  useEffect(() => {
    const token = s?.token;
    if (!token) return;
    let live = true;
    api
      .cookie()
      .catch(() => undefined)
      .then(() => live && setCookieFor(token));
    return () => {
      live = false;
    };
  }, [s?.token]);
  // The selected profile may have been deleted on another device since this browser
  // picked it: check once per sign-in and profile (see session.forgetMissingProfile).
  const profileId = s?.profileId ?? null;
  useEffect(() => {
    if (!s?.token || !profileId) return;
    let live = true;
    api
      .profiles()
      .then((r) => {
        // Only for the profile it was asked about: a switch meanwhile is checked on its own.
        if (live && session.get()?.profileId === profileId)
          session.forgetMissingProfile(r.profiles.map((p) => p.id));
      })
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [s?.token, profileId]);
  // Bumped when the setup wizard finishes, so this reads its flag again.
  const [, setSetupDone] = useState(0);

  if (!s) return <Login />;
  // Signed in mid-setup (the wizard made this admin): back to the wizard, even after a reload.
  if (s.isAdmin && setupInProgressFor() === s.userId)
    return <SetupWizard onFinish={() => setSetupDone((n) => n + 1)} />;
  if (cookieFor !== s.token) return <Spinner />;
  // A TV's sign-in link works without a profile picked: the TV picks its own.
  if (
    !s.profileId &&
    loc.pathname !== "/profiles" &&
    !loc.pathname.startsWith("/link")
  )
    return <Navigate to="/profiles" replace />;

  // A page that throws shows an error in its place instead of blanking the whole site;
  // keyed by page, so going to another page starts clean. Every /watch/:id is one page:
  // keying by the full path rebuilt the player for each episode, which dropped
  // fullscreen on Next and Autoplay. resetKey still clears an error on any path change.
  const pageKey = loc.pathname.startsWith("/watch/") ? "/watch" : loc.pathname;
  return (
    <PageErrorBoundary key={pageKey} resetKey={loc.pathname}>
      <Routes>
        <Route path="/profiles" element={<Profiles />} />
        <Route path="/" element={<Home />} />
        <Route path="/browse" element={<Browse />} />
        <Route path="/discover" element={<Discover />} />
        <Route path="/discover/:type/:tmdbId" element={<DiscoverTitle />} />
        <Route path="/lists" element={<Lists />} />
        <Route path="/account/*" element={<AccountSection />} />
        <Route path="/series/:id" element={<Series />} />
        <Route path="/movie/:id" element={<Movie />} />
        <Route path="/watch/:id" element={<Watch />} />
        <Route path="/legal/:doc" element={<Legal />} />
        <Route path="/link" element={<LinkEnter />} />
        <Route path="/link/:code" element={<LinkTv />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </PageErrorBoundary>
  );
}
