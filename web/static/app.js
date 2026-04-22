/* ============================================
   Moca Tracker — Dashboard App Logic
   ============================================ */

// ---- Logout ----
function doLogout() {
    try {
        // Synchronous XHR ensures the Set-Cookie (clear) response is processed
        // before we navigate away. This is intentionally blocking.
        var xhr = new XMLHttpRequest();
        xhr.open('POST', '/api/auth/logout', false); // synchronous
        xhr.send();
    } catch (e) {
        // Network error — still redirect
    }
    window.location.href = '/login';
}

// ---- Theme (runs immediately to prevent flash) ----
function updateLogoForTheme(theme) {
    const src = theme === 'light' ? '/logo-light.png?v=3' : '/logo.png?v=3';
    document.querySelectorAll('.logo-img, .mobile-logo-img').forEach(img => {
        img.src = src;
    });
}

(function initTheme() {
    const saved = localStorage.getItem('theme');
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    const theme = saved || (prefersDark ? 'dark' : 'dark'); // default dark
    document.documentElement.setAttribute('data-theme', theme);
    document.addEventListener('DOMContentLoaded', () => updateLogoForTheme(theme));
})();

function toggleTheme() {
    const html = document.documentElement;
    const current = html.getAttribute('data-theme') || 'dark';
    const next = current === 'dark' ? 'light' : 'dark';
    html.setAttribute('data-theme', next);
    localStorage.setItem('theme', next);
    updateLogoForTheme(next);
    // Re-render charts with new theme colors
    if (statsData) {
        updateDashboard(statsData);
    }
}

// ---- Custom Confirm Modal ----
let currentConfirmResolve = null;

function showConfirm(title, message) {
    return new Promise(resolve => {
        document.getElementById('confirmModalTitle').textContent = title;
        document.getElementById('confirmModalMessage').textContent = message;
        document.getElementById('confirmModal').style.display = 'flex';
        currentConfirmResolve = resolve;
    });
}

function closeConfirmModal() {
    document.getElementById('confirmModal').style.display = 'none';
    if(currentConfirmResolve) currentConfirmResolve(false);
    currentConfirmResolve = null;
}

function confirmModalOk() {
    document.getElementById('confirmModal').style.display = 'none';
    if(currentConfirmResolve) currentConfirmResolve(true);
    currentConfirmResolve = null;
}

// ---- State ----
let currentView = 'dashboard';
let statsData = null;
let chartMode = 'racc';      // racc | sav
let breakdownMode = 'dept';    // dept | zone
let notifications = [];

// ---- Init ----
document.addEventListener('DOMContentLoaded', () => {
    initNavigation();
    initUpload();
    initFilters();
    initChartTabs();
    initBreakdownTabs();
    initNTPClock();
    refreshData();
    setInterval(refreshData, 30000); // 30s — avoid re-triggering animations

    // After initial stagger animations, disable them so data refreshes don't replay
    setTimeout(() => {
        document.querySelectorAll('.metric-card, .chart-card, .card, .view-header').forEach(el => {
            el.style.animation = 'none';
        });
    }, 1500);

    // Restore view from URL hash on load
    const hash = location.hash.replace('#', '');
    const validViews = ['dashboard', 'interventions', 'technicians', 'planning', 'failures', 'notifications', 'import', 'settings'];
    if (hash && validViews.includes(hash)) {
        switchView(hash, true);
    }

    // Handle browser back/forward
    window.addEventListener('hashchange', () => {
        const h = location.hash.replace('#', '');
        if (h && h !== currentView) {
            switchView(h, true);
        }
    });
});

// ---- Navigation ----
function initNavigation() {
    document.querySelectorAll('.nav-item[data-view]').forEach(item => {
        item.addEventListener('click', (e) => {
            e.preventDefault();
            switchView(item.dataset.view);
        });
    });

    const sidebar = document.getElementById('sidebar');
    const overlay = document.getElementById('sidebarOverlay');
    const mobileBtn = document.getElementById('mobileMenuBtn');
    const toggleBtn = document.getElementById('sidebarToggle');

    // Desktop: hamburger button in sidebar header toggles collapsed
    // Mobile: same button closes sidebar
    if (toggleBtn) {
        toggleBtn.addEventListener('click', () => {
            if (window.innerWidth <= 768) {
                // Mobile: close sidebar
                sidebar.classList.remove('open');
                overlay.classList.remove('active');
            } else {
                // Desktop: toggle collapsed
                sidebar.classList.toggle('collapsed');
            }
        });
    }

    // Mobile header hamburger: open sidebar
    if (mobileBtn) {
        mobileBtn.addEventListener('click', () => {
            if (window.innerWidth <= 768) {
                sidebar.classList.add('open');
                overlay.classList.add('active');
            } else {
                // Desktop: un-collapse the sidebar
                sidebar.classList.remove('collapsed');
            }
        });
    }

    // Close sidebar on overlay click
    if (overlay) {
        overlay.addEventListener('click', () => {
            sidebar.classList.remove('open');
            overlay.classList.remove('active');
        });
    }

    // Close sidebar on nav click (mobile only)
    document.querySelectorAll('.nav-item').forEach(item => {
        item.addEventListener('click', () => {
            if (window.innerWidth <= 768) {
                sidebar.classList.remove('open');
                overlay.classList.remove('active');
            }
        });
    });
}

// View name → page title mapping
const viewTitles = {
    dashboard: 'Dashboard',
    interventions: 'Interventions',
    technicians: 'Techniciens',
    planning: 'Planning',
    failures: 'Échecs NOK',
    notifications: 'Notifications',
    import: 'Import',
    settings: 'Paramètres'
};

function switchView(view, fromHash) {
    currentView = view;
    document.querySelectorAll('.view').forEach(v => v.classList.remove('active'));
    document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));

    const el = document.getElementById(`view-${view}`);
    const nav = document.getElementById(`nav-${view}`);
    if (el) el.classList.add('active');
    if (nav) nav.classList.add('active');

    // Update URL hash (without triggering hashchange when we set it)
    if (!fromHash) {
        history.pushState(null, '', `#${view}`);
    }

    // Update page title
    document.title = `Moca Consult — ${viewTitles[view] || 'Dashboard'}`;

    // Load data for specific views
    if (view === 'notifications') loadNotifications();
    if (view === 'settings') loadSettings();
}

// ---- NTP / Analog Clock ----
function initNTPClock() {
    fetchNTPTime();
    setInterval(fetchNTPTime, 10000); // refresh network time slowly
    
    // Spring-animated ultra sleek Framer analog clock ticker
    setInterval(tickAnalogClock, 1000);
    tickAnalogClock();
}

function tickAnalogClock() {
    const now = new Date();
    const sec = now.getSeconds();
    const min = now.getMinutes();
    const hr = now.getHours();
    
    // Compute exact degrees
    const secDeg = (sec / 60) * 360;
    const minDeg = (min / 60) * 360 + (sec / 60) * 6; 
    const hrDeg = (hr % 12 / 12) * 360 + (min / 60) * 30;
    
    const secHand = document.getElementById('secondHand');
    const minHand = document.getElementById('minuteHand');
    const hrHand = document.getElementById('hourHand');
    
    // Prevent the 59 -> 0 backwards spin transition glitch
    if (sec === 0 && secHand) secHand.style.transition = 'none';
    else if (secHand) secHand.style.transition = 'transform 0.05s cubic-bezier(0.4, 2.08, 0.55, 0.44)';
    
    if (secHand) secHand.style.transform = `translateX(-50%) rotate(${secDeg}deg)`;
    if (minHand) minHand.style.transform = `translateX(-50%) rotate(${minDeg}deg)`;
    if (hrHand) hrHand.style.transform = `translateX(-50%) rotate(${hrDeg}deg)`;
}

async function fetchNTPTime() {
    try {
        const resp = await fetch('/api/time');
        const data = await resp.json();
        const timeEl = document.getElementById('ntpTime');
        const dateEl = document.getElementById('ntpDate');
        if (timeEl) timeEl.textContent = data.time || '--:--:--';
        if (dateEl) {
            dateEl.textContent = `${data.date || ''} · France (CET)`;
        }
    } catch (e) {
        // Fallback: show local time
        const now = new Date();
        const timeEl = document.getElementById('ntpTime');
        const dateEl = document.getElementById('ntpDate');
        if (timeEl) timeEl.textContent = now.toLocaleTimeString('fr-FR');
        if (dateEl) dateEl.textContent = now.toLocaleDateString('fr-FR') + ' · France (Local)';
    }
}

// ---- Data Fetching ----
async function refreshData() {
    try {
        const resp = await fetch('/api/stats');
        const data = await resp.json();
        if (data.redirect) {
            window.location.href = data.redirect;
            return;
        }
        if (data.status === 'no_data') {
            return;
        }
        statsData = data;
        updateDashboard(data);
        updateInterventions(data);
        updateTechnicians(data);
        updateGantt(data);
        updateFailures(data);

    } catch (e) {
        console.error('Refresh error:', e);
    }
}

// ---- Dashboard ----
function updateDashboard(data) {
    // Metrics
    animateValue('metricTotal', data.total || 0);
    animateValue('metricOKRacc', data.racc_ok || 0);
    animateValue('metricOKSav', data.sav_ok || 0);
    animateValue('metricNOKRacc', data.racc_nok || 0);
    animateValue('metricNOKSav', data.sav_nok || 0);

    const rateRacc = ((data.racc_rate || 0) * 100);
    const rateSav = ((data.sav_rate || 0) * 100);
    
    document.getElementById('metricRateRacc').textContent = rateRacc.toFixed(1) + '%';
    document.getElementById('metricRateRacc').style.color = rateRacc >= 80 ? 'var(--green-400)' : rateRacc >= 60 ? 'var(--amber-400)' : 'var(--red-400)';

    document.getElementById('metricRateSav').textContent = rateSav.toFixed(1) + '%';
    document.getElementById('metricRateSav').style.color = rateSav >= 80 ? 'var(--green-400)' : rateSav >= 60 ? 'var(--amber-400)' : 'var(--red-400)';

    document.getElementById('metricPDC').textContent = data.pdc || 0;
    document.getElementById('metricInProgress').textContent = data.in_progress || 0;


    // NOK badge
    document.getElementById('nokBadge').textContent = data.total_nok || 0;

    // Source file
    const fileTagName = document.getElementById('fileTagName');
    if (data.source_file) {
        const name = data.source_file.split(/[\/\\]/).pop();
        let displayName = name;
        const match = name.match(/^(\d{4})-(\d{2})-(\d{2})\.xlsx$/);
        if (match) {
            const parts = ['janvier', 'février', 'mars', 'avril', 'mai', 'juin', 'juillet', 'août', 'septembre', 'octobre', 'novembre', 'décembre'];
            displayName = `${match[3]} ${parts[parseInt(match[2])-1]} ${match[1]}`;
        }
        fileTagName.textContent = displayName;
    }

    // Donut chart
    drawDonut(data);

    // Tech bars
    updateTechBars(data);

    // Breakdown
    updateBreakdown(data);

    // NOK table on dashboard
    updateNOKTable(data);
}

