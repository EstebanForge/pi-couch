import { App as KApp } from "konsta/react";
import { lazy, Suspense } from "react";
import { usePath } from "./nav";
import HomeScreen from "./screens/HomeList";

// Home stays eager. Session and secondary screens load on first visit.
const ChangesScreen = lazy(() => import("./screens/Changes"));
const FilesScreen = lazy(() => import("./screens/Files"));
const QuotasScreen = lazy(() => import("./screens/Quotas"));
const SessionScreen = lazy(() => import("./screens/Session"));
const SettingsScreen = lazy(() => import("./screens/Settings"));
const TerminalScreen = lazy(() => import("./screens/Terminal"));

function Screen() {
	const path = usePath();
	let m = path.match(/^\/sessions\/(.+)\/changes$/);
	if (m) {
		const key = decodeURIComponent(m[1]);
		return <ChangesScreen key={`${key}:changes`} sessionKey={key} />;
	}
	m = path.match(/^\/sessions\/(.+)\/files$/);
	if (m) {
		const key = decodeURIComponent(m[1]);
		return <FilesScreen key={`${key}:files`} sessionKey={key} />;
	}
	m = path.match(/^\/sessions\/(.+)\/terminal$/);
	if (m) {
		const key = decodeURIComponent(m[1]);
		return <TerminalScreen key={`${key}:terminal`} sessionKey={key} />;
	}
	m = path.match(/^\/sessions\/(.+)$/);
	if (m) {
		const key = decodeURIComponent(m[1]);
		return <SessionScreen key={key} sessionKey={key} />;
	}
	if (path === "/settings") {
		return <SettingsScreen />;
	}
	if (path === "/quotas") {
		return <QuotasScreen />;
	}
	return <HomeScreen />;
}

export default function App() {
	return (
		<KApp theme="material" dark className="dark">
			<Suspense fallback={null}>
				<Screen />
			</Suspense>
		</KApp>
	);
}
