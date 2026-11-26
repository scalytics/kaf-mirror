const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const html = fs.readFileSync(path.join(__dirname, '../../web/index.html'), 'utf8');
function functionSource(name) {
    const start = html.indexOf(`        function ${name}(`);
    assert.ok(start >= 0, `missing ${name}`);
    return html.slice(start, html.indexOf('\n        function ', start + 1));
}

function dashboard(fetch = async () => ({ ok: true, json: async () => ({ timestamp: null }) })) {
    const tbody = { innerHTML: '' };
    const context = vm.createContext({
        document: { querySelector: () => tbody, getElementById: () => null },
        fetch, sessionStorage: { getItem: () => 'test-token' },
        can: () => false, asArray: value => Array.isArray(value) ? value : [],
        appState: { expandedJobs: new Set() },
    });
    vm.runInContext(['escapeHtml', 'renderJobFailure', 'updateJobsTable'].map(functionSource).join('\n'), context);
    return { tbody, render: jobs => context.updateJobsTable(jobs) };
}

test('failed and halted jobs show an escaped reason in the list and details', async () => {
    for (const status of ['failed', 'paused']) {
        const { tbody, render } = dashboard();
        await render([{ id: 'job-1', status, failed_reason: '<img src=x onerror="alert(1)"> & \'ransom\'\nsecond line' }]);
        assert.equal((tbody.innerHTML.match(/class="job-failure-reason"/g) || []).length, 2);
        assert.equal((tbody.innerHTML.match(/&lt;img src=x onerror=&quot;alert\(1\)&quot;&gt; &amp; &#39;ransom&#39;\nsecond line/g) || []).length, 2);
        assert.ok(!tbody.innerHTML.includes('<img'));
        assert.ok(tbody.innerHTML.includes('overflow-wrap: anywhere'));
    }
});

test('missing reasons and stale reasons on running jobs are hidden', async () => {
    for (const job of [
        { status: 'failed' }, { status: 'failed', failed_reason: null },
        { status: 'failed', failed_reason: '' },
        { status: 'active', failed_reason: 'old failure' },
        { status: 'stopped', failed_reason: 'old failure' }, {},
    ]) {
        const { tbody, render } = dashboard();
        await render([{ id: 'job-1', ...job }]);
        assert.ok(!tbody.innerHTML.includes('job-failure-reason'));
        assert.ok(!tbody.innerHTML.includes('old failure'));
    }
});

test('metrics failures do not hide failure reasons', async () => {
    for (const fetch of [async () => { throw new Error('offline'); }, async () => ({ ok: false })]) {
        const { tbody, render } = dashboard(fetch);
        await render([{ id: 'job-1', status: 'failed', failed_reason: 'produce denied' }]);
        assert.equal((tbody.innerHTML.match(/produce denied/g) || []).length, 2);
    }
});

test('refresh removes a resolved failure and handles an empty list', async () => {
    const { tbody, render } = dashboard();
    await render([{ id: 'job-1', status: 'failed', failed_reason: 'old failure' }]);
    await render([{ id: 'job-1', status: 'active' }]);
    assert.ok(!tbody.innerHTML.includes('old failure'));
    for (const jobs of [[], null]) {
        await render(jobs);
        assert.ok(tbody.innerHTML.includes('No replication jobs found'));
    }
});

test('job status and configuration text cannot inject HTML', async () => {
    const { tbody, render } = dashboard();
    const attack = '<svg onload="alert(1)">';
    await render([{ id: 'job-1', status: attack, name: attack, compression: attack,
        source_cluster_name: attack, target_cluster_name: attack }]);
    assert.ok(!tbody.innerHTML.includes('<svg'));
    assert.ok(!tbody.innerHTML.includes('class="status <'));
});
