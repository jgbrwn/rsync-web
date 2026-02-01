// State
let currentWs = null;
let currentJobId = null;
let browserTarget = null;
let browserCurrentPath = '.';
let sshHosts = [];
let sshTarget = null;

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    loadStatus();
    loadHistory();
    loadSSHHosts();
    updateCommandPreview();
    
    // Update preview on input changes
    document.querySelectorAll('.rsync-opt, #source, #destination, #excludes, #custom-opts').forEach(el => {
        el.addEventListener('change', updateCommandPreview);
        el.addEventListener('input', updateCommandPreview);
    });
    
    // Poll for job updates
    setInterval(loadHistory, 5000);
});

async function loadStatus() {
    try {
        const res = await fetch('/api/status');
        const data = await res.json();
        
        document.getElementById('workdir').textContent = `Working directory: ${data.work_dir}`;
        
        if (data.rsync_available) {
            document.getElementById('status-badge').innerHTML = 
                `<span class="bg-green-100 text-green-800 px-2 py-1 rounded">✓ ${data.rsync_version}</span>`;
            document.getElementById('rsync-warning').classList.add('hidden');
        } else {
            document.getElementById('status-badge').innerHTML = 
                `<span class="bg-red-100 text-red-800 px-2 py-1 rounded">✗ rsync not found</span>`;
            document.getElementById('rsync-warning').classList.remove('hidden');
            document.getElementById('run-btn').disabled = true;
        }
    } catch (e) {
        console.error('Failed to load status:', e);
    }
}

async function loadSSHHosts() {
    try {
        const res = await fetch('/api/ssh-hosts');
        sshHosts = await res.json();
    } catch (e) {
        console.error('Failed to load SSH hosts:', e);
    }
}

async function loadHistory() {
    try {
        const res = await fetch('/api/history');
        const history = await res.json();
        
        // Separate running and completed jobs
        const running = history.filter(h => h.status === 'running' || h.status === 'pending');
        const completed = history.filter(h => h.status !== 'running' && h.status !== 'pending');
        
        // Update jobs list
        const jobsList = document.getElementById('jobs-list');
        if (running.length === 0) {
            jobsList.innerHTML = '<div class="text-gray-500 text-sm">No running jobs</div>';
        } else {
            jobsList.innerHTML = running.map(job => `
                <div class="flex items-center justify-between p-2 bg-gray-50 rounded cursor-pointer hover:bg-gray-100"
                     onclick="viewJob(${job.id})">
                    <div class="flex-1 truncate">
                        <span class="text-sm font-mono">${escapeHtml(job.full_command.substring(0, 50))}...</span>
                    </div>
                    <span class="status-${job.status} text-sm ml-2">● ${job.status}</span>
                </div>
            `).join('');
        }
        
        // Update history list
        const historyList = document.getElementById('history-list');
        if (completed.length === 0) {
            historyList.innerHTML = '<div class="text-gray-500 text-sm">No history yet</div>';
        } else {
            historyList.innerHTML = completed.slice(0, 20).map(job => `
                <div class="flex items-center justify-between p-2 bg-gray-50 rounded">
                    <div class="flex-1 truncate cursor-pointer hover:text-blue-500" onclick="viewJob(${job.id})">
                        <span class="text-sm font-mono">${escapeHtml(job.full_command.substring(0, 40))}...</span>
                        <span class="text-xs text-gray-500 block">${new Date(job.created_at).toLocaleString()}</span>
                    </div>
                    <div class="flex items-center gap-2">
                        <span class="status-${job.status} text-sm">●</span>
                        <button onclick="reuseCommand(${job.id})" class="text-blue-500 hover:text-blue-700 text-sm">↻</button>
                        <button onclick="deleteHistory(${job.id})" class="text-red-500 hover:text-red-700 text-sm">✕</button>
                    </div>
                </div>
            `).join('');
        }
    } catch (e) {
        console.error('Failed to load history:', e);
    }
}

function getSelectedOptions() {
    const opts = [];
    document.querySelectorAll('.rsync-opt:checked').forEach(el => {
        opts.push(el.dataset.opt);
    });
    
    // Handle excludes
    const excludes = document.getElementById('excludes').value.trim();
    if (excludes) {
        excludes.split('\n').forEach(ex => {
            if (ex.trim()) opts.push(`--exclude=${ex.trim()}`);
        });
    }
    
    // Handle custom options
    const custom = document.getElementById('custom-opts').value.trim();
    if (custom) {
        opts.push(...custom.split(/\s+/));
    }
    
    return opts;
}

function updateCommandPreview() {
    const source = document.getElementById('source').value || '<source>';
    const dest = document.getElementById('destination').value || '<destination>';
    const opts = getSelectedOptions();
    
    const cmd = `rsync ${opts.join(' ')} ${source} ${dest}`;
    document.getElementById('command-preview').textContent = cmd;
}

async function runCommand() {
    const source = document.getElementById('source').value;
    const dest = document.getElementById('destination').value;
    
    if (!source || !dest) {
        alert('Please specify source and destination');
        return;
    }
    
    const opts = getSelectedOptions();
    
    try {
        const res = await fetch('/api/run', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ source, destination: dest, options: opts })
        });
        
        if (!res.ok) {
            const text = await res.text();
            alert(`Error: ${text}`);
            return;
        }
        
        const job = await res.json();
        viewJob(job.id);
        loadHistory();
    } catch (e) {
        alert(`Error: ${e.message}`);
    }
}

