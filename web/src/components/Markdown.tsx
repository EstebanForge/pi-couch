import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

/** Assistant text renders as markdown (GFM). Raw HTML never renders:
 * there is no rehype-raw, so tags arrive as escaped plain text. User
 * bubbles and thinking stay plain text by design. */
export function Markdown({ text }: { text: string }) {
	return (
		<div className="md px-3 py-1 text-[15px] text-base-content">
			<ReactMarkdown
				remarkPlugins={[remarkGfm]}
				components={{
					a: (props) => <a {...props} target="_blank" rel="noopener noreferrer" />,
				}}
			>
				{text}
			</ReactMarkdown>
		</div>
	);
}
