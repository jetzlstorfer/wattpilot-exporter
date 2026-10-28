(function () {
    const status = document.getElementById('data-freshness');
    if (!status) return;

    // Compare against the snapshot actually rendered, not a later status read:
    // a background refresh can finish while the server is rendering the page.
    const renderedAt = status.dataset.updatedAt;
    const lastUpdated = renderedAt ? `Last updated: ${new Date(renderedAt).toLocaleString()}. ` : '';
    let timer;
    let checking = false;

    async function check() {
        clearTimeout(timer);
        if (document.hidden || checking) return;
        checking = true;
        let delay = 30000;
        try {
            const response = await fetch('/data-status', {
                cache: 'no-store',
                signal: AbortSignal.timeout(10000),
            });
            if (response.status !== 204) {
                throw new Error(`Freshness check returned HTTP ${response.status}`);
            }
            const updatedAt = response.headers.get('X-Wattpilot-Data-Updated-At');
            if (updatedAt && updatedAt !== renderedAt) {
                window.location.reload();
                return;
            }
            const state = response.headers.get('X-Wattpilot-Refresh-Status');
            if (state === 'refreshing') {
                status.textContent = `${lastUpdated}Refreshing charging data...`;
                delay = 1000;
            } else if (state === 'failed') {
                status.textContent = renderedAt
                    ? `${lastUpdated}Refresh failed. Showing cached data; will retry automatically.`
                    : 'Refresh failed. Will retry automatically.';
            } else {
                status.textContent = `${lastUpdated}Automatic freshness checks enabled.`;
            }
        } catch (error) {
            console.warn('Unable to check charging data freshness', error);
            status.textContent = `${lastUpdated}Unable to check for updates; will retry automatically.`;
        } finally {
            checking = false;
            timer = setTimeout(check, delay);
        }
    }

    document.addEventListener('visibilitychange', check);
    window.addEventListener('pageshow', check);
    check();
})();
