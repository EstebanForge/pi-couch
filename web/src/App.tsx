import { App as KApp } from "konsta/react";
import { usePath } from "./nav";
import ChangesScreen from "./screens/Changes";
import FilesScreen from "./screens/Files";
import HomeScreen from "./screens/HomeList";
import SessionScreen from "./screens/Session";

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
	m = path.match(/^\/sessions\/(.+)$/);
	if (m) {
		const key = decodeURIComponent(m[1]);
		return <SessionScreen key={key} sessionKey={key} />;
	}
	return <HomeScreen />;
}

export default function App() {
	return (
		<KApp theme="material" dark className="dark">
			<Screen />
		</KApp>
	);
}
