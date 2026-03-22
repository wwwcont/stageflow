document.addEventListener('click', (event) => {
  const button = event.target.closest('[data-copy-target]');
  if (!button) return;
  const target = document.querySelector(button.dataset.copyTarget);
  if (!target) return;
  navigator.clipboard.writeText(target.innerText || target.textContent || '');
});

document.addEventListener('DOMContentLoaded', () => {
  document.querySelectorAll('[data-run-events-panel]').forEach(initRunEventsPanel);
  document.querySelectorAll('[data-extraction-editor]').forEach(initExtractionEditor);
});

function initRunEventsPanel(panel) {
  const list = panel.querySelector('[data-log-list]');
  const statusNode = panel.querySelector('[data-log-connection-status]');
  const filterNode = panel.querySelector('[data-log-filter]');
  const autoScrollToggle = panel.querySelector('[data-log-autoscroll-toggle]');
  const clearButton = panel.querySelector('[data-log-clear]');
  const emptyState = () => panel.querySelector('[data-log-empty-state]');
  let autoScroll = true;
  let source = null;
  let streamCompleted = panel.dataset.streamState === 'completed';

  function setStatus(status) {
    statusNode.textContent = status;
    statusNode.className = `live-log-status status-${status}`;
  }

  function currentLastSequence() {
    return Number(list.dataset.lastSequence || '0');
  }

  function setLastSequence(sequence) {
    list.dataset.lastSequence = String(sequence);
  }

  function applyFilter() {
    const level = filterNode.value;
    list.querySelectorAll('[data-log-entry]').forEach((entry) => {
      const visible = level === 'all' || entry.dataset.level === level;
      entry.style.display = visible ? '' : 'none';
    });
  }

  function maybeScroll() {
    if (!autoScroll) return;
    list.scrollTop = list.scrollHeight;
  }

  function renderDetails(details) {
    if (!details || details === '{}' || details === 'null') return '';
    return `<details class="live-log-details"><summary>Details</summary><pre>${escapeHTML(details)}</pre></details>`;
  }

  function appendEvent(item) {
    if (emptyState()) emptyState().remove();
    const article = document.createElement('article');
    article.className = 'live-log-entry';
    article.dataset.logEntry = 'true';
    article.dataset.level = item.level;
    article.dataset.sequence = String(item.sequence);
    article.innerHTML = `
      <div class="live-log-meta">
        <span class="muted">${escapeHTML(item.timestamp)}</span>
        <span class="log-badge level-${escapeHTML(item.level)}">${escapeHTML(item.level)}</span>
        <span class="log-badge type">${escapeHTML(item.eventType)}</span>
        <span class="log-badge step">${escapeHTML(item.stepLabel)}</span>
      </div>
      <div class="live-log-message">${escapeHTML(item.message)}</div>
      ${item.hasDetails ? renderDetails(item.details) : ''}
    `;
    list.appendChild(article);
    setLastSequence(item.sequence);
    applyFilter();
    maybeScroll();
    if (item.eventType === 'run_finished' || item.eventType === 'run_failed') {
      streamCompleted = true;
      setStatus('completed');
      if (source) {
        source.close();
      }
    }
  }

  function connect() {
    if (streamCompleted) {
      setStatus('completed');
      return;
    }
    const url = new URL(panel.dataset.streamUrl, window.location.origin);
    url.searchParams.set('after', String(currentLastSequence()));
    setStatus('connecting');
    source = new EventSource(url.toString());
    source.addEventListener('open', () => setStatus('live'));
    source.addEventListener('error', () => {
      if (streamCompleted) {
        setStatus('completed');
        return;
      }
      setStatus('reconnecting');
    });
    source.addEventListener('run_event', (event) => {
      const item = JSON.parse(event.data);
      appendEvent(item);
    });
  }

  filterNode.addEventListener('change', applyFilter);
  autoScrollToggle.addEventListener('click', () => {
    autoScroll = !autoScroll;
    autoScrollToggle.textContent = autoScroll ? 'Pause auto-scroll' : 'Resume auto-scroll';
    if (autoScroll) maybeScroll();
  });
  clearButton.addEventListener('click', () => {
    list.innerHTML = '<div class="muted" data-log-empty-state>View cleared. New events will continue to appear here.</div>';
  });
  list.addEventListener('scroll', () => {
    const nearBottom = list.scrollTop + list.clientHeight >= list.scrollHeight - 24;
    if (!nearBottom) autoScroll = false;
  });

  applyFilter();
  maybeScroll();
  connect();
}

function escapeHTML(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function initExtractionEditor(section) {
  const list = section.querySelector('[data-extraction-rule-list]');
  const template = section.querySelector('[data-extraction-rule-template]');
  const addButton = section.querySelector('[data-add-extraction-rule]');
  if (!list || !template || !addButton) return;

  function ensureAtLeastOneRow() {
    if (list.querySelector('[data-extraction-rule-row]')) return;
    addRow();
  }

  function addRow() {
    const fragment = template.content.cloneNode(true);
    list.appendChild(fragment);
  }

  addButton.addEventListener('click', () => addRow());

  section.addEventListener('click', (event) => {
    const removeButton = event.target.closest('[data-remove-extraction-rule]');
    if (!removeButton) return;
    const row = removeButton.closest('[data-extraction-rule-row]');
    if (!row) return;
    row.remove();
    ensureAtLeastOneRow();
  });

  ensureAtLeastOneRow();
}