function viewJob(id) {
    currentJobId = id;
    
    // Close existing WebSocket
    if (currentWs) {
        currentWs.close();
    }
    
    // Clear output
    const output = document.getElementById('output');
    output.innerHTML = '';
    document.getElementById('current-job-label').textContent = `Job #${id}`;
    
    // Connect WebSocket
    const wsProtocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    currentWs = new WebSocket(`${wsProtocol}//${location.host}/ws/job/${id}`);
    
    currentWs.onmessage = (event) => {
        const msg = JSON.parse(event.data);
        
        if (msg.type === 'output') {
            const line = document.createElement('div');
            line.textContent = msg.data;
            output.appendChild(line);
            output.scrollTop = output.scrollHeight;
        } else if (msg.type === 'done') {
            const line = document.createElement('div');
            line.className = `mt-2 pt-2 border-t border-gray-700 status-${msg.status}`;
            line.textContent = `\n--- Job ${msg.status} ---`;
            output.appendChild(line);
            document.getElementById('cancel-btn').classList.add('hidden');
            loadHistory();
        }
    };
    
    currentWs.onopen = () => {
        document.getElementById('cancel-btn').classList.remove('hidden');
    };
    
    currentWs.onerror = () => {
        document.getElementById('cancel-btn').classList.add('hidden');
    };
}

async function cancelJob() {
    if (!currentJobId) return;
    
    try {
        await fetch(`/api/cancel/${currentJobId}`, { method: 'POST' });
        loadHistory();
    } catch (e) {
        console.error('Failed to cancel:', e);
    }
}

async function reuseCommand(id) {
    try {
        const res = await fetch(`/api/job/${id}`);
        const data = await res.json();
        const job = data.history;
        
        document.getElementById('source').value = job.source;
        document.getElementById('destination').value = job.destination;
        
        // Parse and set options
        const opts = JSON.parse(job.options);
        document.querySelectorAll('.rsync-opt').forEach(el => el.checked = false);
        
        const excludes = [];
        const custom = [];
        
        opts.forEach(opt => {
            const checkbox = document.querySelector(`.rsync-opt[data-opt="${opt}"]`);
            if (checkbox) {
                checkbox.checked = true;
            } else if (opt.startsWith('--exclude=')) {
                excludes.push(opt.replace('--exclude=', ''));
            } else {
                custom.push(opt);
            }
        });
        
        document.getElementById('excludes').value = excludes.join('\n');
        document.getElementById('custom-opts').value = custom.join(' ');
        
        updateCommandPreview();
    } catch (e) {
        console.error('Failed to load job:', e);
    }
}

async function deleteHistory(id) {
    if (!confirm('Delete this history entry?')) return;
    
    try {
        await fetch(`/api/history/${id}`, { method: 'DELETE' });
        loadHistory();
    } catch (e) {
        console.error('Failed to delete:', e);
    }
}

function copyCommand() {
    const cmd = document.getElementById('command-preview').textContent;
    navigator.clipboard.writeText(cmd);
}

function toggleAdvanced() {
    const el = document.getElementById('advanced-options');
    el.classList.toggle('hidden');
}

// File Browser
function openBrowser(target) {
    browserTarget = target;
    browserCurrentPath = '.';
    document.getElementById('browser-modal').classList.remove('hidden');
    loadBrowserPath('.');
}

function closeBrowser() {
    document.getElementById('browser-modal').classList.add('hidden');
}

async function loadBrowserPath(path) {
    try {
        const res = await fetch(`/api/browse?path=${encodeURIComponent(path)}`);
        const data = await res.json();
        
        browserCurrentPath = data.current_path;
        document.getElementById('browser-path').textContent = data.full_path;
        
        const entries = document.getElementById('browser-entries');
        entries.innerHTML = data.entries.map(entry => `
            <div class="file-entry flex items-center p-2 cursor-pointer rounded"
                 onclick="${entry.is_dir ? `loadBrowserPath('${entry.path}')` : `selectFile('${entry.path}')`}">
                <span class="mr-2">${entry.is_dir ? '📁' : '📄'}</span>
                <span class="flex-1">${escapeHtml(entry.name)}</span>
                ${!entry.is_dir ? `<span class="text-sm text-gray-500">${formatSize(entry.size)}</span>` : ''}
            </div>
        `).join('');
    } catch (e) {
        console.error('Failed to browse:', e);
    }
}

function selectFile(path) {
    document.getElementById(browserTarget).value = path;
    closeBrowser();
    updateCommandPreview();
}

function selectCurrentPath() {
    document.getElementById(browserTarget).value = browserCurrentPath;
    closeBrowser();
    updateCommandPreview();
}

// SSH Host Picker
function openHostPicker(target) {
    sshTarget = target;
    document.getElementById('ssh-modal').classList.remove('hidden');
    
    const list = document.getElementById('ssh-hosts-list');
    if (sshHosts.length === 0) {
        list.innerHTML = '<div class="text-gray-500 text-sm">No SSH hosts found in ~/.ssh/config</div>';
    } else {
        list.innerHTML = sshHosts.map(host => `
            <div class="p-2 bg-gray-50 rounded cursor-pointer hover:bg-gray-100"
                 onclick="selectHost('${host.name}')">
                <div class="font-medium">${escapeHtml(host.name)}</div>
                <div class="text-sm text-gray-500">
                    ${host.user ? host.user + '@' : ''}${host.hostname || host.name}${host.port ? ':' + host.port : ''}
                </div>
            </div>
        `).join('');
    }
}

function closeHostPicker() {
    document.getElementById('ssh-modal').classList.add('hidden');
}

function selectHost(name) {
    const remotePath = document.getElementById('ssh-remote-path').value || '~/';
    document.getElementById(sshTarget).value = `${name}:${remotePath}`;
    closeHostPicker();
    updateCommandPreview();
}

// Utilities
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function formatSize(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}
