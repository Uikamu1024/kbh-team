import { Route, Routes } from "react-router-dom";
import RootGate from "@/pages/RootGate";
import Onboarding from "@/pages/Onboarding";

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<RootGate />} />
      <Route path="/onboarding" element={<Onboarding />} />
    </Routes>
  );
}
