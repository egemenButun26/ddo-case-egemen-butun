import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { App } from "./App";
import { AuthProvider } from "./auth/AuthContext";
import "./styles.css";

const kok = document.getElementById("root");
if (!kok) {
  throw new Error("root elemanı bulunamadı.");
}

createRoot(kok).render(
  <BrowserRouter>
    <AuthProvider>
      <App />
    </AuthProvider>
  </BrowserRouter>
);