// ---- Donut Chart (Animated) ----
function drawDonut(data) {
    const canvas = document.getElementById('donutChart');
    const ctx = canvas.getContext('2d');
    const dpr = window.devicePixelRatio || 1;
    canvas.width = 220 * dpr;
    canvas.height = 220 * dpr;
    ctx.scale(dpr, dpr);

    let ok, nok, label;
    if (chartMode === 'racc') {
        ok = data.racc_ok || 0;
        nok = data.racc_nok || 0;
        label = 'RACC';
    } else {
        ok = data.sav_ok || 0;
        nok = data.sav_nok || 0;
        label = 'SAV';
    }

    const total = ok + nok || 1;
    const rate = ok / total;
    const cx = 110, cy = 110, r = 80, lineWidth = 14;
    const duration = 800;
    const start = performance.now();

    // Cancel any previous animation
    if (window._donutAnimId) cancelAnimationFrame(window._donutAnimId);

    function easeOut(t) { return 1 - Math.pow(1 - t, 3); }

    function frame(now) {
        const elapsed = now - start;
        const progress = Math.min(elapsed / duration, 1);
        const easedProgress = easeOut(progress);

        ctx.clearRect(0, 0, 220, 220);

        // Background ring
        ctx.beginPath();
        ctx.arc(cx, cy, r, 0, Math.PI * 2);
        ctx.strokeStyle = document.documentElement.getAttribute('data-theme') === 'light' ? 'rgba(0,0,0,0.08)' : 'rgba(255,255,255,0.06)';
        ctx.lineWidth = lineWidth;
        ctx.lineCap = 'butt';
        ctx.stroke();

        const currentAngle = Math.PI * 2 * easedProgress;

        // OK arc
        if (ok > 0) {
            const okEnd = Math.min(currentAngle, Math.PI * 2 * rate);
            if (okEnd > 0.01) {
                ctx.beginPath();
                ctx.arc(cx, cy, r, -Math.PI / 2, -Math.PI / 2 + okEnd);
                ctx.strokeStyle = '#22c55e';
                ctx.lineWidth = lineWidth;
                ctx.lineCap = 'round';
                ctx.stroke();
            }
        }

        // NOK arc
        if (nok > 0) {
            const nokStart = Math.PI * 2 * rate;
            if (currentAngle > nokStart) {
                ctx.beginPath();
                ctx.arc(cx, cy, r, -Math.PI / 2 + nokStart, -Math.PI / 2 + currentAngle);
                ctx.strokeStyle = '#ef4444';
                ctx.lineWidth = lineWidth;
                ctx.lineCap = 'round';
                ctx.stroke();
            }
        }

        // Center text (animated count)
        const displayRate = (rate * 100 * easedProgress).toFixed(1);
        ctx.fillStyle = getComputedStyle(document.documentElement).getPropertyValue('--text-primary').trim() || '#f0f0f5';
        ctx.font = 'bold 28px Inter, sans-serif';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillText(displayRate + '%', cx, cy - 6);

        ctx.fillStyle = getComputedStyle(document.documentElement).getPropertyValue('--text-secondary').trim() || '#8b8ba0';
        ctx.font = '12px Inter, sans-serif';
        ctx.fillText(label, cx, cy + 18);

        if (progress < 1) {
            window._donutAnimId = requestAnimationFrame(frame);
        }
    }

    window._donutAnimId = requestAnimationFrame(frame);

    // Legend (immediate)
    const legend = document.getElementById('chartLegend');
    legend.innerHTML = `
        <div class="legend-item"><span class="legend-dot" style="background:#22c55e"></span><span>OK</span><span class="legend-value">${ok}</span></div>
        <div class="legend-item"><span class="legend-dot" style="background:#ef4444"></span><span>NOK</span><span class="legend-value">${nok}</span></div>
        <div class="legend-item" style="margin-top:4px;border-top:1px solid var(--border);padding-top:8px"><span class="legend-dot" style="background:#60a5fa"></span><span>Total</span><span class="legend-value">${total}</span></div>
    `;
}

// ---- Chart Tabs ----
function initChartTabs() {
    document.querySelectorAll('[data-chart]').forEach(tab => {
        tab.addEventListener('click', () => {
            document.querySelectorAll('[data-chart]').forEach(t => t.classList.remove('active'));
            tab.classList.add('active');
            chartMode = tab.dataset.chart;
            if (statsData) drawDonut(statsData);
        });
    });
}

// ---- Breakdown Tabs ----
function initBreakdownTabs() {
    document.querySelectorAll('[data-breakdown]').forEach(tab => {
        tab.addEventListener('click', () => {
            document.querySelectorAll('[data-breakdown]').forEach(t => t.classList.remove('active'));
            tab.classList.add('active');
            breakdownMode = tab.dataset.breakdown;
            if (statsData) updateBreakdown(statsData);
        });
    });
}

// ---- Tech Performance Bars ----
function updateTechBars(data) {
    const container = document.getElementById('techBars');
    const techs = (data.by_technician || []).slice().sort((a, b) => b.total - a.total).slice(0, 12);

    if (!techs.length) {
        container.innerHTML = '<div class="empty-state small">Aucune donnée</div>';
        return;
    }

    const maxTotal = Math.max(...techs.map(t => t.total), 1);

    container.innerHTML = techs.map((t, i) => {
        const okW = (t.ok / maxTotal * 100).toFixed(1);
        const nokW = (t.nok / maxTotal * 100).toFixed(1);
        const rateColor = t.rate_ok >= 0.8 ? 'var(--green-400)' : t.rate_ok >= 0.6 ? 'var(--amber-400)' : 'var(--red-400)';
        const delay = (i * 0.04).toFixed(2);
        return `<div class="tech-bar-row" style="animation:fadeInUp 0.4s ${delay}s both">
            <span class="tech-bar-name" title="${t.name}">${t.name.split(' ').pop()}</span>
            <div class="tech-bar-track">
                <div class="tech-bar-ok" data-width="${okW}" style="width:0%;transition:width 0.6s ${delay}s cubic-bezier(0.16,1,0.3,1)"></div>
                <div class="tech-bar-nok" data-width="${nokW}" style="width:0%;transition:width 0.6s ${(parseFloat(delay) + 0.1).toFixed(2)}s cubic-bezier(0.16,1,0.3,1)"></div>
            </div>
            <div class="tech-bar-stats">
                <span class="tech-bar-total">${t.total} <span style="font-weight:500; font-size:0.65rem; color:var(--text-tertiary)">int.</span></span>
                <span class="tech-bar-rate" style="color:${rateColor}">${(t.rate_ok * 100).toFixed(0)}% OK</span>
            </div>
        </div>`;
    }).join('');

    // Animate bars in
    requestAnimationFrame(() => {
        container.querySelectorAll('.tech-bar-ok, .tech-bar-nok').forEach(bar => {
            bar.style.width = bar.dataset.width + '%';
        });
    });
}

// ---- Breakdown ----
function updateBreakdown(data) {
    const container = document.getElementById('breakdownList');
    const raw = breakdownMode === 'dept' ? (data.by_department || {}) : (data.by_zone || {});
    const entries = Object.entries(raw).sort((a, b) => b[1] - a[1]);

    if (!entries.length) {
        container.innerHTML = '<div class="empty-state small">Aucune donnée</div>';
        return;
    }

    const maxVal = Math.max(...entries.map(e => e[1]), 1);
    const colors = ['#60a5fa', '#a78bfa', '#22d3ee', '#34d399', '#fbbf24', '#f87171'];

    container.innerHTML = entries.map(([label, count], i) => {
        const w = (count / maxVal * 100).toFixed(1);
        const color = colors[i % colors.length];
        const delay = (i * 0.05).toFixed(2);
        return `<div class="breakdown-item" style="animation:fadeInUp 0.4s ${delay}s both">
            <span class="breakdown-label">${label}</span>
            <div class="breakdown-bar"><div class="breakdown-bar-fill" data-width="${w}" style="width:0%;background:${color};transition:width 0.5s ${delay}s cubic-bezier(0.16,1,0.3,1)"></div></div>
            <span class="breakdown-value">${count}</span>
        </div>`;
    }).join('');

    // Animate bars in
    requestAnimationFrame(() => {
        container.querySelectorAll('.breakdown-bar-fill').forEach(bar => {
            bar.style.width = bar.dataset.width + '%';
        });
    });
}

window.nokSortState = { field: null, asc: true };

function updateNOKTable(data) {
    const card = document.getElementById('nokAlertCard');
    const body = document.getElementById('nokTableBody');

    // Dashboard preview: fix to the latest 15 records, then sort within them
    let noks = [...(data.nok_records || [])].slice(0, 15);
    
    // Sort logic (within the fixed 15)
    if (window.nokSortState.field) {
        noks.sort((a, b) => {
            const field = window.nokSortState.field;
            let valA = a[field] || '';
            let valB = b[field] || '';
            
            // Special cases
            if (field === 'dept') {
                valA = a.department || '';
                valB = b.department || '';
            }
            if (field === 'reference') {
                valA = a.reference || '';
                valB = b.reference || '';
            }
            if (field === 'tech') {
                valA = a.tech || '';
                valB = b.tech || '';
            }
            
            if (typeof valA === 'string') valA = valA.toLowerCase();
            if (typeof valB === 'string') valB = valB.toLowerCase();
            
            if (valA < valB) return window.nokSortState.asc ? -1 : 1;
            if (valA > valB) return window.nokSortState.asc ? 1 : -1;
            return 0;
        });
    }

    if (!noks.length) {
        card.style.display = 'none';
        return;
    }

    card.style.display = 'block';

    body.innerHTML = noks.map(r => `<tr>
        <td style="color:var(--text-primary);font-weight:500">${esc(r.tech)}</td>
        <td><span class="state-badge nok">${esc(r.type)}</span></td>
        <td style="font-family:monospace;font-size:0.78rem">${esc(r.reference)}</td>
        <td>${esc(r.department)}</td>
        <td>${esc(r.duration)}</td>
        <td style="max-width:200px;overflow:hidden;text-overflow:ellipsis" title="${esc(r.reason)}">${esc(r.reason)}</td>
        <td>${esc(r.category || '—')}</td>
    </tr>`).join('');
}

function sortNOKTable(field) {
    if (window.nokSortState.field === field) {
        window.nokSortState.asc = !window.nokSortState.asc;
    } else {
        window.nokSortState.field = field;
        window.nokSortState.asc = true;
    }
    
    // Update headers UI
    document.querySelectorAll('#nokTable th.sortable').forEach(th => {
        th.classList.remove('active', 'asc', 'desc');
        if (th.dataset.field === field) {
            th.classList.add('active', window.nokSortState.asc ? 'asc' : 'desc');
        }
    });

    // Refresh table immediately with existing data
    if (statsData) {
        updateNOKTable(statsData);
    }
}

window.interventionsSortState = { field: null, asc: true };
window.interventionsCurrentPage = 1;
const INTERVENTIONS_PER_PAGE = 25;

function updateInterventions(data) {
    const records = data.all_records || [];
    renderInterventionTable(records);
}

