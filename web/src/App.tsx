import { Navigate, Route, Routes } from "react-router-dom";
import { Layout } from "./components/Layout";
import ProjectsPage from "./pages/ProjectsPage";
import ProjectPage from "./pages/ProjectPage";
import ChapterPage from "./pages/ChapterPage";
import BiblePage from "./pages/BiblePage";
import WritersPage from "./pages/WritersPage";
import SettingsPage from "./pages/SettingsPage";
import HistoryPage from "./pages/HistoryPage";
import StatsPage from "./pages/StatsPage";

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Navigate to="/projects" replace />} />
        <Route path="/projects" element={<ProjectsPage />} />
        <Route path="/projects/:projectId" element={<ProjectPage />} />
        <Route path="/projects/:projectId/bible" element={<BiblePage />} />
        <Route path="/chapters/:chapterId" element={<ChapterPage />} />
        <Route path="/chapters/:chapterId/history" element={<HistoryPage />} />
        <Route path="/writers" element={<WritersPage />} />
        <Route path="/writers/stats" element={<StatsPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="*" element={<div className="p-8 text-stone-600">There is nothing at this address.</div>} />
      </Route>
    </Routes>
  );
}
