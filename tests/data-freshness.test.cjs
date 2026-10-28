const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const path = require('node:path');
const { test } = require('node:test');
const { runInNewContext } = require('node:vm');

const script = readFileSync(path.join(__dirname, '../static/data-freshness.js'), 'utf8');
const oldVersion = '2026-10-28T12:00:00.123456Z';
const newVersion = '2026-10-28T12:00:00.654321Z';

function response(version = oldVersion, state = 'idle', status = 204) {
    return {
        status,
        headers: new Map([
            ['X-Wattpilot-Data-Updated-At', version],
            ['X-Wattpilot-Refresh-Status', state],
        ]),
    };
}

async function browser(responses, { renderedAt = oldVersion, hidden = false, element = true } = {}) {
    const snapshot = { dataset: { updatedAt: renderedAt } };
    Object.defineProperty(snapshot, 'textContent', {
        set() { assert.fail('freshness checks must not write UI status text'); },
    });
    const listeners = {};
    const timers = new Map();
    const requests = [];
    const warnings = [];
    let timerId = 0;
    let reloads = 0;
    const document = {
        hidden,
        getElementById: () => element ? snapshot : null,
        addEventListener: (event, fn) => { listeners[event] = fn; },
    };
    runInNewContext(script, {
        document,
        window: {
            location: { reload: () => { reloads++; } },
            addEventListener: (event, fn) => { listeners[event] = fn; },
        },
        fetch: async (url, options) => {
            requests.push({ url, options });
            const result = responses.shift();
            if (result instanceof Error) throw result;
            assert.ok(result, 'unexpected extra freshness request');
            return result;
        },
        AbortSignal,
        console: { warn: (...args) => warnings.push(args) },
        setTimeout: (fn, delay) => {
            timers.set(++timerId, { fn, delay });
            return timerId;
        },
        clearTimeout: (id) => timers.delete(id),
    });
    const flush = () => new Promise(setImmediate);
    await flush();
    return {
        document, requests, warnings, listeners,
        get reloads() { return reloads; },
        get delay() { return [...timers.values()][0]?.delay; },
        async tick() {
            assert.equal(timers.size, 1, 'checks must not overlap');
            const [id, timer] = [...timers.entries()][0];
            timers.delete(id);
            await timer.fn();
        },
        async event(name) {
            await listeners[name]();
            await flush();
        },
    };
}

test('fresh snapshots are checked every 30 seconds without browser caching or reloads', async () => {
    const page = await browser([response(), response()]);
    assert.equal(page.delay, 30000);
    assert.equal(page.warnings.length, 0);
    await page.tick();
    assert.equal(page.reloads, 0);
    assert.equal(page.requests.length, 2);
    assert.equal(page.requests[0].url, '/data-status');
    assert.equal(page.requests[0].options.cache, 'no-store');
    assert.ok(page.requests[0].options.signal instanceof AbortSignal);
});

test('an in-flight refresh updates the open page as soon as the new snapshot arrives', async () => {
    const page = await browser([response(oldVersion, 'refreshing'), response(newVersion)]);
    assert.equal(page.warnings.length, 0);
    assert.equal(page.delay, 1000);
    await page.tick();
    assert.equal(page.reloads, 1);
});

test('a refresh completed during rendering is detected on the first check', async () => {
    const page = await browser([response(newVersion)]);
    // Both versions are in the same second; do not truncate the snapshot token.
    assert.equal(page.reloads, 1);
});

test('a page rendered without data recovers automatically', async () => {
    const page = await browser([response(newVersion)], { renderedAt: '' });
    assert.equal(page.reloads, 1);
});

test('refresh failures retain cached data and continue checking for recovery', async () => {
    const page = await browser([response(oldVersion, 'failed'), response(newVersion)]);
    assert.equal(page.reloads, 0);
    assert.equal(page.warnings.length, 1);
    assert.match(page.warnings[0][0], /refresh failed.*cached data.*retrying automatically/);
    assert.equal(page.delay, 30000);
    await page.tick();
    assert.equal(page.reloads, 1);
});

test('network failures and authentication redirects are reported and retried', async () => {
    for (const failure of [new Error('offline'), response(null, null, 200)]) {
        const page = await browser([failure, response()]);
        assert.equal(page.reloads, 0);
        assert.equal(page.warnings.length, 1);
        assert.match(page.warnings[0][0], /Unable to check charging data freshness/);
        assert.equal(page.delay, 30000);
        await page.tick();
        assert.equal(page.warnings.length, 1);
    }
});

test('hidden tabs pause checks and check immediately on returning or restoring', async () => {
    const page = await browser([response(), response(), response()]);
    page.document.hidden = true;
    await page.event('visibilitychange');
    assert.equal(page.delay, undefined);
    assert.equal(page.requests.length, 1);
    page.document.hidden = false;
    await page.event('visibilitychange');
    assert.equal(page.requests.length, 2);
    assert.equal(page.delay, 30000);
    await page.event('pageshow');
    assert.equal(page.requests.length, 3);
    assert.equal(page.delay, 30000);
});

test('a tab opened in the background does not poll until visible', async () => {
    const page = await browser([response()], { hidden: true });
    assert.equal(page.requests.length, 0);
    page.document.hidden = false;
    await page.event('visibilitychange');
    assert.equal(page.requests.length, 1);
});

test('pages without snapshot metadata do not poll', async () => {
    const page = await browser([], { element: false });
    assert.equal(page.requests.length, 0);
    assert.equal(page.delay, undefined);
});