function renderInterventionTable(records) {
    const body = document.getElementById('interventionsBody');
    const searchVal = (document.getElementById('interventionSearch')?.value || '').toLowerCase();
    const stateVal = document.getElementById('stateFilter')?.value || '';
    const typeVal = document.getElementById('typeFilter')?.value || '';

    let filtered = [...records];
    
    // 1. Filter
    if (searchVal) {
        filtered = filtered.filter(r =>
            (r.tech || '').toLowerCase().includes(searchVal) ||
            (r.reference || '').toLowerCase().includes(searchVal) ||
            (r.pm || '').toLowerCase().includes(searchVal)
        );
    }
    if (stateVal) {
        filtered = filtered.filter(r => (r.state || '').toUpperCase() === stateVal);
    }
    if (typeVal) {
        filtered = filtered.filter(r => (r.type || '').toUpperCase() === typeVal);
    }

    // 2. Sort
    const { field, asc } = window.interventionsSortState;
    if (field) {
        filtered.sort((a, b) => {
            let valA = a[field] || '';
            let valB = b[field] || '';
            
            const prepare = v => {
                if (typeof v !== 'string') return v;
                if (v.match(/^\d{2}\/\d{2}\/\d{4} \d{2}:\d{2}$/)) {
                    const parts = v.split(/[\/ :]/);
                    return `${parts[2]}-${parts[1]}-${parts[0]} ${parts[3]}:${parts[4]}`;
                }
                return v;
            };

            const pA = prepare(valA);
            const pB = prepare(valB);

            if (typeof pA === 'string' && typeof pB === 'string') {
                return asc ? pA.localeCompare(pB) : pB.localeCompare(pA);
            }
            if (pA < pB) return asc ? -1 : 1;
            if (pA > pB) return asc ? 1 : -1;
            return 0;
        });
    }

    document.querySelectorAll('th .sort-icon[id^="sort-icon-int-"]').forEach(el => el.innerHTML = '');
    if (field) {
        const iconEl = document.getElementById(`sort-icon-int-${field}`);
        if (iconEl) iconEl.innerHTML = asc ? ' ↑' : ' ↓';
    }

    // 3. Paginate
    document.getElementById('interventionCount').textContent = `${filtered.length} intervention${filtered.length !== 1 ? 's' : ''}`;
    
    const totalPages = Math.ceil(filtered.length / INTERVENTIONS_PER_PAGE);
    if (window.interventionsCurrentPage > totalPages && totalPages > 0) {
        window.interventionsCurrentPage = totalPages;
    }
    if (totalPages === 0) window.interventionsCurrentPage = 1;

    const startIdx = (window.interventionsCurrentPage - 1) * INTERVENTIONS_PER_PAGE;
    const paginated = filtered.slice(startIdx, startIdx + INTERVENTIONS_PER_PAGE);

    body.innerHTML = paginated.map(r => {
        const stateClass = (r.state || '').toUpperCase() === 'OK' ? 'ok' : (r.state || '').toUpperCase() === 'NOK' ? 'nok' : 'en-cours';
        return `<tr>
            <td style="font-family:monospace;font-size:0.78rem">${esc(r.reference)}</td>
            <td style="color:var(--text-primary);font-weight:500">${esc(r.tech)}</td>
            <td>${esc(r.type)}</td>
            <td><span class="state-badge ${stateClass}">${esc(r.state)}</span></td>
            <td>${esc(r.start_time)}</td>
            <td>${esc(r.end_time)}</td>
            <td>${esc(r.duration)}</td>
            <td>${esc(r.department)}</td>
            <td>${esc(r.zone)}</td>
            <td>${esc(r.delay_type || '—')}</td>
            <td style="max-width:180px;overflow:hidden;text-overflow:ellipsis" title="${esc(r.fail_code)}">${esc(r.fail_code || '—')}</td>
        </tr>`;
    }).join('');

    renderInterventionsPagination(totalPages);
}

function renderInterventionsPagination(totalPages) {
    const container = document.getElementById('interventionsPagination');
    if (totalPages <= 1) {
        container.innerHTML = '';
        return;
    }
    
    let current = window.interventionsCurrentPage;
    let html = `<span class="page-info">Page ${current} / ${totalPages}</span>`;
    
    html += `<button class="page-btn" ${current === 1 ? 'disabled' : ''} onclick="setInterventionsPage(${current - 1})">⟨</button>`;
    
    for (let i = 1; i <= totalPages; i++) {
        if (i === 1 || i === totalPages || (i >= current - 2 && i <= current + 2)) {
            html += `<button class="page-btn ${current === i ? 'active' : ''}" onclick="setInterventionsPage(${i})">${i}</button>`;
        } else if (i === current - 3 || i === current + 3) {
            html += `<span class="page-ellipsis">…</span>`;
        }
    }
    
    html += `<button class="page-btn" ${current === totalPages ? 'disabled' : ''} onclick="setInterventionsPage(${current + 1})">⟩</button>`;
    
    container.innerHTML = html;
}

window.setInterventionsPage = function(page) {
    window.interventionsCurrentPage = page;
    if (statsData) renderInterventionTable(statsData.all_records || []);
};

window.sortInterventions = function(field) {
    if (window.interventionsSortState.field === field) {
        window.interventionsSortState.asc = !window.interventionsSortState.asc;
    } else {
        window.interventionsSortState.field = field;
        window.interventionsSortState.asc = true;
    }
    window.interventionsCurrentPage = 1;
    if (statsData) renderInterventionTable(statsData.all_records || []);
};

function initFilters() {
    ['interventionSearch', 'stateFilter', 'typeFilter'].forEach(id => {
        const el = document.getElementById(id);
        if (el) {
            const handler = () => {
                window.interventionsCurrentPage = 1;
                if (statsData) renderInterventionTable(statsData.all_records || []);
            };
            if (id === 'interventionSearch') {
                el.addEventListener('input', handler);
            } else {
                el.addEventListener('change', handler);
            }
        }
    });

    // Tech sort & search
    const techSort = document.getElementById('techSort');
    if (techSort) {
        techSort.addEventListener('change', () => {
            techPage = 1;
            if (statsData) updateTechnicians(statsData);
        });
    }
    const techSearch = document.getElementById('techSearch');
    if (techSearch) {
        techSearch.addEventListener('input', () => {
            techPage = 1;
            if (statsData) updateTechnicians(statsData);
        });
    }
}

// ---- Pagination Utility ----
const TECH_PER_PAGE = 12;
const TECH_CONFIG_PER_PAGE = 15;
let techPage = 1;
let techConfigPage = 1;

function renderPagination(containerId, currentPage, totalPages, totalItems, onPageChange) {
    const container = document.getElementById(containerId);
    if (!container) return;
    if (totalPages <= 1) { container.innerHTML = ''; return; }

    let html = '';
    // Prev
    html += `<button class="page-btn" ${currentPage <= 1 ? 'disabled' : ''} onclick="${onPageChange}(${currentPage - 1})">&laquo;</button>`;

    // Page numbers with ellipsis
    const maxVisible = 5;
    let startPage = Math.max(1, currentPage - Math.floor(maxVisible / 2));
    let endPage = Math.min(totalPages, startPage + maxVisible - 1);
    if (endPage - startPage < maxVisible - 1) startPage = Math.max(1, endPage - maxVisible + 1);

    if (startPage > 1) {
        html += `<button class="page-btn" onclick="${onPageChange}(1)">1</button>`;
        if (startPage > 2) html += `<span class="page-ellipsis">&hellip;</span>`;
    }
    for (let p = startPage; p <= endPage; p++) {
        html += `<button class="page-btn ${p === currentPage ? 'active' : ''}" onclick="${onPageChange}(${p})">${p}</button>`;
    }
    if (endPage < totalPages) {
        if (endPage < totalPages - 1) html += `<span class="page-ellipsis">&hellip;</span>`;
        html += `<button class="page-btn" onclick="${onPageChange}(${totalPages})">${totalPages}</button>`;
    }

    // Next
    html += `<button class="page-btn" ${currentPage >= totalPages ? 'disabled' : ''} onclick="${onPageChange}(${currentPage + 1})">&raquo;</button>`;

    // Info
    const from = (currentPage - 1) * (containerId.includes('Config') ? TECH_CONFIG_PER_PAGE : TECH_PER_PAGE) + 1;
    const perPage = containerId.includes('Config') ? TECH_CONFIG_PER_PAGE : TECH_PER_PAGE;
    const to = Math.min(currentPage * perPage, totalItems);
    html += `<span class="page-info">${from}–${to} sur ${totalItems}</span>`;

    container.innerHTML = html;
}

function goTechPage(page) {
    techPage = page;
    if (statsData) updateTechnicians(statsData);
}

function goTechConfigPage(page) {
    techConfigPage = page;
    applyTechConfigPagination();
}

// ---- Technicians View ----
function buildTypeSection(label, ok, nok, okColor, nokColor) {
    const total = ok + nok;
    const rate = total > 0 ? (ok / total * 100) : 0;
    const okPct = total > 0 ? (ok / total * 100).toFixed(1) : '0';
    const nokPct = total > 0 ? (nok / total * 100).toFixed(1) : '0';
    const rateBadgeClass = rate >= 80 ? 'rate-good' : rate >= 60 ? 'rate-mid' : 'rate-low';
    const isRacc = label === 'RACC';
    const labelClass = isRacc ? 'type-racc' : 'type-sav';

    return `<div class="tech-type-section">
        <div class="tech-type-header">
            <span class="tech-type-badge ${labelClass}">${label}</span>
            <span class="tech-type-rate ${rateBadgeClass}">${total > 0 ? rate.toFixed(0) + '%' : '—'}</span>
        </div>
        <div class="tech-type-counts">
            <span class="tech-type-ok">✓ ${ok}</span>
            <span class="tech-type-nok">✗ ${nok}</span>
            <span class="tech-type-total">${total}</span>
        </div>
        <div class="tech-type-bar">
            <div class="tech-type-bar-ok" style="width:${okPct}%;background:${okColor}"></div>
            <div class="tech-type-bar-nok" style="width:${nokPct}%;background:${nokColor}"></div>
        </div>
    </div>`;
}

function updateTechnicians(data) {
    const container = document.getElementById('techGrid');
    let techs = (data.by_technician || []).slice();
    const sortVal = document.getElementById('techSort')?.value || 'racc_nok';
    const searchVal = (document.getElementById('techSearch')?.value || '').toLowerCase().trim();

    // Filter by search query
    if (searchVal) {
        techs = techs.filter(t => (t.name || '').toLowerCase().includes(searchVal) || (t.sector || '').toLowerCase().includes(searchVal));
    }

    switch (sortVal) {
        case 'racc_nok': techs.sort((a, b) => (b.racc_nok || 0) - (a.racc_nok || 0)); break;
        case 'sav_nok':  techs.sort((a, b) => (b.sav_nok || 0) - (a.sav_nok || 0)); break;
        case 'racc_ok':  techs.sort((a, b) => (b.racc_ok || 0) - (a.racc_ok || 0)); break;
        case 'sav_ok':   techs.sort((a, b) => (b.sav_ok || 0) - (a.sav_ok || 0)); break;
        case 'name': techs.sort((a, b) => a.name.localeCompare(b.name)); break;
    }

    if (!techs.length) {
        container.innerHTML = '<div class="empty-state">Aucune donnée technicien</div>';
        document.getElementById('techPagination').innerHTML = '';
        return;
    }

    // Pagination
    const totalPages = Math.ceil(techs.length / TECH_PER_PAGE);
    if (techPage > totalPages) techPage = totalPages;
    const startIdx = (techPage - 1) * TECH_PER_PAGE;
    const pageTechs = techs.slice(startIdx, startIdx + TECH_PER_PAGE);

    const avatarColors = [
        ['#1d4ed8', '#dbeafe'], ['#7c3aed', '#ede9fe'], ['#059669', '#d1fae5'],
        ['#dc2626', '#fee2e2'], ['#d97706', '#fef3c7'], ['#0891b2', '#cffafe'],
        ['#c026d3', '#fae8ff'], ['#4338ca', '#e0e7ff']
    ];

    container.innerHTML = pageTechs.map((t, i) => {
        const globalIdx = startIdx + i;
        const initials = t.name.split(' ').map(n => n[0]).join('').toUpperCase().slice(0, 2);
        const [bg, fg] = avatarColors[globalIdx % avatarColors.length];

        return `<div class="tech-card">
            <div class="tech-card-header">
                <div class="tech-avatar" style="background:${bg};color:${fg}">${initials}</div>
                <div class="tech-card-name">${esc(t.name)}</div>
            </div>
            <div class="tech-card-type-sections">
                ${buildTypeSection('RACC', t.racc_ok || 0, t.racc_nok || 0, 'var(--green-400)', 'var(--red-400)')}
                ${buildTypeSection('SAV', t.sav_ok || 0, t.sav_nok || 0, 'var(--blue-400)', 'var(--red-400)')}
            </div>
        </div>`;
    }).join('');

    renderPagination('techPagination', techPage, totalPages, techs.length, 'goTechPage');
}

