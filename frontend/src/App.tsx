import { Route, Routes } from "react-router-dom";
import RootGate from "@/pages/RootGate";

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<RootGate />} />
    </Routes>
  );
}
