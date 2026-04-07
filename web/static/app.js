/* ============================================
   Fiber Tracker — Dashboard App Logic
   ============================================ */

// ---- Theme (runs immediately to prevent flash) ----
(function initTheme() {
    const saved = localStorage.getItem('theme');
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    const theme = saved || (prefersDark ? 'dark' : 'dark'); // default dark
    document.documentElement.setAttribute('data-theme', theme);
})();

function toggleTheme() {
    const html = document.documentElement;
    const current = html.getAttribute('data-theme') || 'dark';
    const next = current === 'dark' ? 'light' : 'dark';
    html.setAttribute('data-theme', next);
    localStorage.setItem('theme', next);
    // Re-render charts with new theme colors
    if (statsData) {
        updateDashboard(statsData);
    }
}

// ---- State ----
let currentView = 'dashboard';
let statsData = null;
let chartMode = 'global';      // global | racc | sav
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
    document.title = `Technosmart — ${viewTitles[view] || 'Dashboard'}`;

    // Load data for specific views
    if (view === 'notifications') loadNotifications();
    if (view === 'settings') loadSettings();
}

// ---- NTP Clock ----
function initNTPClock() {
    fetchNTPTime();
    setInterval(fetchNTPTime, 10000); // refresh every 10s
}

async function fetchNTPTime() {
    try {
        const resp = await fetch('/api/time');
        const data = await resp.json();
        const timeEl = document.getElementById('ntpTime');
        const dateEl = document.getElementById('ntpDate');
        if (timeEl) timeEl.textContent = data.time || '--:--:--';
        if (dateEl) {
            dateEl.textContent = `${data.date || ''} · France`;
        }
    } catch (e) {
        // Fallback: show local time
        const now = new Date();
        const timeEl = document.getElementById('ntpTime');
        if (timeEl) timeEl.textContent = now.toLocaleTimeString('fr-FR');
    }
}