// ---- GANTT View ----
function updateGantt(data) {
    const ganttData = data.gantt_data || [];
    const headerRow = document.getElementById('ganttHeader');
    const body = document.getElementById('ganttBody');

    if (!ganttData.length) {
        headerRow.innerHTML = '<th>Technicien</th>';
        body.innerHTML = '<tr><td colspan="12" class="empty-state small">Aucune donnée GANTT</td></tr>';
        return;
    }

    // Time slots headers
    const timeSlots = ['08h', '09h', '10h', '11h', '12h', '13h', '14h', '15h', '16h', '17h'];
    headerRow.innerHTML = '<th>Technicien</th><th>Sect.</th>' +
        timeSlots.map(t => `<th>${t}</th>`).join('');

    body.innerHTML = ganttData.map(g => {
        const slots = (g.slots || []);
        const slotCells = [];
        for (let i = 0; i < 10; i++) {
            const val = (slots[i] || '').trim();
            let cls = 'empty';
            let display = '—';
            const upper = val.toUpperCase();
            if (upper.includes('OK RACC')) { cls = 'ok-racc'; display = 'OK RACC'; }
            else if (upper.includes('NOK RACC')) { cls = 'nok-racc'; display = 'NOK RACC'; }
            else if (upper.includes('OK SAV')) { cls = 'ok-sav'; display = 'OK SAV'; }
            else if (upper.includes('NOK SAV')) { cls = 'nok-sav'; display = 'NOK SAV'; }
            else if (upper.includes('EN COURS')) { cls = 'en-cours'; display = 'En cours'; }
            else if (val) { display = val; }
            slotCells.push(`<td><span class="gantt-cell ${cls}">${display}</span></td>`);
        }

        return `<tr>
            <td style="color:var(--text-primary);font-weight:500">${esc(g.tech)}</td>
            <td>${esc(g.sector)}</td>
            ${slotCells.join('')}
        </tr>`;
    }).join('');
}

// ---- Failures View ----
function updateFailures(data) {
    // By category
    const catContainer = document.getElementById('failureByCat');
    const cats = Object.entries(data.failures_by_category || {}).sort((a, b) => b[1] - a[1]);
    const maxCat = Math.max(...cats.map(c => c[1]), 1);

    catContainer.innerHTML = cats.length ? cats.map(([label, count]) => `
        <div class="failure-bar-row">
            <div class="failure-bar-label"><span>${esc(label)}</span><span>${count}</span></div>
            <div class="failure-bar-track"><div class="failure-bar-fill" style="width:${(count / maxCat * 100).toFixed(1)}%"></div></div>
        </div>
    `).join('') : '<div class="empty-state small">Aucune donnée</div>';

    // By type
    const typeContainer = document.getElementById('failureByType');
    const types = Object.entries(data.failures_by_type || {}).sort((a, b) => b[1] - a[1]);
    const maxType = Math.max(...types.map(t => t[1]), 1);

    typeContainer.innerHTML = types.length ? types.map(([label, count]) => `
        <div class="failure-bar-row">
            <div class="failure-bar-label"><span>${esc(label)}</span><span>${count}</span></div>
            <div class="failure-bar-track"><div class="failure-bar-fill" style="width:${(count / maxType * 100).toFixed(1)}%"></div></div>
        </div>
    `).join('') : '<div class="empty-state small">Aucune donnée</div>';

    // Init sorting state if necessary 
    if (!window.failureSortState) {
        window.failureSortState = { field: null, asc: true };
    }
    window.allNokRecords = data.nok_records || [];
    renderFailureTable();
}

function renderFailureTable() {
    const failBody = document.getElementById('failureBody');
    let noks = [...(window.allNokRecords || [])];
    const { field, asc } = window.failureSortState;

    if (field) {
        noks.sort((a, b) => {
            let valA = a[field] || '';
            let valB = b[field] || '';
            
            // Format converter helper for sorting purposes
            const prepare = v => {
                if (typeof v !== 'string') return v;
                // Handle DD/MM/YYYY HH:MM -> YYYY-MM-DD HH:MM
                if (v.match(/^\d{2}\/\d{2}\/\d{4} \d{2}:\d{2}$/)) {
                    const parts = v.split(/[\/ :]/);
                    return `${parts[2]}-${parts[1]}-${parts[0]} ${parts[3]}:${parts[4]}`;
                }
                return v;
            };

            const pA = prepare(valA);
            const pB = prepare(valB);

            if (typeof pA === 'string' && typeof pB === 'string') {
                return asc ? pA.localeCompare(pB) : pB.localeCompare(pA);
            }
            if (pA < pB) return asc ? -1 : 1;
            if (pA > pB) return asc ? 1 : -1;
            return 0;
        });
    }

    // Update icons
    document.querySelectorAll('.sort-icon').forEach(el => el.innerHTML = '');
    if (field) {
        const iconEl = document.getElementById(`sort-icon-${field}`);
        if (iconEl) iconEl.innerHTML = asc ? ' ↑' : ' ↓';
    }

    failBody.innerHTML = noks.map(r => `<tr>
        <td style="color:var(--text-primary);font-weight:500">${esc(r.tech)}</td>
        <td><span class="state-badge nok">${esc(r.type)}</span></td>
        <td style="font-family:monospace;font-size:0.78rem">${esc(r.reference)}</td>
        <td style="font-size:0.78rem">${esc(r.pm)}</td>
        <td>${esc(r.department)}</td>
        <td>${esc(r.start_time)}</td>
        <td>${esc(r.end_time)}</td>
        <td>${esc(r.duration)}</td>
        <td style="max-width:220px;overflow:hidden;text-overflow:ellipsis" title="${esc(r.reason)}">${esc(r.reason)}</td>
        <td>${esc(r.category || '—')}</td>
    </tr>`).join('');
}

window.sortFailures = function(field) {
    if (!window.failureSortState) window.failureSortState = { field: null, asc: true };
    
    if (window.failureSortState.field === field) {
        window.failureSortState.asc = !window.failureSortState.asc;
    } else {
        window.failureSortState.field = field;
        window.failureSortState.asc = true;
    }
    renderFailureTable();
}

// ---- Notifications ----
async function loadNotifications() {
    try {
        const resp = await fetch('/api/notifications');
        notifications = await resp.json();
        renderNotifications();
    } catch (e) { console.error(e); }
}

function renderNotifications() {
    const container = document.getElementById('notifList');

    if (!notifications || !notifications.length) {
        container.innerHTML = '<div class="empty-state">Aucune notification envoyée</div>';
        return;
    }

    // Count stats
    const successCount = notifications.filter(n => n.success).length;
    const errorCount = notifications.filter(n => !n.success && n.type !== 'warning').length;
    const warningCount = notifications.filter(n => n.type === 'warning').length;

    // Type labels
    const typeLabels = {
        stats: 'Statistiques',
        morning: 'Bonjour',
        eod_thanks: 'Remerciement',
        eod_report: 'Rapport fin de journée',
        nok_alert: 'Alerte NOK',
        warning: 'Avertissement'
    };

    // Icon map
    const typeIcons = {
        stats: '📊',
        morning: '🌅',
        eod_thanks: '🙏',
        eod_report: '📋',
        nok_alert: '🚨',
        warning: '⚠️'
    };

    // Summary bar
    let summaryHtml = `<div class="notif-summary">
        <span>${notifications.length} notification${notifications.length > 1 ? 's' : ''}</span>
        <span class="notif-summary-stat success">✓ ${successCount} envoyé${successCount > 1 ? 's' : ''}</span>
        ${errorCount > 0 ? `<span class="notif-summary-stat error">✗ ${errorCount} échec${errorCount > 1 ? 's' : ''}</span>` : ''}
        ${warningCount > 0 ? `<span class="notif-summary-stat warning">⚠ ${warningCount} avert.</span>` : ''}
    </div>`;

    let itemsHtml = notifications.map(n => {
        const isWarning = n.type === 'warning';
        const isError = !n.success && !isWarning;
        const isSuccess = n.success;

        // Determine CSS class
        let itemClass = 'notif-success';
        if (isError) itemClass = 'notif-error';
        if (isWarning) itemClass = 'notif-warning';

        // Icon
        let iconClass = n.type;
        let icon = typeIcons[n.type] || '📨';
        if (isError) { icon = '❌'; iconClass = 'notif-icon-error'; }

        // Status badge
        let badge = '<span class="notif-status-badge success">Envoyé</span>';
        if (isError) badge = '<span class="notif-status-badge error">Échec</span>';
        if (isWarning) badge = '<span class="notif-status-badge warning">Attention</span>';

        // Label
        const label = typeLabels[n.type] || n.type;

        // Time
        const ts = new Date(n.timestamp).toLocaleString('fr-FR');

        // Body content
        let bodyContent = '';
        if (isWarning) {
            bodyContent = `<div class="notif-warning-msg">${esc(n.message)}</div>`;
        } else if (isError) {
            bodyContent = `<div class="notif-error-msg">${esc(n.message)}</div>`;
        } else {
            bodyContent = `<div class="notif-msg">${esc(n.message)}</div>`;
        }

        return `<div class="notif-item ${itemClass}">
            <div class="notif-icon ${esc(iconClass)}">${icon}</div>
            <div class="notif-body">
                <div class="notif-type">
                    ${esc(label)} → ${esc(n.recipient)}
                    ${badge}
                </div>
                ${bodyContent}
            </div>
            <div class="notif-time">${ts}</div>
            <button class="notif-delete-btn" onclick="clearNotification(${n.id})" title="Supprimer">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
        </div>`;
    }).join('');

    container.innerHTML = summaryHtml + itemsHtml;
}

async function clearNotification(id) {
    try {
        await fetch('/api/notifications?id=' + id, { method: 'DELETE' });
        loadNotifications();
    } catch(e) { console.error(e); }
}

async function clearAllNotifications() {
    if(!await showConfirm('Tout supprimer', 'Voulez-vous vraiment effacer tout l\'historique des notifications ?')) return;
    try {
        await fetch('/api/notifications', { method: 'DELETE' });
        loadNotifications();
    } catch(e) { console.error(e); }
}


// ---- Settings ----
let currentConfig = null;

