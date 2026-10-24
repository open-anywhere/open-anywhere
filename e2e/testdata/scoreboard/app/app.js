'use strict';
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const port = Number(process.env.PORT);
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  console.error('PORT must be an integer from 1 to 65535');
  process.exit(1);
}
const host = process.env.HOST || '0.0.0.0';
const initial = () => ({ red: 0, blue: 0, round: 1, last_team: null });
let state = initial();
const clients = new Set();
const json = (res, code, body) => {
  res.writeHead(code, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
  res.end(JSON.stringify(body));
};
const broadcast = () => {
  const event = `data: ${JSON.stringify(state)}\n\n`;
  for (const res of clients) res.write(event);
};
const staticFiles = new Map([
  ['/', ['index.html', 'text/html; charset=utf-8']],
  ['/styles.css', ['styles.css', 'text/css']],
  ['/ui.js', ['ui.js', 'text/javascript']]
]);
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://localhost');
  if (req.method === 'GET' && url.pathname === '/health') return json(res, 200, { status: 'ok' });
  if (req.method === 'GET' && staticFiles.has(url.pathname)) {
    const [name, contentType] = staticFiles.get(url.pathname);
    res.writeHead(200, { 'Content-Type': contentType, 'Cache-Control': 'no-store' });
    return res.end(fs.readFileSync(path.join(__dirname, 'public', name)));
  }
  if (req.method === 'GET' && url.pathname === '/events') {
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store', Connection: 'keep-alive' });
    clients.add(res);
    res.write(`data: ${JSON.stringify(state)}\n\n`);
    res.on('close', () => clients.delete(res));
    return;
  }
  if (req.method === 'GET' && url.pathname === '/api/state') return json(res, 200, { message: '当前比分', ...state });
  if (req.method === 'POST' && ['/api/score', '/api/reset'].includes(url.pathname)) {
    if (!(req.headers['content-type'] || '').startsWith('application/json')) return json(res, 415, { message: '请发送 JSON' });
    let body = '';
    try {
      for await (const chunk of req) {
        body += chunk;
        if (Buffer.byteLength(body) > 4096) return json(res, 413, { message: '请求过大' });
      }
      body = JSON.parse(body);
    } catch { return json(res, 400, { message: 'JSON 格式错误' }); }
    if (!body || typeof body !== 'object' || Array.isArray(body)) return json(res, 400, { message: '请求须为对象' });
    if (url.pathname === '/api/reset') state = initial();
    else {
      if (!['red', 'blue'].includes(body.team_id) || !Number.isInteger(body.points) || body.points < 1 || body.points > 10) {
        return json(res, 400, { message: 'team_id 为 red 或 blue；points 为 1 至 10 的整数' });
      }
      state[body.team_id] += body.points;
      state.last_team = body.team_id;
      state.round += 1;
    }
    broadcast();
    return json(res, 200, { message: url.pathname === '/api/reset' ? '比分已重置' : '加分成功', ...state });
  }
  json(res, 404, { message: '接口不存在' });
});
const heartbeat = setInterval(() => { for (const res of clients) res.write(': heartbeat\n\n'); }, 1000);
server.on('error', err => { console.error(err); process.exit(1); });
server.listen(port, host, () => console.log(`scoreboard listening on ${host}:${port}`));
process.on('SIGTERM', () => {
  clearInterval(heartbeat);
  for (const res of clients) res.end();
  server.close(() => process.exit(0));
  server.closeIdleConnections();
});