// ---- Data Fetching ----
async function refreshData() {
    try {
        const resp = await fetch('/api/stats');
        const data = await resp.json();
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
    animateValue('metricOK', data.total_ok || 0);
    animateValue('metricNOK', data.total_nok || 0);

    const rate = ((data.rate_ok || 0) * 100);
    document.getElementById('metricRate').textContent = rate.toFixed(1) + '%';
    document.getElementById('metricRate').style.color = rate >= 80 ? 'var(--green-400)' : rate >= 60 ? 'var(--amber-400)' : 'var(--red-400)';

    document.getElementById('metricPDC').textContent = data.pdc || 0;
    document.getElementById('metricInProgress').textContent = data.in_progress || 0;

    const rateLabel = document.getElementById('metricRateLabel');
    if (rate >= 80) {
        rateLabel.textContent = '✓ Objectif atteint';
        rateLabel.style.color = 'var(--green-400)';
    } else {
        rateLabel.textContent = '⚠ Sous objectif';
        rateLabel.style.color = 'var(--amber-400)';
    }

    // Progress bars
    const total = data.total || 1;
    document.getElementById('barOK').style.width = ((data.total_ok / total) * 100) + '%';
    document.getElementById('barNOK').style.width = ((data.total_nok / total) * 100) + '%';

    // NOK badge
    document.getElementById('nokBadge').textContent = data.total_nok || 0;

    // Source file
    const fileTagName = document.getElementById('fileTagName');
    if (data.source_file) {
        const name = data.source_file.split('/').pop();
        fileTagName.textContent = name;
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
    } else if (chartMode === 'sav') {
        ok = data.sav_ok || 0;
        nok = data.sav_nok || 0;
        label = 'SAV';
    } else {
        ok = data.total_ok || 0;
        nok = data.total_nok || 0;
        label = 'Global';
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
                <div class="tech-bar-nok" data-width="${nokW}" style="width:0%;transition:width 0.6s ${(parseFloat(delay)+0.1).toFixed(2)}s cubic-bezier(0.16,1,0.3,1)"></div>
            </div>
            <span class="tech-bar-value" style="color:${rateColor}">${(t.rate_ok * 100).toFixed(0)}%</span>
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

// ---- NOK Table (Dashboard) ----
function updateNOKTable(data) {
    const card = document.getElementById('nokAlertCard');
    const body = document.getElementById('nokTableBody');
    const count = document.getElementById('nokAlertCount');
    const noks = data.nok_records || [];

    if (!noks.length) {
        card.style.display = 'none';
        return;
    }

    card.style.display = 'block';
    count.textContent = noks.length;

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

// ---- Interventions Table ----
function updateInterventions(data) {
    const body = document.getElementById('interventionsBody');
    const records = data.all_records || [];
    renderInterventionTable(records);
}

function renderInterventionTable(records) {
    const body = document.getElementById('interventionsBody');
    const searchVal = (document.getElementById('interventionSearch')?.value || '').toLowerCase();
    const stateVal = document.getElementById('stateFilter')?.value || '';
    const typeVal = document.getElementById('typeFilter')?.value || '';

    let filtered = records;
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

    document.getElementById('interventionCount').textContent = `${filtered.length} intervention${filtered.length !== 1 ? 's' : ''}`;

    body.innerHTML = filtered.map(r => {
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
}

function initFilters() {
    ['interventionSearch', 'stateFilter', 'typeFilter'].forEach(id => {
        const el = document.getElementById(id);
        if (el) {
            el.addEventListener('input', () => {
                if (statsData) renderInterventionTable(statsData.all_records || []);
            });
            el.addEventListener('change', () => {
                if (statsData) renderInterventionTable(statsData.all_records || []);
            });
        }
    });

    // Tech sort
    const techSort = document.getElementById('techSort');
    if (techSort) {
        techSort.addEventListener('change', () => {
            if (statsData) updateTechnicians(statsData);
        });
    }
}

// ---- Technicians View ----
function updateTechnicians(data) {
    const container = document.getElementById('techGrid');
    let techs = (data.by_technician || []).slice();
    const sortVal = document.getElementById('techSort')?.value || 'nok';

    switch (sortVal) {
        case 'nok': techs.sort((a, b) => b.nok - a.nok); break;
        case 'ok': techs.sort((a, b) => b.ok - a.ok); break;
        case 'rate': techs.sort((a, b) => a.rate_ok - b.rate_ok); break;
        case 'name': techs.sort((a, b) => a.name.localeCompare(b.name)); break;
    }

    if (!techs.length) {
        container.innerHTML = '<div class="empty-state">Aucune donnée technicien</div>';
        return;
    }

    const avatarColors = [
        ['#1d4ed8', '#dbeafe'], ['#7c3aed', '#ede9fe'], ['#059669', '#d1fae5'],
        ['#dc2626', '#fee2e2'], ['#d97706', '#fef3c7'], ['#0891b2', '#cffafe'],
        ['#c026d3', '#fae8ff'], ['#4338ca', '#e0e7ff']
    ];

    container.innerHTML = techs.map((t, i) => {
        const initials = t.name.split(' ').map(n => n[0]).join('').toUpperCase().slice(0, 2);
        const [bg, fg] = avatarColors[i % avatarColors.length];
        const rate = (t.rate_ok * 100);
        const rateColor = rate >= 80 ? 'var(--green-400)' : rate >= 60 ? 'var(--amber-400)' : 'var(--red-400)';
        const total = t.total || 1;

        return `<div class="tech-card">
            <div class="tech-card-header">
                <div class="tech-avatar" style="background:${bg};color:${fg}">${initials}</div>
                <div>
                    <div class="tech-card-name">${esc(t.name)}</div>
                    <div class="tech-card-sector">Secteur ${esc(t.sector)}</div>
                </div>
                <div class="tech-card-rate">
                    <div class="tech-card-rate-value" style="color:${rateColor}">${rate.toFixed(0)}%</div>
                    <div class="tech-card-rate-label">Taux OK</div>
                </div>
            </div>
            <div class="tech-card-stats">
                <div class="tech-stat">
                    <div class="tech-stat-label">OK</div>
                    <div class="tech-stat-value green">${t.ok}</div>
                </div>
                <div class="tech-stat">
                    <div class="tech-stat-label">NOK</div>
                    <div class="tech-stat-value red">${t.nok}</div>
                </div>
                <div class="tech-stat">
                    <div class="tech-stat-label">RACC</div>
                    <div class="tech-stat-value">${t.racc_ok + t.racc_nok}</div>
                </div>
                <div class="tech-stat">
                    <div class="tech-stat-label">SAV</div>
                    <div class="tech-stat-value">${t.sav_ok + t.sav_nok}</div>
                </div>
            </div>
            <div class="tech-card-bar">
                <div class="tech-card-bar-ok" style="width:${(t.ok/total*100).toFixed(1)}%"></div>
                <div class="tech-card-bar-nok" style="width:${(t.nok/total*100).toFixed(1)}%"></div>
            </div>
        </div>`;
    }).join('');
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
            <div class="failure-bar-track"><div class="failure-bar-fill" style="width:${(count/maxCat*100).toFixed(1)}%"></div></div>
        </div>
    `).join('') : '<div class="empty-state small">Aucune donnée</div>';

    // By type
    const typeContainer = document.getElementById('failureByType');
    const types = Object.entries(data.failures_by_type || {}).sort((a, b) => b[1] - a[1]);
    const maxType = Math.max(...types.map(t => t[1]), 1);

    typeContainer.innerHTML = types.length ? types.map(([label, count]) => `
        <div class="failure-bar-row">
            <div class="failure-bar-label"><span>${esc(label)}</span><span>${count}</span></div>
            <div class="failure-bar-track"><div class="failure-bar-fill" style="width:${(count/maxType*100).toFixed(1)}%"></div></div>
        </div>
    `).join('') : '<div class="empty-state small">Aucune donnée</div>';

    // Detailed table
    const failBody = document.getElementById('failureBody');
    const noks = data.nok_records || [];
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

    container.innerHTML = notifications.map(n => {
        const icons = { stats: '📊', nok_alert: '🚨', morning: '🌅' };
        const icon = icons[n.type] || '📨';
        const ts = new Date(n.timestamp).toLocaleString('fr-FR');

        return `<div class="notif-item">
            <div class="notif-icon ${n.type}">${icon}</div>
            <div class="notif-body">
                <div class="notif-type">${esc(n.type)} → ${esc(n.recipient)}</div>
                <div class="notif-msg">${esc(n.message)}</div>
            </div>
            <div class="notif-time">${ts}</div>
        </div>`;
    }).join('');
}

// ---- Settings ----
let currentConfig = null;

async function loadSettings() {
    try {
        const resp = await fetch('/api/config');
        currentConfig = await resp.json();

        document.getElementById('settingsMyNumber').value = currentConfig.MY_NUMBER || '';
        document.getElementById('settingsWatchFolder').value = currentConfig.WATCH_FOLDER || '';
        document.getElementById('settingsHour').value = currentConfig.MORNING_HOUR || 8;
        document.getElementById('settingsInterval').value = currentConfig.STATS_INTERVAL_HOURS || 2;
        document.getElementById('settingsEODHour').value = currentConfig.EOD_HOUR || 17;
        document.getElementById('settingsWhatsApp').checked = currentConfig.WHATSAPP_ENABLED || false;

        // Build editable tech table
        const body = document.getElementById('techConfigBody');
        const techs = currentConfig.TECHNICIENS || {};
        const entries = Object.entries(techs);

        if (!entries.length) {
            body.innerHTML = '<tr><td colspan="3" class="empty-state small">Aucun technicien — cliquez "Ajouter"</td></tr>';
        } else {
            body.innerHTML = entries.map(([name, num], i) => `
                <tr data-idx="${i}">
                    <td><input type="text" class="inline-input tech-name-input" value="${esc(name)}" placeholder="NOM Prénom"></td>
                    <td><input type="tel" class="inline-input tech-num-input" value="${esc(num)}" placeholder="+33612345678"></td>
                    <td style="text-align:center">
                        <button class="btn-delete" onclick="removeTechRow(this)" title="Supprimer">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <polyline points="3 6 5 6 21 6"/>
                                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                            </svg>
                        </button>
                    </td>
                </tr>
            `).join('');
        }
        updateTechCount();

        // Check WhatsApp connection status
        checkWAStatus();
    } catch (e) {
        console.error('Load settings error:', e);
        showToast('Erreur lors du chargement des paramètres', 'error');
    }
}

async function saveSettings() {
    const btn = document.getElementById('saveSettingsBtn');
    btn.disabled = true;
    btn.innerHTML = '<span class="spinner"></span> Sauvegarde...';

    try {
        // Gather technicians from the table
        const techs = {};
        const rows = document.querySelectorAll('#techConfigBody tr[data-idx]');
        rows.forEach(row => {
            const name = row.querySelector('.tech-name-input')?.value.trim();
            const num = row.querySelector('.tech-num-input')?.value.trim();
            if (name && num) {
                techs[name] = num;
            }
        });

        const cfg = {
            TECHNICIENS: techs,
            MY_NUMBER: document.getElementById('settingsMyNumber').value.trim(),
            WATCH_FOLDER: document.getElementById('settingsWatchFolder').value.trim(),
            EXCEL_PATTERN: currentConfig?.EXCEL_PATTERN || '*.xlsx',
            MORNING_HOUR: parseInt(document.getElementById('settingsHour').value) || 8,
            MORNING_MINUTE: currentConfig?.MORNING_MINUTE || 0,
            STATS_INTERVAL_HOURS: parseInt(document.getElementById('settingsInterval').value) || 2,
            EOD_HOUR: parseInt(document.getElementById('settingsEODHour').value) || 17,
            FRANCE_TZ: currentConfig?.FRANCE_TZ || 'Europe/Paris',
            NTP_SERVER: currentConfig?.NTP_SERVER || 'fr.pool.ntp.org',
            WEB_PORT: currentConfig?.WEB_PORT || 8080,
            WHATSAPP_ENABLED: document.getElementById('settingsWhatsApp').checked
        };

        const resp = await fetch('/api/config', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(cfg)
        });

        const result = await resp.json();
        if (result.status === 'saved') {
            showToast('✅ Paramètres sauvegardés !', 'success');
            currentConfig = cfg;
        } else {
            showToast('Erreur: ' + (result.error || 'inconnue'), 'error');
        }
    } catch (e) {
        console.error('Save error:', e);
        showToast('Erreur réseau lors de la sauvegarde', 'error');
    }

    btn.disabled = false;
    btn.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/>
        <polyline points="17 21 17 13 7 13 7 21"/>
        <polyline points="7 3 7 8 15 8"/>
    </svg> Sauvegarder`;
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
        <td><input type="text" class="inline-input tech-name-input" value="" placeholder="NOM Prénom" autofocus></td>
        <td><input type="tel" class="inline-input tech-num-input" value="" placeholder="+33612345678"></td>
        <td style="text-align:center">
            <button class="btn-delete" onclick="removeTechRow(this)" title="Supprimer">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="3 6 5 6 21 6"/>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                </svg>
            </button>
        </td>
    `;
    body.appendChild(tr);
    tr.querySelector('.tech-name-input').focus();
    updateTechCount();
    tr.style.animation = 'fadeIn 0.25s ease';
}

function removeTechRow(btn) {
    const row = btn.closest('tr');
    row.style.opacity = '0';
    row.style.transform = 'translateX(20px)';
    row.style.transition = 'all 0.2s ease';
    setTimeout(() => {
        row.remove();
        updateTechCount();
        // Show empty state if no rows left
        const body = document.getElementById('techConfigBody');
        if (!body.querySelectorAll('tr[data-idx]').length) {
            body.innerHTML = '<tr><td colspan="3" class="empty-state small">Aucun technicien — cliquez "Ajouter"</td></tr>';
        }
    }, 200);
}

function updateTechCount() {
    const count = document.querySelectorAll('#techConfigBody tr[data-idx]').length;
    document.getElementById('techCount').textContent = `${count} technicien${count !== 1 ? 's' : ''}`;
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
                <div class="fp-name">${esc(f.name)}</div>
                <div class="fp-meta">${f.modified} · ${formatSize(f.size)}</div>
            </div>
            ${f.active ? '<span class="fp-check">✓</span>' : ''}
        </div>
    `).join('');
}

function filterFileList(query) {
    const q = query.toLowerCase().trim();
    const filtered = q ? filePanelData.filter(f => f.name.toLowerCase().includes(q)) : filePanelData;
    renderFilePanelList(sortFiles(filtered, fileSortMode));
}

function sortFileList(mode, btn) {
    fileSortMode = mode;
    document.querySelectorAll('.sort-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    const q = document.getElementById('filePanelSearch').value.toLowerCase().trim();
    const filtered = q ? filePanelData.filter(f => f.name.toLowerCase().includes(q)) : filePanelData;
    renderFilePanelList(sortFiles(filtered, mode));
}

function sortFiles(files, mode) {
    const copy = [...files];
    if (mode === 'name') copy.sort((a, b) => a.name.localeCompare(b.name));
    else if (mode === 'date') copy.sort((a, b) => (b.modified || '').localeCompare(a.modified || ''));
    else if (mode === 'size') copy.sort((a, b) => (b.size || 0) - (a.size || 0));
    return copy;
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
    const phone = cfg.MY_NUMBER;

    if (!phone) {
        showToast('Configurez votre numéro dans les paramètres', 'error');
        return;
    }

    try {
        const resp = await fetch('/api/whatsapp/send', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                to: phone.replace('+', ''),
                message: '✅ Test Technosmart — la connexion WhatsApp fonctionne !'
            })
        });
        const data = await resp.json();
        if (data.status === 'sent') {
            showToast('Message test envoyé !', 'success');
        } else {
            showToast(`Erreur: ${data.error || 'inconnu'}`, 'error');
        }
    } catch (err) {
        showToast('Erreur d\'envoi', 'error');
    }
}

async function logoutWA() {
    if (!confirm('Voulez-vous vraiment déconnecter WhatsApp ?')) return;

    try {
        await fetch('/api/whatsapp/logout', { method: 'POST' });
        showToast('WhatsApp déconnecté', 'info');
        checkWAStatus();
    } catch (err) {
        showToast('Erreur de déconnexion', 'error');
    }
}

