<script lang="ts">
	import type { SocialAccount } from '@openpost/query-catalog';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import InlineNotice from '$lib/components/inline-notice.svelte';
	import Disclosure from '$lib/components/disclosure.svelte';
	import Choice from './choice.svelte';
	import type { Source } from './api';
	import { m } from '$lib/paraglide/messages';
	let {
		source,
		accounts,
		onchange
	}: {
		source: Source;
		accounts: SocialAccount[];
		onchange: (source: Source) => void;
	} = $props();
	const eligible = $derived(accounts.filter((account) => account.platform === 'instagram'));
	const accountID = $derived(source.account_ids?.[0] ?? '');
	const split = (value: string) =>
		value
			.split(',')
			.map((keyword) => keyword.trim())
			.filter(Boolean);
</script>

<div class="space-y-4">
	<div class="space-y-2">
		<Label for="message-account">{m.accounts_heading()}</Label><Choice
			id="message-account"
			value={accountID}
			options={[
				{ value: '', label: m.workflows_required() },
				...eligible.map((account) => ({
					value: account.id,
					label: account.account_username || account.platform
				}))
			]}
			onchange={(id) => onchange({ ...source, account_ids: id ? [id] : [] })}
		/>
	</div>
	{#if !eligible.length}<InlineNotice
			tone="info"
			message={m.workflows_message_no_accounts()}
		/>{/if}
	<div class="space-y-2">
		<Label for="message-keywords">{m.workflows_comment_keywords()}</Label><Input
			id="message-keywords"
			value={(source.keywords ?? []).join(', ')}
			onchange={(event) => onchange({ ...source, keywords: split(event.currentTarget.value) })}
		/>
		<p class="text-xs text-muted-foreground">{m.workflows_message_keywords_help()}</p>
	</div>
	<Disclosure summary={m.workflows_configure()} class="space-y-4">
		<div class="space-y-2">
			<Label for="message-exclude">{m.workflows_comment_exclude()}</Label><Input
				id="message-exclude"
				value={(source.excluded_keywords ?? []).join(', ')}
				onchange={(event) =>
					onchange({ ...source, excluded_keywords: split(event.currentTarget.value) })}
			/>
		</div>
		<div class="space-y-2">
			<Label for="message-match">{m.workflows_comment_match()}</Label><Choice
				id="message-match"
				value={source.match_mode ?? 'word'}
				options={[
					{ value: 'word', label: m.workflows_comment_word() },
					{ value: 'exact', label: m.workflows_comment_exact() },
					{ value: 'contains', label: m.workflows_comment_contains() }
				]}
				onchange={(match_mode) =>
					onchange({ ...source, match_mode: match_mode as Source['match_mode'] })}
			/>
		</div>
		<div class="space-y-2">
			<Label for="message-daily">{m.workflows_message_daily_limit()}</Label><Input
				id="message-daily"
				type="number"
				min={1}
				max={1000}
				value={source.daily_limit ?? 100}
				oninput={(event) => onchange({ ...source, daily_limit: Number(event.currentTarget.value) })}
			/>
		</div>
		<div class="space-y-2">
			<Label for="message-priority">{m.workflows_comment_priority()}</Label><Input
				id="message-priority"
				type="number"
				min={1}
				max={100}
				value={source.priority ?? 50}
				oninput={(event) => onchange({ ...source, priority: Number(event.currentTarget.value) })}
			/>
			<p class="text-xs text-muted-foreground">{m.workflows_message_priority_help()}</p>
		</div>
	</Disclosure>
	<p class="text-xs leading-5 text-muted-foreground">{m.workflows_message_received_help()}</p>
</div>
