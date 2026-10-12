import { App as KApp } from "konsta/react";
import { usePath } from "./nav";
import HomeScreen from "./screens/HomeList";
import SessionScreen from "./screens/Session";

function Screen() {
	const path = usePath();
	const m = path.match(/^\/sessions\/(.+)$/);
	if (m) {
		const key = decodeURIComponent(m[1]);
		return <SessionScreen key={key} sessionKey={key} />;
	}
	return <HomeScreen />;
}

export default function App() {
	return (
		<KApp theme="material" dark>
			<Screen />
		</KApp>
	);
}
