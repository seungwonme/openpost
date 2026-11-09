<script lang="ts">
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import type { Step, Value } from './api';
	import {
		defaultFollowGate,
		followGate,
		maxButtonTitleLength,
		maxFollowChecks,
		type FollowGate
	} from './message-validation';
	import { m } from '$lib/paraglide/messages';
	let {
		step,
		readonly = false,
		oninput
	}: { step: Step; readonly?: boolean; oninput: (key: string, value: Value) => void } = $props();
	const gate = $derived(followGate(step.inputs?.follow_gate?.literal));
	const write = (next: FollowGate | null) => oninput('follow_gate', { literal: next });
	const update = (change: Partial<FollowGate>) => gate && write({ ...gate, ...change });
</script>

<fieldset class="space-y-3 rounded-lg border p-3">
	<legend class="px-1 text-sm font-medium">{m.workflows_follow_gate()}</legend>
	<label class="flex items-start gap-2 text-sm"
		><Checkbox
			checked={gate !== null}
			disabled={readonly}
			onCheckedChange={(checked) => write(checked === true ? defaultFollowGate() : null)}
		/>{m.workflows_follow_gate_enable()}</label
	>
	<p class="text-xs leading-5 text-muted-foreground">{m.workflows_follow_gate_help()}</p>
	{#if gate}
		<div class="space-y-2">
			<Label for={`${step.id}-button-title`}>{m.workflows_follow_gate_title()}</Label>
			<Input
				id={`${step.id}-button-title`}
				value={gate.button_title}
				maxlength={maxButtonTitleLength}
				disabled={readonly}
				oninput={(event) => update({ button_title: event.currentTarget.value })}
			/>
		</div>
		<div class="space-y-2">
			<Label for={`${step.id}-button-url`}>{m.workflows_follow_gate_url()}</Label>
			<Input
				id={`${step.id}-button-url`}
				type="url"
				inputmode="url"
				value={gate.button_url}
				disabled={readonly}
				oninput={(event) => update({ button_url: event.currentTarget.value })}
			/>
		</div>
		<div class="space-y-2">
			<Label for={`${step.id}-not-following`}>{m.workflows_follow_gate_text()}</Label>
			<Textarea
				id={`${step.id}-not-following`}
				value={gate.not_following_text}
				rows={3}
				disabled={readonly}
				oninput={(event) => update({ not_following_text: event.currentTarget.value })}
			/>
		</div>
		<div class="space-y-2">
			<Label for={`${step.id}-follow-checks`}>{m.workflows_follow_gate_checks()}</Label>
			<Input
				id={`${step.id}-follow-checks`}
				type="number"
				min={1}
				max={maxFollowChecks}
				value={gate.max_checks}
				disabled={readonly}
				oninput={(event) => update({ max_checks: Number(event.currentTarget.value) })}
			/>
		</div>
		<label class="flex items-start gap-2 text-sm"
			><Checkbox
				checked={gate.quick_reply_only === true}
				disabled={readonly}
				onCheckedChange={(checked) => update({ quick_reply_only: checked === true })}
			/>{m.workflows_follow_gate_quick_reply_only()}</label
		>
		<p class="text-xs leading-5 text-muted-foreground">{m.workflows_follow_gate_limits()}</p>
	{/if}
</fieldset>
