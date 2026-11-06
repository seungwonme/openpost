<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { inboxQueryKeys, messageAutomationQueryOptions } from '@openpost/query-catalog';
	import { messageAutomationQueryAPI } from '$lib/query/message-automation';
	import { queryClient } from '$lib/query/client';
	import { workspaceCtx } from '$lib/stores/workspace.svelte';
	import { Button } from '$lib/components/ui/button';
	import InlineNotice from '$lib/components/inline-notice.svelte';
	import {
		removeConversationOptOut,
		resumeConversationAutomation,
		takeOverConversation
	} from './api';
	import AutomationOptOut from './automation-opt-out.svelte';
	import MessageReplyReview from './message-reply-review.svelte';
	import { m } from '$lib/paraglide/messages';
	let {
		workspaceID,
		conversationID,
		runID = ''
	}: { workspaceID: string; conversationID: string; runID?: string } = $props();
	let busy = $state(false),
		error = $state('');
	const canEdit = $derived(['admin', 'editor'].includes(workspaceCtx.currentWorkspace?.role ?? ''));
	const pendingStates = ['ready', 'sending'];
	const automation = createQuery(() => ({
		...messageAutomationQueryOptions(messageAutomationQueryAPI, workspaceID, conversationID),
		refetchInterval: (query) =>
			query.state.data?.replies?.some((reply) => pendingStates.includes(reply.state)) ? 3000 : false
	}));
	const replies = $derived(
		(automation.data?.replies ?? []).filter((reply) => !runID || reply.run_id === runID)
	);
	const active = $derived(
		replies.some((reply) =>
			['scheduled', 'awaiting_review', 'ready', 'sending'].includes(reply.state)
		)
	);
	const visible = $derived(
		Boolean(
			automation.data?.stopped_at ||
			automation.data?.lapsed_at ||
			automation.data?.opted_out_at ||
			automation.data?.opt_out_removed_at ||
			replies.length
		)
	);
	async function act(action: 'stop' | 'resume' | 'remove-opt-out') {
		const workspace = workspaceID,
			conversation = conversationID;
		busy = true;
		error = '';
		try {
			if (action === 'stop') await takeOverConversation(workspace, conversation);
			else if (action === 'resume') await resumeConversationAutomation(workspace, conversation);
			else await removeConversationOptOut(workspace, conversation);
			await queryClient.invalidateQueries({ queryKey: inboxQueryKeys.engagementRoot(workspace) });
		} catch (cause) {
			if (conversation === conversationID)
				error = cause instanceof Error ? cause.message : m.workflows_operation_failed();
		} finally {
			if (conversation === conversationID) busy = false;
		}
	}
</script>

{#if automation.isError}<InlineNotice tone="error" message={m.workflows_load_failed()} />{/if}
{#if visible}
	<section
		class="space-y-3 rounded-lg border bg-card p-3"
		aria-label={m.workflows_message_automation()}
	>
		{#if automation.data?.stopped_at}
			<InlineNotice
				tone="info"
				message={automation.data.stop_reason || m.workflows_message_stopped()}
			/>
			{#if automation.data.lapses_at}<p class="text-xs text-muted-foreground">
					{m.workflows_message_lapses({
						date: new Date(automation.data.lapses_at).toLocaleString()
					})}
				</p>{/if}
		{:else if automation.data?.lapsed_at}<p class="text-xs text-muted-foreground">
				{m.workflows_message_lapsed({
					date: new Date(automation.data.lapsed_at).toLocaleString()
				})}
			</p>{/if}
		<AutomationOptOut
			optedOut={Boolean(automation.data?.opted_out_at)}
			removedAt={automation.data?.opt_out_removed_at}
			removedBy={automation.data?.opt_out_removed_by}
			{canEdit}
			{busy}
			onremove={() => act('remove-opt-out')}
		/>
		{#each replies as reply (reply.id)}<MessageReplyReview {reply} />{/each}
		{#if error}<InlineNotice tone="error" message={error} />{/if}
		{#if canEdit}<div class="flex flex-wrap gap-2">
				{#if automation.data?.stopped_at}<Button
						variant="outline"
						size="sm"
						disabled={busy}
						onclick={() => act('resume')}>{m.workflows_message_resume()}</Button
					>{:else if active}<Button
						variant="outline"
						size="sm"
						disabled={busy}
						onclick={() => act('stop')}>{m.workflows_comment_takeover()}</Button
					>{/if}
			</div>{/if}
	</section>
{/if}
