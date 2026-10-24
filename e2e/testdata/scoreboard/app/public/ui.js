'use strict';
const status = document.querySelector('#status');
function draw(state) {
  document.querySelector('#red-score').textContent = String(state.red).padStart(2, '0');
  document.querySelector('#blue-score').textContent = String(state.blue).padStart(2, '0');
  document.querySelector('#round').textContent = `ROUND ${String(state.round).padStart(2, '0')}`;
  document.querySelector('#state-color').style.backgroundColor = state.last_team === 'red' ? 'rgb(232,58,58)' : state.last_team === 'blue' ? 'rgb(36,91,232)' : 'rgb(128,128,128)';
  document.querySelector('#last-team').textContent = state.last_team === 'red' ? '红队拿下一分' : state.last_team === 'blue' ? '蓝队拿下一分' : '等待第一分';
}
const events = new EventSource('/events');
events.onmessage = event => { draw(JSON.parse(event.data)); status.textContent = 'LIVE'; };
events.onerror = () => { status.textContent = '重新连接中'; };
async function send(route, body) {
  try {
    const response = await fetch(route, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const data = await response.json();
    if (!response.ok) throw new Error(data.message);
    draw(data);
  } catch (error) { status.textContent = error.message; }
}
for (const button of document.querySelectorAll('[data-team]')) button.addEventListener('click', () => send('/api/score', { team_id: button.dataset.team, points: 1 }));
document.querySelector('#reset').addEventListener('click', () => send('/api/reset', {}));
