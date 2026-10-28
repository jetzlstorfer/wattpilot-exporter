(function () {
    const snapshot = document.getElementById('data-freshness');
    if (!snapshot) return;

    // Compare against the snapshot actually rendered, not a later status read:
    // a background refresh can finish while the server is rendering the page.
    const renderedAt = snapshot.dataset.updatedAt;
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
                delay = 1000;
            } else if (state === 'failed') {
                console.warn('Charging data refresh failed; keeping cached data and retrying automatically.');
            }
        } catch (error) {
            console.warn('Unable to check charging data freshness', error);
        } finally {
            checking = false;
            timer = setTimeout(check, delay);
        }
    }

    document.addEventListener('visibilitychange', check);
    window.addEventListener('pageshow', check);
    check();
})();