async function loadSettings() {
    try {
        const resp = await fetch('/api/config', { cache: 'no-store' });
        currentConfig = await resp.json();

        document.getElementById('settingsMyNumber').value = currentConfig.MY_NUMBER || '';
        // Time pickers: convert hour+minute to HH:MM format
        const mh = String(currentConfig.MORNING_HOUR || 8).padStart(2, '0');
        const mm = String(currentConfig.MORNING_MINUTE || 0).padStart(2, '0');
        document.getElementById('settingsMorningTime').value = mh + ':' + mm;
        const ih = String(currentConfig.STATS_INTERVAL_HOURS || 2).padStart(2, '0');
        const im = String(currentConfig.STATS_INTERVAL_MINUTES || 0).padStart(2, '0');
        document.getElementById('settingsInterval').value = ih + ':' + im;
        const eh = String(currentConfig.EOD_HOUR || 17).padStart(2, '0');
        const em = String(currentConfig.EOD_MINUTE || 0).padStart(2, '0');
        document.getElementById('settingsEODTime').value = eh + ':' + em;
        document.getElementById('settingsWhatsApp').checked = currentConfig.WHATSAPP_ENABLED || false;

        // Message templates
        document.getElementById('settingsMsgTest').value = currentConfig.MSG_TEST || '';
        document.getElementById('settingsMsgMorning').value = currentConfig.MSG_MORNING || '';
        document.getElementById('settingsMsgEODThanks').value = currentConfig.MSG_EOD_THANKS || '';
        document.getElementById('settingsMsgLateStart').value = currentConfig.MSG_LATE_START || '';

        // SMTP
        document.getElementById('settingsSMTPHost').value = currentConfig.SMTP_HOST || '';
        document.getElementById('settingsSMTPPort').value = currentConfig.SMTP_PORT || 587;
        document.getElementById('settingsSMTPUsername').value = currentConfig.SMTP_USERNAME || '';
        document.getElementById('settingsSMTPPassword').value = currentConfig.SMTP_PASSWORD || '';
        document.getElementById('settingsSMTPFrom').value = currentConfig.SMTP_FROM || '';

        // Google Drive
        loadDriveStatus();

        // Load SMTP config from Zitadel (overrides local values)
        loadZitadelSMTPStatus();

        // Build editable tech table
        const body = document.getElementById('techConfigBody');
        const techs = currentConfig.TECHNICIENS || {};
        const entries = Object.entries(techs);

        if (!entries.length) {
            body.innerHTML = '<tr><td colspan="4" class="empty-state small">Aucun technicien — cliquez "Ajouter"</td></tr>';
        } else {
            body.innerHTML = entries.map(([name, num], i) => {
                // Try to split the name into Nom and Prenom based on the first space
                const parts = name.trim().split(' ');
                const nom = parts[0] || '';
                const prenom = parts.length > 1 ? parts.slice(1).join(' ') : '';
                return `
                <tr data-idx="${i}">
                    <td><input type="text" class="inline-input tech-nom-input" value="${esc(nom)}" placeholder="Nom" disabled style="opacity:0.7;cursor:not-allowed"></td>
                    <td><input type="text" class="inline-input tech-prenom-input" value="${esc(prenom)}" placeholder="Prénom" disabled style="opacity:0.7;cursor:not-allowed"></td>
                    <td><input type="tel" class="inline-input tech-num-input" value="${esc(num)}" placeholder="Numéro WhatsApp" disabled style="opacity:0.7;cursor:not-allowed"></td>
                    <td style="text-align:center; display:flex; justify-content:center; gap:8px;">
                        <button class="btn-icon btn-edit-tech" onclick="editTechRow(this)" title="Éditer" style="background:none;border:none;color:var(--text-secondary);cursor:pointer;padding:4px;">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
                                <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
                            </svg>
                        </button>
                        <button class="btn-icon btn-save-tech" onclick="saveTechRow(this)" title="Sauvegarder" style="display:none;background:none;border:none;color:var(--green-400);cursor:pointer;padding:4px;">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <polyline points="20 6 9 17 4 12"/>
                            </svg>
                        </button>
                        <button class="btn-icon btn-cancel-tech" onclick="cancelTechRow(this)" title="Annuler" style="display:none;background:none;border:none;color:var(--red-400);cursor:pointer;padding:4px;">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <line x1="18" y1="6" x2="6" y2="18"></line>
                                <line x1="6" y1="6" x2="18" y2="18"></line>
                            </svg>
                        </button>
                        <button class="btn-delete" onclick="removeTechRow(this)" title="Supprimer" style="cursor:pointer;padding:4px;">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <polyline points="3 6 5 6 21 6"/>
                                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                            </svg>
                        </button>
                    </td>
                </tr>
            `}).join('');
        }
        applyTechConfigPagination();

        // Check WhatsApp connection status
        checkWAStatus();

        if (typeof snapshotAllValues === 'function') {
            snapshotAllValues();
            initChangeTracking();
        }

        if (typeof initFluxTimePickers === 'function') {
            initFluxTimePickers();
            syncFluxTimePickers();
        }
    } catch (e) {
        console.error('Load settings error:', e);
        showToast('Erreur lors du chargement des paramètres', 'error');
    }
}

async function saveSettings(quiet = false, btn = null) {
    const targetBtn = btn || document.getElementById('saveSettingsBtn');
    let originalText = '';
    if (targetBtn) {
        originalText = targetBtn.innerHTML;
        targetBtn.disabled = true;
        targetBtn.innerHTML = '<span class="spinner"></span> Sauvegarde...';
    }

    try {
        // Gather technicians from the table
        const techs = {};
        const rows = document.querySelectorAll('#techConfigBody tr[data-idx]');
        rows.forEach(row => {
            const nom = row.querySelector('.tech-nom-input')?.value.trim() || '';
            const prenom = row.querySelector('.tech-prenom-input')?.value.trim() || '';
            const name = (nom + ' ' + prenom).trim();
            const num = row.querySelector('.tech-num-input')?.value.trim() || '';
            if (name) {
                techs[name] = num;
            }
        });

        const cfg = {
            TECHNICIENS: techs,
            MY_NUMBER: document.getElementById('settingsMyNumber').value.trim(),
            WATCH_FOLDER: currentConfig?.WATCH_FOLDER || '/home/hus/Downloads',
            EXCEL_PATTERN: currentConfig?.EXCEL_PATTERN || '*.xlsx',
            MORNING_HOUR: (() => { const t = document.getElementById('settingsMorningTime').value.split(':'); return parseInt(t[0]) || 8; })(),
            MORNING_MINUTE: (() => { const t = document.getElementById('settingsMorningTime').value.split(':'); return parseInt(t[1]) || 0; })(),
            STATS_INTERVAL_HOURS: (() => { const t = document.getElementById('settingsInterval').value.split(':'); return parseInt(t[0]) || 2; })(),
            STATS_INTERVAL_MINUTES: (() => { const t = document.getElementById('settingsInterval').value.split(':'); return parseInt(t[1]) || 0; })(),
            EOD_HOUR: (() => { const t = document.getElementById('settingsEODTime').value.split(':'); return parseInt(t[0]) || 17; })(),
            EOD_MINUTE: (() => { const t = document.getElementById('settingsEODTime').value.split(':'); return parseInt(t[1]) || 0; })(),
            FRANCE_TZ: currentConfig?.FRANCE_TZ || 'Europe/Paris',
            NTP_SERVER: currentConfig?.NTP_SERVER || 'fr.pool.ntp.org',
            WEB_PORT: currentConfig?.WEB_PORT || 9510,
            WHATSAPP_ENABLED: document.getElementById('settingsWhatsApp').checked,
            MSG_TEST: document.getElementById('settingsMsgTest').value.trim(),
            MSG_MORNING: document.getElementById('settingsMsgMorning').value.trim(),
            MSG_EOD_THANKS: document.getElementById('settingsMsgEODThanks').value.trim(),
            MSG_LATE_START: document.getElementById('settingsMsgLateStart').value.trim(),
            // SMTP
            SMTP_HOST: document.getElementById('settingsSMTPHost').value.trim(),
            SMTP_PORT: parseInt(document.getElementById('settingsSMTPPort').value) || 587,
            SMTP_USERNAME: document.getElementById('settingsSMTPUsername').value.trim(),
            SMTP_PASSWORD: document.getElementById('settingsSMTPPassword').value.trim(),
            SMTP_FROM: document.getElementById('settingsSMTPFrom').value.trim(),
            // Google Drive (preserved from config)
            GDRIVE_FOLDER_ID: currentConfig?.GDRIVE_FOLDER_ID || '',
            GDRIVE_FOLDER_NAME: currentConfig?.GDRIVE_FOLDER_NAME || '',
            GDRIVE_ENABLED: currentConfig?.GDRIVE_ENABLED || false,
            GDRIVE_SYNC_MINUTES: currentConfig?.GDRIVE_SYNC_MINUTES || 5,
            // Auth (preserve)
            ADMIN_EMAIL: currentConfig?.ADMIN_EMAIL || '',
            ADMIN_PASSWORD: ''
        };

        const resp = await fetch('/api/config', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(cfg)
        });

        const result = await resp.json();
        if (result.status === 'saved') {
            if (!quiet) showToast('✅ Paramètres sauvegardés !', 'success');
            // Reload from server so currentConfig always reflects the true persisted state
            // (includes OIDC / Zitadel fields not managed by this form)
            try {
                const refreshResp = await fetch('/api/config', { cache: 'no-store' });
                currentConfig = await refreshResp.json();
            } catch (_) {
                currentConfig = cfg; // fallback to local copy if refresh fails
            }
        } else {
            if (!quiet) showToast('Erreur: ' + (result.error || 'inconnue'), 'error');
            if (quiet) throw new Error(result.error);
        }
    } catch (e) {
        console.error('Save error:', e);
        showToast('Erreur réseau lors de la sauvegarde', 'error');
    }

    if (targetBtn) {
        targetBtn.innerHTML = originalText;
    }

    if (typeof snapshotAllValues === 'function') {
        snapshotAllValues();
    }
}

function addTechRow() {
    const body = document.getElementById('techConfigBody');
    // Remove empty state row if present
    const emptyRow = body.querySelector('tr:not([data-idx])');
    if (emptyRow) emptyRow.remove();

    const idx = body.querySelectorAll('tr[data-idx]').length;
    const tr = document.createElement('tr');
    tr.dataset.idx = idx;
    tr.innerHTML = `
        <td><input type="text" class="inline-input tech-nom-input" value="" placeholder="Nom" autofocus></td>
        <td><input type="text" class="inline-input tech-prenom-input" value="" placeholder="Prénom"></td>
        <td><input type="tel" class="inline-input tech-num-input" value="" placeholder="Numéro WhatsApp"></td>
        <td style="text-align:center; display:flex; justify-content:center; gap:8px;">
            <button class="btn-icon btn-edit-tech" onclick="editTechRow(this)" title="Éditer" style="display:none;background:none;border:none;color:var(--text-secondary);cursor:pointer;padding:4px;">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
                    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
                </svg>
            </button>
            <button class="btn-icon btn-save-tech" onclick="saveTechRow(this)" title="Sauvegarder" style="background:none;border:none;color:var(--green-400);cursor:pointer;padding:4px;">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="20 6 9 17 4 12"/>
                </svg>
            </button>
            <button class="btn-icon btn-cancel-tech" onclick="cancelTechRow(this)" title="Annuler" style="background:none;border:none;color:var(--red-400);cursor:pointer;padding:4px;">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="18" y1="6" x2="6" y2="18"></line>
                    <line x1="6" y1="6" x2="18" y2="18"></line>
                </svg>
            </button>
            <button class="btn-delete" onclick="removeTechRow(this)" title="Supprimer" style="padding:4px;cursor:pointer;">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="3 6 5 6 21 6"/>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                </svg>
            </button>
        </td>
    `;
    body.appendChild(tr);
    tr.querySelector('.tech-nom-input').focus();
    updateTechCount();
    tr.style.animation = 'fadeIn 0.25s ease';
}

async function editTechRow(btn) {
    const row = btn.closest('tr');
    const inputs = row.querySelectorAll('.inline-input');
    
    // Store original values so we can cancel
    if (!row.hasAttribute('data-original-nom')) {
        row.setAttribute('data-original-nom', inputs[0].value);
        row.setAttribute('data-original-prenom', inputs[1].value);
        row.setAttribute('data-original-num', inputs[2].value);
    }
    
    // Enable inputs
    inputs.forEach(input => {
        input.disabled = false;
        input.style.opacity = '1';
        input.style.cursor = 'text';
    });
    
    row.querySelector('.btn-edit-tech').style.display = 'none';
    row.querySelector('.btn-save-tech').style.display = 'inline-block';
    row.querySelector('.btn-cancel-tech').style.display = 'inline-block';
    
    inputs[0].focus();
}

