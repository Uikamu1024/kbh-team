import { Route, Routes } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import RootGate from "@/pages/RootGate";
import Onboarding from "@/pages/Onboarding";
import Home from "@/pages/Home";

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<RootGate />} />
      <Route path="/onboarding" element={<Onboarding />} />
      <Route element={<AppShell />}>
        <Route path="/home" element={<Home />} />
      </Route>
    </Routes>
  );
}
