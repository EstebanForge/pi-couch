import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./index.css";

const el = document.getElementById("root");
if (!el) throw new Error("missing #root");
createRoot(el).render(
	<StrictMode>
		<App />
	</StrictMode>,
);

// The worker carries the PWA fetch handler (desktop install) and the push
// events; the bell's subscribe path reuses this registration. Register at
// boot, not only on the bell tap, so install works from any page.
if ("serviceWorker" in navigator) {
	navigator.serviceWorker.register("/sw.js").catch(() => {});
}