async function saveTechRow(btn) {
    const row = btn.closest('tr');
    const inputs = row.querySelectorAll('.inline-input');
    
    let valid = true;
    if(!inputs[0].value.trim()) valid = false;
    
    if(!valid) {
        const ok = await showConfirm("Supprimer ?", "Le nom est vide, voulez-vous supprimer la ligne ?");
        if(ok) {
            row.remove();
            updateTechCount();
            return;
        } else {
            return; // Stay in edit mode
        }
    }
    
    // Lock it
    inputs.forEach(input => {
        input.disabled = true;
        input.style.opacity = '0.7';
        input.style.cursor = 'not-allowed';
    });
    
    // Update originals
    row.setAttribute('data-original-nom', inputs[0].value);
    row.setAttribute('data-original-prenom', inputs[1].value);
    row.setAttribute('data-original-num', inputs[2].value);
    
    row.querySelector('.btn-edit-tech').style.display = 'inline-block';
    row.querySelector('.btn-save-tech').style.display = 'none';
    row.querySelector('.btn-cancel-tech').style.display = 'none';
    
    saveSettings(true);
}

function cancelTechRow(btn) {
    const row = btn.closest('tr');
    const inputs = row.querySelectorAll('.inline-input');
    
    // Revert to original values
    if (row.hasAttribute('data-original-nom')) {
        inputs[0].value = row.getAttribute('data-original-nom');
        inputs[1].value = row.getAttribute('data-original-prenom');
        inputs[2].value = row.getAttribute('data-original-num');
        
        // Lock it
        inputs.forEach(input => {
            input.disabled = true;
            input.style.opacity = '0.7';
            input.style.cursor = 'not-allowed';
        });
        
        row.querySelector('.btn-edit-tech').style.display = 'inline-block';
        row.querySelector('.btn-save-tech').style.display = 'none';
        row.querySelector('.btn-cancel-tech').style.display = 'none';
    } else {
        // If it was a newly added row without originals, just remove it
        row.remove();
        updateTechCount();
    }
}

async function removeTechRow(btn) {
    const ok = await showConfirm("⚠️ Attention", "Voulez-vous vraiment supprimer ce technicien ?");
    if (!ok) return;
    const row = btn.closest('tr');
    row.style.opacity = '0';
    row.style.transform = 'translateX(20px)';
    row.style.transition = 'all 0.2s ease';
    setTimeout(() => {
        row.remove();
        updateTechCount();
        saveSettings(true);
        // Show empty state if no rows left
        const body = document.getElementById('techConfigBody');
        if (!body.querySelectorAll('tr[data-idx]').length) {
            body.innerHTML = '<tr><td colspan="4" class="empty-state small">Aucun technicien — cliquez "Ajouter"</td></tr>';
        }
    }, 200);
}

function updateTechCount() {
    const count = document.querySelectorAll('#techConfigBody tr[data-idx]').length;
    document.getElementById('techCount').textContent = `${count} technicien${count !== 1 ? 's' : ''}`;
}

function filterTechConfigTable() {
    techConfigPage = 1;
    applyTechConfigPagination();
}

function applyTechConfigPagination() {
    const query = (document.getElementById('techConfigSearch')?.value || '').toLowerCase().trim();
    const allRows = Array.from(document.querySelectorAll('#techConfigBody tr[data-idx]'));

    // Filter
    const matchedRows = allRows.filter(row => {
        const nom = (row.querySelector('.tech-nom-input')?.value || '').toLowerCase();
        const prenom = (row.querySelector('.tech-prenom-input')?.value || '').toLowerCase();
        const num = (row.querySelector('.tech-num-input')?.value || '').toLowerCase();
        return !query || nom.includes(query) || prenom.includes(query) || num.includes(query);
    });

    // Pagination
    const totalPages = Math.ceil(matchedRows.length / TECH_CONFIG_PER_PAGE) || 1;
    if (techConfigPage > totalPages) techConfigPage = totalPages;
    const startIdx = (techConfigPage - 1) * TECH_CONFIG_PER_PAGE;
    const endIdx = startIdx + TECH_CONFIG_PER_PAGE;

    // Hide all, show only current page of matched rows
    allRows.forEach(row => row.style.display = 'none');
    matchedRows.forEach((row, i) => {
        row.style.display = (i >= startIdx && i < endIdx) ? '' : 'none';
    });

    // Update count
    const total = allRows.length;
    if (query && matchedRows.length !== total) {
        document.getElementById('techCount').textContent = `${matchedRows.length}/${total} technicien${total !== 1 ? 's' : ''}`;
    } else {
        document.getElementById('techCount').textContent = `${total} technicien${total !== 1 ? 's' : ''}`;
    }

    renderPagination('techConfigPagination', techConfigPage, totalPages, matchedRows.length, 'goTechConfigPage');
}

// ---- Upload ----
function initUpload() {
    const zone = document.getElementById('uploadZone');
    const input = document.getElementById('fileInput');

    zone.addEventListener('click', () => input.click());

    zone.addEventListener('dragover', (e) => {
        e.preventDefault();
        zone.classList.add('dragover');
    });

    zone.addEventListener('dragleave', () => zone.classList.remove('dragover'));

    zone.addEventListener('drop', (e) => {
        e.preventDefault();
        zone.classList.remove('dragover');
        if (e.dataTransfer.files.length) uploadFile(e.dataTransfer.files[0]);
    });

    input.addEventListener('change', () => {
        if (input.files.length) uploadFile(input.files[0]);
    });
}

async function uploadFile(file) {
    if (!file.name.match(/\.xlsx?$/i)) {
        showToast('Seuls les fichiers .xlsx sont acceptés', 'error');
        return;
    }

    const progress = document.getElementById('uploadProgress');
    const fill = document.getElementById('progressFill');
    const text = document.getElementById('progressText');

    progress.style.display = 'block';
    fill.style.width = '30%';
    text.textContent = `Upload de ${file.name}...`;

    const formData = new FormData();
    formData.append('file', file);

    try {
        fill.style.width = '60%';
        const resp = await fetch('/api/upload', { method: 'POST', body: formData });
        fill.style.width = '100%';
        const data = await resp.json();

        if (data.status === 'ok') {
            text.textContent = `✓ ${file.name} analysé avec succès`;
            showToast(`${file.name} importé et analysé`, 'success');
            refreshData();
            setTimeout(() => switchView('dashboard'), 1500);
        } else {
            text.textContent = `⚠ ${data.warning || 'Erreur'}`;
            showToast(data.warning || 'Erreur lors de l\'import', 'error');
        }
    } catch (e) {
        text.textContent = `✗ Erreur: ${e.message}`;
        showToast('Erreur réseau', 'error');
    }

    setTimeout(() => { progress.style.display = 'none'; fill.style.width = '0%'; }, 3000);
}

// ---- Utilities ----
function animateValue(elementId, target) {
    const el = document.getElementById(elementId);
    if (!el) return;
    const current = parseInt(el.textContent) || 0;

    if (current === target) {
        el.textContent = target;
        return;
    }

    const duration = 600;
    const start = performance.now();

    function step(ts) {
        const progress = Math.min((ts - start) / duration, 1);
        const eased = 1 - Math.pow(1 - progress, 3); // easeOutCubic
        el.textContent = Math.round(current + (target - current) * eased);
        if (progress < 1) requestAnimationFrame(step);
    }
    requestAnimationFrame(step);
}

function esc(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

function showToast(message, type = 'info') {
    const container = document.getElementById('toastContainer');
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    const icons = { success: '✓', error: '✗', info: 'ℹ' };
    toast.innerHTML = `<span>${icons[type] || 'ℹ'}</span><span>${esc(message)}</span>`;
    container.appendChild(toast);
    setTimeout(() => {
        toast.style.opacity = '0';
        toast.style.transform = 'translateY(20px)';
        setTimeout(() => toast.remove(), 300);
    }, 4000);
}

// ---- File Panel ----
let filePanelData = [];
let fileSortMode = 'name';

function openFilePanel() {
    document.getElementById('filePanel').classList.add('open');
    document.getElementById('filePanelOverlay').classList.add('open');
    document.getElementById('filePanelSearch').value = '';
    loadFilePanelList();
    setTimeout(() => document.getElementById('filePanelSearch').focus(), 300);
}

function closeFilePanel() {
    document.getElementById('filePanel').classList.remove('open');
    document.getElementById('filePanelOverlay').classList.remove('open');
}

// Close on Escape key
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeFilePanel();
});

async function loadFilePanelList() {
    const list = document.getElementById('filePanelList');
    const countBadge = document.getElementById('filePanelCount');
    try {
        const resp = await fetch('/api/files');
        const files = await resp.json();
        if (files.redirect) {
            window.location.href = files.redirect;
            return;
        }
        filePanelData = files || [];
        countBadge.textContent = filePanelData.length;
        renderFilePanelList(filePanelData);
    } catch (err) {
        list.innerHTML = '<div class="empty-state small">Erreur de chargement</div>';
    }
}

function renderFilePanelList(files) {
    const list = document.getElementById('filePanelList');
    if (!files.length) {
        list.innerHTML = '<div class="empty-state small">Aucun fichier trouvé</div>';
        return;
    }
    list.innerHTML = files.map(f => `
        <div class="file-panel-item ${f.active ? 'active' : ''}" onclick="selectFileFromPanel('${esc(f.name)}')">
            <div class="fp-icon">📊</div>
            <div class="fp-info">
                <div class="fp-name">
                    ${esc(f.display_name || f.name)}
                    ${f.is_today ? '<span class="state-badge en-cours" style="margin-left:8px;padding:2px 6px;font-size:0.65rem;">Aujourd\'hui</span>' : ''}
                </div>
                <div class="fp-meta">${f.modified} · ${formatSize(f.size)}</div>
            </div>
            ${f.active ? '<span class="fp-check">✓</span>' : ''}
        </div>
    `).join('');
}

function filterFileList(query) {
    const q = query.toLowerCase().trim();
    // Allow searching by formatted date string (e.g. "avril") or YYYY-MM-DD
    const filtered = q ? filePanelData.filter(f => 
        (f.name && f.name.toLowerCase().includes(q)) || 
        (f.display_name && f.display_name.toLowerCase().includes(q))
    ) : filePanelData;
    renderFilePanelList(filtered);
}

async function selectFileFromPanel(name) {
    closeFilePanel();
    showToast(`Chargement de ${name}...`, 'info');

    try {
        const resp = await fetch('/api/files/select', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name })
        });
        const data = await resp.json();

        if (data.error) {
            showToast(`Erreur: ${data.error}`, 'error');
            return;
        }

        document.getElementById('fileTagName').textContent = name;
        await refreshData();
        showToast(`${name} chargé avec succès`, 'success');
    } catch (err) {
        showToast('Erreur de chargement du fichier', 'error');
    }
}

function formatSize(bytes) {
    if (bytes < 1024) return bytes + ' o';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' Ko';
    return (bytes / (1024 * 1024)).toFixed(1) + ' Mo';
}


// ---- WhatsApp ----
let waPollingInterval = null;

async function checkWAStatus() {
    try {
        const resp = await fetch('/api/whatsapp/status');
        const data = await resp.json();

        const badge = document.getElementById('waStatusBadge');
        const qrSection = document.getElementById('waQRSection');
        const connSection = document.getElementById('waConnectedSection');

        if (!badge) return;

        if (data.connected) {
            badge.textContent = 'Connecté';
            badge.className = 'wa-status connected';
            if (qrSection) qrSection.style.display = 'none';
            if (connSection) {
                connSection.style.display = 'block';
                document.getElementById('waPhoneDisplay').textContent = '+' + data.phone;
            }
            // Stop polling QR when connected
            if (waPollingInterval) {
                clearInterval(waPollingInterval);
                waPollingInterval = null;
            }
        } else if (data.status === 'scanning') {
            badge.textContent = 'En attente du scan';
            badge.className = 'wa-status scanning';
        } else {
            badge.textContent = 'Déconnecté';
            badge.className = 'wa-status disconnected';
            if (qrSection) qrSection.style.display = 'block';
            if (connSection) connSection.style.display = 'none';
        }
    } catch (err) {
        // Silent fail
    }
}

