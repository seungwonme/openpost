import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import { page } from 'vitest/browser';
import AutomationOptOut from './automation-opt-out.svelte';
import { m } from '$lib/paraglide/messages';

describe('AutomationOptOut', () => {
	it('lets an editor remove an active opt-out', async () => {
		const onremove = vi.fn();
		render(AutomationOptOut, { optedOut: true, canEdit: true, busy: false, onremove });
		await expect.element(page.getByText(m.workflows_opt_out_active())).toBeVisible();
		await page.getByRole('button', { name: m.workflows_opt_out_remove() }).click();
		expect(onremove).toHaveBeenCalledOnce();
	});

	it('does not offer removal without edit access', async () => {
		render(AutomationOptOut, { optedOut: true, canEdit: false, busy: false, onremove: vi.fn() });
		await expect.element(page.getByText(m.workflows_opt_out_active())).toBeVisible();
		expect(page.getByRole('button').elements()).toHaveLength(0);
	});

	it('shows who removed the opt-out and when', async () => {
		render(AutomationOptOut, {
			optedOut: false,
			removedAt: '2026-11-05T10:00:00Z',
			removedBy: 'Sample editor',
			canEdit: true,
			busy: false,
			onremove: vi.fn()
		});
		await expect.element(page.getByText('Sample editor', { exact: false })).toBeVisible();
		expect(page.getByRole('button').elements()).toHaveLength(0);
	});
});
