<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import type { Step, Value } from './api';
	import MessageFollowGateFields from './message-follow-gate-fields.svelte';
	import {
		maxSequenceMessages,
		sequenceMessages,
		type SequenceMessage
	} from './message-validation';
	import { m } from '$lib/paraglide/messages';
	let {
		step,
		readonly = false,
		oninput
	}: { step: Step; readonly?: boolean; oninput: (key: string, value: Value) => void } = $props();
	const messages = $derived(sequenceMessages(step.inputs?.messages?.literal));
	const write = (next: SequenceMessage[]) => oninput('messages', { literal: next });
	const update = (index: number, change: Partial<SequenceMessage>) =>
		write(messages.map((message, i) => (i === index ? { ...message, ...change } : message)));
	function move(index: number, by: number) {
		const next = [...messages];
		const [moved] = next.splice(index, 1);
		next.splice(index + by, 0, moved);
		write(next);
	}
</script>

<fieldset class="space-y-3">
	<legend class="text-sm font-medium">{m.workflows_message_sequence_messages()}</legend>
	<p class="text-xs leading-5 text-muted-foreground">{m.workflows_message_sequence_limits()}</p>
	<ol class="space-y-3">
		{#each messages as message, index (index)}
			<li class="space-y-2 rounded-lg border p-3">
				<Label for={`${step.id}-message-${index}`}
					>{m.workflows_message_sequence_item({ number: index + 1 })}</Label
				>
				<Textarea
					id={`${step.id}-message-${index}`}
					value={message.text}
					rows={3}
					disabled={readonly}
					oninput={(event) => update(index, { text: event.currentTarget.value })}
				/>
				<Label for={`${step.id}-delay-${index}`}>{m.workflows_message_sequence_delay()}</Label>
				<Input
					id={`${step.id}-delay-${index}`}
					type="number"
					min={0}
					max={1439}
					value={message.delay_minutes}
					disabled={readonly}
					oninput={(event) => update(index, { delay_minutes: Number(event.currentTarget.value) })}
				/>
				{#if !readonly}<div class="flex flex-wrap gap-2">
						<Button
							variant="ghost"
							size="sm"
							disabled={index === 0}
							aria-label={`${m.workflows_message_sequence_up()}: ${index + 1}`}
							onclick={() => move(index, -1)}>{m.workflows_message_sequence_up()}</Button
						>
						<Button
							variant="ghost"
							size="sm"
							disabled={index === messages.length - 1}
							aria-label={`${m.workflows_message_sequence_down()}: ${index + 1}`}
							onclick={() => move(index, 1)}>{m.workflows_message_sequence_down()}</Button
						>
						<Button
							variant="ghost"
							size="sm"
							disabled={messages.length === 1}
							aria-label={`${m.workflows_message_sequence_remove()}: ${index + 1}`}
							onclick={() => write(messages.filter((_, i) => i !== index))}
							>{m.workflows_message_sequence_remove()}</Button
						>
					</div>{/if}
			</li>
		{/each}
	</ol>
	<p class="text-xs leading-5 text-muted-foreground">{m.workflows_message_sequence_delay_help()}</p>
	{#if !readonly}<Button
			variant="outline"
			size="sm"
			disabled={messages.length >= maxSequenceMessages}
			onclick={() => write([...messages, { text: '', delay_minutes: 60 }])}
			>{m.workflows_message_sequence_add()}</Button
		>{/if}
</fieldset>
{#if step.inputs?.engagement_id}<MessageFollowGateFields {step} {readonly} {oninput} />{/if}
