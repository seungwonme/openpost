import type { Definition, Source, Step } from './api';
import { m } from '$lib/paraglide/messages';
type Issue = { node: string; field: string; message: string };

const messageWindowMinutes = 24 * 60;

export function messageSourceIssues(source: Source): Issue[] {
	if (source.kind !== 'message_received') return [];
	const issues: Issue[] = [];
	if (!source.account_ids?.length)
		issues.push({ node: 'source', field: 'account_ids', message: m.workflows_required() });
	const limit = Number(source.daily_limit);
	if (!Number.isInteger(limit) || limit < 1 || limit > 1000)
		issues.push({
			node: 'source',
			field: 'daily_limit',
			message: m.workflows_message_daily_limit()
		});
	return issues;
}

/** Report an authored delay the provider window can never accommodate. */
export function messageDelayIssues(definition: Definition): Issue[] {
	if (definition.source.kind !== 'message_received') return [];
	const issues: Issue[] = [];
	function walk(steps: Step[], start: number): number {
		let elapsed = start;
		for (const step of steps) {
			if (step.kind === 'wait' && !step.inputs?.minutes?.reference) {
				const minutes = Number(step.inputs?.minutes?.literal);
				if (Number.isFinite(minutes)) elapsed += minutes;
			}
			if (step.kind === 'message_reply' && elapsed >= messageWindowMinutes)
				issues.push({
					node: step.id,
					field: 'text',
					message: m.workflows_message_delay_window()
				});
			elapsed = Math.max(elapsed, walk(step.then ?? [], elapsed), walk(step.else ?? [], elapsed));
		}
		return elapsed;
	}
	walk(definition.steps ?? [], 0);
	return issues;
}
