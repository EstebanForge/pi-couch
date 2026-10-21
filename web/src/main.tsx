import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./index.css";
import { activeScheme, applyScheme } from "./theme";

// Paint the stored scheme before the first render so a non-default
// pick never flashes the default.
applyScheme(activeScheme());

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
