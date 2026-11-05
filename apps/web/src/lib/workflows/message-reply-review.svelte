<script lang="ts">
	import { untrack } from 'svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import {
		inboxQueryKeys,
		workflowRunQueryOptions,
		type MessageAutomationReply
	} from '@openpost/query-catalog';
	import { workflowQueryAPI } from '$lib/query/workflows';
	import { queryClient } from '$lib/query/client';
	import { workspaceCtx } from '$lib/stores/workspace.svelte';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import InlineNotice from '$lib/components/inline-notice.svelte';
	import { approveRun } from './api';
	import { m } from '$lib/paraglide/messages';
	let { reply }: { reply: MessageAutomationReply } = $props();
	let text = $state(untrack(() => reply.body)),
		busy = $state(false),
		error = $state('');
	const canAdmin = $derived(workspaceCtx.currentWorkspace?.role === 'admin');
	const run = createQuery(() => ({
		...workflowRunQueryOptions(workflowQueryAPI, reply.workspace_id, reply.run_id),
		enabled: canAdmin && reply.state === 'awaiting_review'
	}));
	const tooLong = $derived(new TextEncoder().encode(text.trim()).length > 1000);
	const stateLabel = $derived(
		reply.state === 'awaiting_review'
			? m.workflows_comment_pending()
			: reply.state === 'sent'
				? m.workflows_message_sent()
				: reply.state === 'failed'
					? m.workflows_message_failed()
					: reply.state === 'uncertain'
						? m.workflows_message_uncertain()
						: reply.state === 'cancelled' || reply.state === 'skipped'
							? m.workflows_message_not_sent()
							: m.workflows_comment_sending()
	);
	async function approve() {
		const captured = reply;
		const current = run.data;
		if (!current || workspaceCtx.currentWorkspace?.id !== captured.workspace_id) return;
		busy = true;
		error = '';
		try {
			await approveRun(captured.workspace_id, current, 0, text);
			if (reply.id === captured.id)
				await queryClient.invalidateQueries({
					queryKey: inboxQueryKeys.engagementRoot(captured.workspace_id)
				});
		} catch (cause) {
			if (reply.id === captured.id)
				error = cause instanceof Error ? cause.message : m.workflows_operation_failed();
		} finally {
			if (reply.id === captured.id) busy = false;
		}
	}
</script>

<div class="space-y-2">
	<p class="text-xs font-medium">{stateLabel}</p>
	{#if reply.reason}<p class="text-xs text-muted-foreground" dir="auto">{reply.reason}</p>{/if}
	{#if reply.state === 'awaiting_review'}<Label for={`message-reply-${reply.id}`}
			>{m.workflows_comment_saved()}</Label
		><Textarea
			id={`message-reply-${reply.id}`}
			bind:value={text}
			rows={4}
			aria-invalid={tooLong}
		/>{#if tooLong}<p class="text-xs text-destructive">
				{m.workflows_comment_private_length()}
			</p>{/if}
		<Button
			size="sm"
			disabled={!canAdmin ||
				busy ||
				!text.trim() ||
				tooLong ||
				run.data?.state !== 'awaiting_approval'}
			onclick={approve}>{m.workflows_message_approve()}</Button
		>
	{:else}<p class="text-sm whitespace-pre-wrap" dir="auto">{reply.body}</p>{/if}
	{#if error}<InlineNotice tone="error" message={error} />{/if}
	<Button
		variant="ghost"
		size="sm"
		href={`/workflows?run=${encodeURIComponent(reply.run_id)}&workspace_id=${encodeURIComponent(reply.workspace_id)}`}
		>{m.workflows_comment_open_run()}</Button
	>
</div>