async function requestWAQR() {
    const btn = document.getElementById('waQRBtn');
    const img = document.getElementById('waQRImage');
    const placeholder = document.getElementById('waQRPlaceholder');

    btn.disabled = true;
    btn.textContent = 'Connexion en cours...';

    try {
        const resp = await fetch('/api/whatsapp/qr');

        if (resp.headers.get('content-type')?.includes('image/png')) {
            const blob = await resp.blob();
            if (img.src && img.src.startsWith('blob:')) URL.revokeObjectURL(img.src);
            img.src = URL.createObjectURL(blob);
            img.style.display = 'block';
            if (placeholder) placeholder.style.display = 'none';

            document.getElementById('waStatusBadge').textContent = 'En attente du scan';
            document.getElementById('waStatusBadge').className = 'wa-status scanning';

            // Poll status every 3s + auto-refresh QR every 18s
            if (waPollingInterval) clearInterval(waPollingInterval);
            let qrRefreshCount = 0;
            waPollingInterval = setInterval(async () => {
                qrRefreshCount++;
                await checkWAStatus();
                // Auto-refresh QR every 18s (whatsmeow sends new codes every ~20s)
                if (qrRefreshCount % 6 === 0) {
                    try {
                        const r = await fetch('/api/whatsapp/qr');
                        if (r.headers.get('content-type')?.includes('image/png')) {
                            const b = await r.blob();
                            if (img.src && img.src.startsWith('blob:')) URL.revokeObjectURL(img.src);
                            img.src = URL.createObjectURL(b);
                        }
                    } catch (e) { /* silent */ }
                }
            }, 3000);

            btn.textContent = 'Actualiser le QR';
            btn.disabled = false;
        } else {
            const data = await resp.json();
            if (data.status === 'connected') {
                showToast('WhatsApp déjà connecté !', 'success');
                checkWAStatus();
            } else {
                showToast(data.error || 'Erreur QR — réessayez', 'error');
            }
            btn.textContent = 'Lier WhatsApp';
            btn.disabled = false;
        }
    } catch (err) {
        showToast('Erreur de connexion WhatsApp', 'error');
        btn.textContent = 'Lier WhatsApp';
        btn.disabled = false;
    }
}

async function sendTestWA() {
    const cfg = await (await fetch('/api/config')).json();
    const myNumber = cfg.MY_NUMBER;

    if (!myNumber) {
        showToast('Aucun Numéro principal configuré', 'error');
        return;
    }

    showToast(`Envoi du test au numéro principal...`, 'info');

    try {
        const resp = await fetch('/api/whatsapp/send', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                to: myNumber.replace('+', ''),
                message: cfg.MSG_TEST || '✅ Test Moca Consult — la connexion WhatsApp fonctionne !'
            })
        });
        const data = await resp.json();
        if (data.status === 'sent') {
            showToast('✅ Test WhatsApp envoyé avec succès au numéro principal !', 'success');
        } else {
            showToast(`Échec WhatsApp: ${data.error}`, 'error');
            console.warn(`Échec de l'envoi WhatsApp: ${data.error}`);
        }
    } catch (err) {
        showToast('Erreur serveur lors de l\'envoi du test', 'error');
        console.error('Erreur WhatsApp:', err);
    }
}

async function logoutWA() {
    const ok = await showConfirm("WhatsApp", "Voulez-vous vraiment déconnecter WhatsApp ?");
    if (!ok) return;

    try {
        await fetch('/api/whatsapp/logout', { method: 'POST' });
        showToast('WhatsApp déconnecté', 'info');
        checkWAStatus();
    } catch (err) {
        showToast('Erreur de déconnexion', 'error');
    }
}

// ---- Generic Settings Change Tracking ----
let _numSnapshot = "";
let _genSnapshot = {};
let _msgSnapshot = {};

function snapshotNumValues() {
    const el = document.getElementById('settingsMyNumber');
    if (el) _numSnapshot = el.value;
}
function hasNumChanged() {
    const el = document.getElementById('settingsMyNumber');
    return el ? el.value !== _numSnapshot : false;
}
function checkNumChanges() {
    const btn = document.getElementById('saveNumBtn');
    if (!btn) return;
    if (hasNumChanged()) {
        btn.disabled = false; btn.style.opacity = '1'; btn.style.cursor = 'pointer';
    } else {
        btn.disabled = true; btn.style.opacity = '0.45'; btn.style.cursor = 'not-allowed';
    }
}

function snapshotGenValues() {
    _genSnapshot = {
        morning: document.getElementById('settingsMorningTime')?.value,
        interval: document.getElementById('settingsInterval')?.value,
        eod: document.getElementById('settingsEODTime')?.value,
        wa: document.getElementById('settingsWhatsApp')?.checked
    };
}
function hasGenChanged() {
    return _genSnapshot.morning !== document.getElementById('settingsMorningTime')?.value ||
           _genSnapshot.interval !== document.getElementById('settingsInterval')?.value ||
           _genSnapshot.eod !== document.getElementById('settingsEODTime')?.value ||
           _genSnapshot.wa !== document.getElementById('settingsWhatsApp')?.checked;
}
function checkGenChanges() {
    const btn = document.getElementById('saveGenBtn');
    if (!btn) return;
    if (hasGenChanged()) {
        btn.disabled = false; btn.style.opacity = '1'; btn.style.cursor = 'pointer';
    } else {
        btn.disabled = true; btn.style.opacity = '0.45'; btn.style.cursor = 'not-allowed';
    }
}

function snapshotMsgValues() {
    _msgSnapshot = {
        test: document.getElementById('settingsMsgTest')?.value,
        morning: document.getElementById('settingsMsgMorning')?.value,
        eod: document.getElementById('settingsMsgEODThanks')?.value
    };
}
function hasMsgChanged() {
    return _msgSnapshot.test !== document.getElementById('settingsMsgTest')?.value ||
           _msgSnapshot.morning !== document.getElementById('settingsMsgMorning')?.value ||
           _msgSnapshot.eod !== document.getElementById('settingsMsgEODThanks')?.value;
}
function checkMsgChanges() {
    const btn = document.getElementById('saveMsgBtn');
    if (!btn) return;
    if (hasMsgChanged()) {
        btn.disabled = false; btn.style.opacity = '1'; btn.style.cursor = 'pointer';
    } else {
        btn.disabled = true; btn.style.opacity = '0.45'; btn.style.cursor = 'not-allowed';
    }
}

let _trackingInitGeneral = false;
function initChangeTracking() {
    if (_trackingInitGeneral) return;
    _trackingInitGeneral = true;

    const numInput = document.getElementById('settingsMyNumber');
    if (numInput) numInput.addEventListener('input', checkNumChanges);
    
    ['settingsMorningTime', 'settingsInterval', 'settingsEODTime'].forEach(id => {
        const el = document.getElementById(id);
        if (el) el.addEventListener('input', checkGenChanges);
    });
    const wa = document.getElementById('settingsWhatsApp');
    if (wa) wa.addEventListener('change', checkGenChanges);
    
    ['settingsMsgTest', 'settingsMsgMorning', 'settingsMsgEODThanks'].forEach(id => {
        const el = document.getElementById(id);
        if (el) el.addEventListener('input', checkMsgChanges);
    });
}

function snapshotAllValues() {
    snapshotNumValues();
    snapshotGenValues();
    snapshotMsgValues();
    checkNumChanges();
    checkGenChanges();
    checkMsgChanges();
}

// ---- Auth / Logout ----

async function handleAppLogout() {
    try {
        const btn = document.getElementById('logoutBtn');
        if (btn) btn.style.opacity = '0.5';
        await fetch('/api/auth/logout', { method: 'POST' });
    } catch (e) { console.error('Logout error:', e); }
    window.location.href = '/login';
}

// Ensure the logout button is bound securely
document.addEventListener('DOMContentLoaded', () => {
    const btn = document.getElementById('logoutBtn');
    if (btn) {
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            handleAppLogout();
        });
    }
});

// Intercept 401 responses globally to redirect to login
const _origFetch = window.fetch;
window.fetch = async function(...args) {
    const resp = await _origFetch.apply(this, args);
    if (resp.status === 401 && !args[0]?.toString().includes('/api/auth/')) {
        window.location.href = '/login';
    }
    return resp;
};

// ---- SMTP ----

async function testSMTP() {
    const btn = document.getElementById('smtpTestBtn');
    const emailInput = document.getElementById('smtpTestEmail');
    const targetEmail = emailInput ? emailInput.value.trim() : '';

    btn.disabled = true;
    btn.textContent = '⏳ Envoi...';
    try {
        // Save first to update SMTP config (quietly)
        await saveSettings(true);
        const resp = await fetch('/api/smtp/test', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ email: targetEmail })
        });
        const data = await resp.json();
        if (data.error) {
            showToast('❌ SMTP: ' + data.error, 'error');
        } else {
            showToast('✅ ' + data.message, 'success');
        }
    } catch (err) {
        showToast('❌ Erreur SMTP', 'error');
    }
    btn.disabled = false;
    btn.textContent = 'Envoyer le test';
}

// ---- Zitadel SMTP Management ----

async function loadZitadelSMTPStatus() {
    try {
        const resp = await fetch('/api/smtp/zitadel');
        const data = await resp.json();

        if (data.error) {
            // Zitadel not reachable — keep badge as-is, fields come from local config
            snapshotSMTPValues();
            initSMTPChangeTracking();
            return;
        }

        const configs = data.result || [];
        if (configs.length === 0) {
            // No Zitadel SMTP config — fields stay from local config
            snapshotSMTPValues();
            initSMTPChangeTracking();
            return;
        }

        const active = configs.find(c => c.state === 'SMTP_CONFIG_ACTIVE');
        const config = active || configs[0];

        // Populate form fields from Zitadel's config
        const hostParts = (config.host || '').split(':');
        document.getElementById('settingsSMTPHost').value = hostParts[0] || '';
        document.getElementById('settingsSMTPPort').value = hostParts[1] || '587';
        document.getElementById('settingsSMTPUsername').value = config.user || '';
        document.getElementById('settingsSMTPFrom').value = config.senderAddress || '';
        document.getElementById('settingsSMTPSenderName').value = config.senderName || 'Moca Tracker';
        document.getElementById('settingsSMTPTLS').checked = config.tls !== false;
        // Don't populate password — Zitadel never returns it

    } catch (e) {
        // Silent — fields stay from local config
    }

    // Snapshot SMTP values for change tracking
    snapshotSMTPValues();
    initSMTPChangeTracking();
}

async function syncSMTPToZitadel() {
    const btn = document.getElementById('saveSMTPBtn');
    btn.disabled = true;
    btn.innerHTML = '<span class="spinner"></span> Synchronisation...';

    try {
        const host = document.getElementById('settingsSMTPHost').value.trim();
        const port = parseInt(document.getElementById('settingsSMTPPort').value) || 587;
        const user = document.getElementById('settingsSMTPUsername').value.trim();
        const password = document.getElementById('settingsSMTPPassword').value.trim();
        const from = document.getElementById('settingsSMTPFrom').value.trim();
        const senderName = document.getElementById('settingsSMTPSenderName').value.trim() || 'Moca Tracker';
        const tls = document.getElementById('settingsSMTPTLS').checked;

        if (!host || !from) {
            showToast('❌ Serveur SMTP et adresse d\'envoi requis', 'error');
            btn.disabled = false;
            btn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="vertical-align:-2px;margin-right:4px;"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/></svg> Sauvegarder SMTP';
            return;
        }

        // Also save to local config (for backward compatibility)
        await saveSettings(true);

        // Sync to Zitadel
        const resp = await fetch('/api/smtp/zitadel', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                host: host,
                port: port,
                user: user,
                password: password,
                senderAddress: from,
                senderName: senderName,
                tls: tls
            })
        });
        const data = await resp.json();

        if (data.error) {
            showToast('❌ Système SMTP : ' + data.error, 'error');
        } else {
            showToast('✅ ' + data.message, 'success');
            // Refresh status badge
            await loadZitadelSMTPStatus();
        }
    } catch (err) {
        showToast('❌ Erreur de synchronisation SMTP', 'error');
    }

    btn.disabled = false;
    btn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="vertical-align:-2px;margin-right:4px;"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/></svg> Sauvegarder SMTP';

    // Re-snapshot and disable save button (no pending changes)
    snapshotSMTPValues();
    disableSaveSMTP();
}

