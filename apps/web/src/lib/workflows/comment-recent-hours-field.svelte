<script lang="ts">
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import type { Source } from './api';
	import { m } from '$lib/paraglide/messages';

	// Fork: comment campaigns may answer comments from before they started.
	let { source, onchange }: { source: Source; onchange: (source: Source) => void } = $props();

	function update(value: string) {
		const hours = Number(value);
		onchange({
			...source,
			include_recent_comments_hours: Number.isInteger(hours) && hours > 0 ? hours : undefined
		});
	}
</script>

<div class="space-y-2">
	<Label for="comment-recent-hours">{m.workflows_comment_recent_hours()}</Label><Input
		id="comment-recent-hours"
		type="number"
		min={0}
		max={168}
		step={1}
		value={source.include_recent_comments_hours ?? 0}
		aria-describedby="comment-recent-hours-help"
		oninput={(event) => update(event.currentTarget.value)}
	/>
	<p id="comment-recent-hours-help" class="text-xs text-muted-foreground">
		{m.workflows_comment_recent_hours_help()}
	</p>
</div>
