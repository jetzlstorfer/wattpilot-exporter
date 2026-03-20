(function () {
    const toggle = document.getElementById('themeToggle');
    if (!toggle) return;
    const darkMQ = window.matchMedia('(prefers-color-scheme: dark)');
    const setIcon = () => {
        toggle.textContent = document.documentElement.classList.contains('dark') ? '☀️' : '🌙';
    };
    setIcon();
    toggle.addEventListener('click', function () {
        const isDark = document.documentElement.classList.toggle('dark');
        if (isDark === darkMQ.matches) {
            localStorage.removeItem('theme');
        } else {
            localStorage.setItem('theme', isDark ? 'dark' : 'light');
        }
        setIcon();
    });
    darkMQ.addEventListener('change', function (e) {
        if (!localStorage.getItem('theme')) {
            if (e.matches) {
                document.documentElement.classList.add('dark');
            } else {
                document.documentElement.classList.remove('dark');
            }
            setIcon();
        }
    });
})();
