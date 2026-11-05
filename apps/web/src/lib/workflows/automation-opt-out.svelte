<script lang="ts">
	import InlineNotice from '$lib/components/inline-notice.svelte';
	import { Button } from '$lib/components/ui/button';
	import { m } from '$lib/paraglide/messages';
	let {
		optedOut,
		removedAt = '',
		removedBy = '',
		canEdit,
		busy,
		onremove
	}: {
		optedOut: boolean;
		removedAt?: string;
		removedBy?: string;
		canEdit: boolean;
		busy: boolean;
		onremove: () => void;
	} = $props();
</script>

{#if optedOut}
	<InlineNotice tone="info" message={m.workflows_opt_out_active()} />
	{#if canEdit}<Button variant="outline" size="sm" disabled={busy} onclick={onremove}
			>{m.workflows_opt_out_remove()}</Button
		>{/if}
{:else if removedAt}
	<p class="text-xs text-muted-foreground">
		{m.workflows_opt_out_removed({
			name: removedBy || m.workflows_opt_out_someone(),
			date: new Date(removedAt).toLocaleString()
		})}
	</p>
{/if}