// ---- SMTP Change Tracking ----

let _smtpSnapshot = {};
let _smtpTrackingInit = false;

const SMTP_FIELD_IDS = [
    'settingsSMTPHost', 'settingsSMTPPort', 'settingsSMTPUsername',
    'settingsSMTPPassword', 'settingsSMTPFrom', 'settingsSMTPSenderName'
];

function snapshotSMTPValues() {
    _smtpSnapshot = {};
    SMTP_FIELD_IDS.forEach(id => {
        const el = document.getElementById(id);
        if (el) _smtpSnapshot[id] = el.value;
    });
    const tls = document.getElementById('settingsSMTPTLS');
    if (tls) _smtpSnapshot['settingsSMTPTLS'] = tls.checked;
}

function hasSMTPChanged() {
    for (const id of SMTP_FIELD_IDS) {
        const el = document.getElementById(id);
        if (el && el.value !== (_smtpSnapshot[id] || '')) return true;
    }
    const tls = document.getElementById('settingsSMTPTLS');
    if (tls && tls.checked !== _smtpSnapshot['settingsSMTPTLS']) return true;
    return false;
}

function enableSaveSMTP() {
    const btn = document.getElementById('saveSMTPBtn');
    if (btn && btn.disabled) {
        btn.disabled = false;
        btn.style.opacity = '1';
        btn.style.cursor = 'pointer';
    }
}

function disableSaveSMTP() {
    const btn = document.getElementById('saveSMTPBtn');
    if (btn) {
        btn.disabled = true;
        btn.style.opacity = '0.45';
        btn.style.cursor = 'not-allowed';
    }
}

function checkSMTPChanges() {
    if (hasSMTPChanged()) {
        enableSaveSMTP();
    } else {
        disableSaveSMTP();
    }
}

function initSMTPChangeTracking() {
    if (_smtpTrackingInit) return;
    _smtpTrackingInit = true;

    SMTP_FIELD_IDS.forEach(id => {
        const el = document.getElementById(id);
        if (el) el.addEventListener('input', checkSMTPChanges);
    });
    const tls = document.getElementById('settingsSMTPTLS');
    if (tls) tls.addEventListener('change', checkSMTPChanges);
}

async function testZitadelSMTP() {
    const btn = document.getElementById('smtpTestBtn');
    const email = document.getElementById('smtpTestEmail').value.trim();

    if (!email) {
        showToast('❌ Entrez une adresse email destinataire', 'error');
        return;
    }

    btn.disabled = true;
    btn.textContent = '⏳ Envoi...';

    try {
        const resp = await fetch('/api/smtp/test', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ email: email })
        });
        const data = await resp.json();

        if (data.error) {
            showToast('❌ ' + data.error, 'error');
        } else {
            showToast('✅ ' + data.message, 'success');
        }
    } catch (err) {
        showToast('❌ Erreur de test SMTP', 'error');
    }

    btn.disabled = false;
    btn.textContent = '📨 Envoyer le test';
}

// ---- Google Drive ----

async function loadDriveStatus() {
    try {
        const resp = await fetch('/api/drive/status');
        const data = await resp.json();
        const badge = document.getElementById('driveStatusBadge');
        const notConn = document.getElementById('driveNotConnected');
        const conn = document.getElementById('driveConnected');

        if (data.configured) {
            badge.textContent = 'Connecté';
            badge.className = 'wa-status connected';
            notConn.style.display = 'none';
            conn.style.display = 'block';

            const folderDisplay = document.getElementById('driveFolderDisplay');
            if (folderDisplay) folderDisplay.textContent = '📂 ' + (data.folder_name || data.folder_id);
        } else {
            badge.textContent = 'Non configuré';
            badge.className = 'wa-status';
            notConn.style.display = 'block';
            conn.style.display = 'none';
        }
    } catch (e) {
        console.error('Drive status error:', e);
    }
}

async function setDriveLink() {
    const input = document.getElementById('driveLinkInput');
    const link = input?.value.trim();
    if (!link) {
        showToast('❌ Collez un lien de dossier Google Drive', 'error');
        return;
    }

    showToast('⏳ Vérification...', 'info');

    try {
        const resp = await fetch('/api/drive/set-folder', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ link: link })
        });
        const data = await resp.json();
        if (data.error) {
            showToast('❌ ' + data.error, 'error');
        } else {
            showToast('✅ Google Drive connecté !', 'success');
            loadDriveStatus();
        }
    } catch (err) {
        showToast('❌ Erreur de connexion', 'error');
    }
}

async function disconnectGDrive() {
    const ok = await showConfirm("Google Drive", "Déconnecter Google Drive ?");
    if (!ok) return;
    try {
        await fetch('/api/drive/disconnect', { method: 'POST' });
        showToast('Google Drive déconnecté', 'info');
        loadDriveStatus();
    } catch (err) {
        showToast('Erreur de déconnexion', 'error');
    }
}

async function syncDriveNow() {
    showToast('🔄 Synchronisation en cours...', 'info');
    try {
        const resp = await fetch('/api/drive/sync', { method: 'POST' });
        const data = await resp.json();
        if (data.error) {
            showToast('❌ ' + data.error, 'error');
        } else {
            showToast(`✅ ${data.downloaded} fichier(s) téléchargé(s)`, 'success');
            if (data.downloaded > 0) refreshData();
        }
} catch (err) {
        showToast('❌ Erreur de synchronisation', 'error');
    }
}

function toggleNumberLock(btn) {
    const input = document.getElementById('settingsMyNumber');
    if (input.readOnly) {
        input.readOnly = false;
        input.style.backgroundColor = 'var(--bg-primary)';
        btn.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
            <path d="M7 11V7a5 5 0 0 1 9.9-1"></path>
        </svg>`;
        btn.title = "Verrouiller";
        input.focus();
    } else {
        input.readOnly = true;
        input.style.backgroundColor = 'var(--bg-tertiary)';
        btn.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="lock-icon">
            <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
            <path d="M7 11V7a5 5 0 0 1 10 0v4"></path>
        </svg>`;
        btn.title = "Modifier (déverrouiller)";
        saveSettings(false); // Automatically trigger save when locking
    }
}

// ---- FluxUI Inspired Custom Time Pickers ----
function initFluxTimePickers() {
    const inputs = document.querySelectorAll('input[type="time"]');
    inputs.forEach(input => {
        if (input.dataset.fluxInitialized) return;
        input.dataset.fluxInitialized = "true";

        const wrapper = document.createElement('div');
        wrapper.className = 'flux-tp-wrapper';
        input.parentNode.insertBefore(wrapper, input);

        input.style.display = 'none';
        wrapper.appendChild(input);

        let optionsHtml = '<div class="flux-tp-columns">';
        const isOptInterval = input.id === 'settingsInterval';
        const startHour = isOptInterval ? 1 : 0;
        const endHour = isOptInterval ? 12 : 23;

        optionsHtml += '<div class="flux-tp-col flux-tp-hours">';
        for (let h = startHour; h <= endHour; h++) {
            const hh = String(h).padStart(2, '0');
            optionsHtml += `<div class="flux-tp-item hour-item" data-h="${hh}">${hh}</div>`;
        }
        optionsHtml += '</div>';

        optionsHtml += '<div class="flux-tp-col flux-tp-minutes">';
        for (let m = 0; m < 60; m++) {
            const mm = String(m).padStart(2, '0');
            optionsHtml += `<div class="flux-tp-item minute-item" data-m="${mm}">${mm}</div>`;
        }
        optionsHtml += '</div></div>';

        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'flux-tp-btn';
        btn.innerHTML = `
            <span class="flux-tp-val">${input.value || '--:--'}</span>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="flux-tp-icon">
                <circle cx="12" cy="12" r="10"></circle>
                <polyline points="12 6 12 12 16 14"></polyline>
            </svg>
        `;
        wrapper.appendChild(btn);

        const dropdown = document.createElement('div');
        dropdown.className = 'flux-tp-dropdown flux-tp-dual';
        dropdown.innerHTML = optionsHtml;
        wrapper.appendChild(dropdown);

        let currentH = input.value ? input.value.split(':')[0] : (isOptInterval ? '02' : '08');
        let currentM = input.value ? input.value.split(':')[1] : '00';

        btn.addEventListener('click', (e) => {
            e.stopPropagation();
            const isOpen = dropdown.classList.contains('show');
            document.querySelectorAll('.flux-tp-dropdown').forEach(d => { d.classList.remove('show', 'visible'); });
            document.querySelectorAll('.flux-tp-btn').forEach(b => { b.classList.remove('open'); });

            if (!isOpen) {
                btn.classList.add('open');
                dropdown.classList.add('show');
                
                if (input.value) {
                    currentH = input.value.split(':')[0];
                    currentM = input.value.split(':')[1];
                }
                
                dropdown.querySelectorAll('.hour-item').forEach(el => {
                    el.classList.toggle('selected', el.dataset.h === currentH);
                });
                dropdown.querySelectorAll('.minute-item').forEach(el => {
                    el.classList.toggle('selected', el.dataset.m === currentM);
                });

                setTimeout(() => dropdown.classList.add('visible'), 10);
                
                const selH = dropdown.querySelector('.hour-item.selected');
                const selM = dropdown.querySelector('.minute-item.selected');
                const colH = dropdown.querySelector('.flux-tp-hours');
                const colM = dropdown.querySelector('.flux-tp-minutes');
                if (selH) colH.scrollTop = selH.offsetTop - colH.clientHeight / 2 + selH.clientHeight / 2;
                if (selM) colM.scrollTop = selM.offsetTop - colM.clientHeight / 2 + selM.clientHeight / 2;
            }
        });

        dropdown.addEventListener('click', (e) => e.stopPropagation());

        dropdown.querySelectorAll('.hour-item').forEach(opt => {
            opt.addEventListener('click', () => {
                currentH = opt.dataset.h;
                dropdown.querySelectorAll('.hour-item').forEach(el => el.classList.remove('selected'));
                opt.classList.add('selected');
                updateTime();
            });
        });

        dropdown.querySelectorAll('.minute-item').forEach(opt => {
            opt.addEventListener('click', () => {
                currentM = opt.dataset.m;
                dropdown.querySelectorAll('.minute-item').forEach(el => el.classList.remove('selected'));
                opt.classList.add('selected');
                updateTime();
            });
        });

        function updateTime() {
            const timeStr = `${currentH}:${currentM}`;
            input.value = timeStr;
            btn.querySelector('.flux-tp-val').textContent = timeStr;
            input.dispatchEvent(new Event('input', { bubbles: true }));
        }
    });

    // Close on click outside
    document.addEventListener('click', () => {
        document.querySelectorAll('.flux-tp-dropdown').forEach(d => d.classList.remove('show', 'visible'));
        document.querySelectorAll('.flux-tp-btn').forEach(b => b.classList.remove('open'));
    });
}

window.syncFluxTimePickers = function() {
    document.querySelectorAll('input[type="time"]').forEach(input => {
        const wrapper = input.closest('.flux-tp-wrapper');
        if (wrapper) {
            wrapper.querySelector('.flux-tp-val').textContent = input.value ? input.value.substring(0,5) : '--:--';
        }
    });
}
