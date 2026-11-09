import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import MessageSequenceFields from './message-sequence-fields.svelte';
import type { Step } from './api';

function followUp(inputs: Step['inputs']): Step {
	return {
		id: 'follow',
		kind: 'message_sequence',
		name: 'Follow up',
		inputs: { messages: { literal: [{ text: 'Here is your guide', delay_minutes: 0 }] }, ...inputs }
	};
}

describe('follow check fields', () => {
	it('turns the follow check on, edits it and turns it off', async () => {
		const oninput = vi.fn();
		const off = followUp({ engagement_id: { reference: 'source.engagement_id' } });
		const screen = render(MessageSequenceFields, { step: off, oninput });
		await screen
			.getByRole('checkbox', { name: 'Send the link only to people who follow this account' })
			.click();
		expect(oninput).toHaveBeenLastCalledWith('follow_gate', {
			literal: {
				button_title: 'Get the guide',
				button_url: 'https://',
				not_following_text:
					'We could not see your follow yet. Follow us, then tap "I followed" again.',
				max_checks: 3
			}
		});
		screen.unmount();

		const gate = {
			button_title: 'Open',
			button_url: 'https://example.com/guide',
			not_following_text: 'Follow first',
			max_checks: 3
		};
		const on = render(MessageSequenceFields, {
			step: followUp({
				engagement_id: { reference: 'source.engagement_id' },
				follow_gate: { literal: gate }
			}),
			oninput
		});
		await on.getByLabelText('Button title').fill('Open guide');
		expect(oninput).toHaveBeenLastCalledWith('follow_gate', {
			literal: { ...gate, button_title: 'Open guide' }
		});
		await on
			.getByRole('checkbox', {
				name: "Count only a tap on the private reply's quick reply button, not typed replies"
			})
			.click();
		expect(oninput).toHaveBeenLastCalledWith('follow_gate', {
			literal: { ...gate, quick_reply_only: true }
		});
		await on
			.getByRole('checkbox', { name: 'Send the link only to people who follow this account' })
			.click();
		expect(oninput).toHaveBeenLastCalledWith('follow_gate', { literal: null });
	});

	it('is offered only to comment campaigns', async () => {
		const screen = render(MessageSequenceFields, {
			step: followUp({ message_id: { reference: 'source.message_id' } }),
			oninput: vi.fn()
		});
		await expect.element(screen.getByLabelText('Message 1')).toBeVisible();
		expect(screen.getByText('Follow check').elements()).toHaveLength(0);
	});
});
