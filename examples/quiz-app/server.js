const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const { URL } = require('node:url');

const PORT = Number(process.env.PORT || 38120);
const HOST = process.env.HOST || '127.0.0.1';
const SESSION_ID = 'quiz-session-001';
const ROOT = __dirname;
const PUBLIC_DIR = path.join(ROOT, 'public');

const QUESTIONS = [
  { id: 'q1', text: '世界上最早的电影是什么？', answer: '火车进站', accepted: ['火车进站', '《火车进站》'] },
  { id: 'q2', text: '《星夜》的作者是谁？', answer: '梵高', accepted: ['梵高', '文森特·梵高', '文森特梵高'] },
  { id: 'q3', text: '中国的首都是哪里？', answer: '北京', accepted: ['北京'] },
  { id: 'q4', text: '《百年孤独》的作者是谁？', answer: '马尔克斯', accepted: ['马尔克斯', '加西亚·马尔克斯'] },
  { id: 'q5', text: '光的速度约为每秒多少公里？', answer: '三十万公里', accepted: ['三十万公里', '30万公里', '300000公里'] }
];

function createInitialState() {
  return {
    session_id: SESSION_ID,
    title: '今晚的知识竞赛',
    phase: 'preparing',
    current_question_number: 0,
    total_questions: QUESTIONS.length,
    players: [
      { player_id: 'p1', user_id: 'u1', display_name: '张之阳', registered: true, ready: false, score: 0 },
      { player_id: 'p2', user_id: 'u2', display_name: '韩鲁鹏', registered: true, ready: false, score: 0 },
      { player_id: 'p3', user_id: 'u3', display_name: '邓军辉', registered: true, ready: false, score: 0 }
    ],
    current_question: null,
    pending_answer: null,
    last_result: null
  };
}

let state = createInitialState();
let answerSequence = 0;
const sseClients = new Set();

function publicState() {
  return JSON.parse(JSON.stringify(state));
}

function questionForClient(question) {
  if (!question) return null;
  return { id: question.id, text: question.text, options: [] };
}

function judgeContext(question, extra = {}) {
  return {
    expected_answer: question.answer,
    accepted_answers: question.accepted,
    judging_instruction: '玩家回答前不要透露标准答案。',
    ...extra
  };
}

function response(message, userMessage, nextAction = { type: 'none' }, extra = {}) {
  return {
    ok: true,
    message,
    user_message: userMessage,
    next_action: nextAction,
    state: publicState(),
    ...extra
  };
}

function broadcastState() {
  const payload = `event: state\ndata: ${JSON.stringify(publicState())}\n\n`;
  for (const client of sseClients) client.write(payload);
}

function sendJson(res, status, data) {
  const body = JSON.stringify(data, null, 2);
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(body),
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Headers': 'Content-Type',
    'Access-Control-Allow-Methods': 'GET,POST,OPTIONS'
  });
  res.end(body);
}

function sendText(res, status, body, contentType) {
  res.writeHead(status, {
    'Content-Type': contentType,
    'Content-Length': Buffer.byteLength(body),
    'Cache-Control': 'no-cache'
  });
  res.end(body);
}

function readJson(req) {
  return new Promise((resolve, reject) => {
    let raw = '';
    req.on('data', chunk => {
      raw += chunk;
      if (raw.length > 1024 * 1024) req.destroy(new Error('request body too large'));
    });
    req.on('end', () => {
      if (!raw.trim()) return resolve({});
      try { resolve(JSON.parse(raw)); } catch { reject(new Error('请求体必须是合法 JSON')); }
    });
    req.on('error', reject);
  });
}

function notFound(res, message = '资源不存在') {
  sendJson(res, 404, { ok: false, message });
}

function badRequest(res, message) {
  sendJson(res, 400, { ok: false, message });
}

function conflict(res, message) {
  sendJson(res, 409, { ok: false, message, state: publicState() });
}

function handleApi(method, pathname, body, res) {
  const prefix = `/api/sessions/${SESSION_ID}`;
  if (pathname === `${prefix}/state` && method === 'GET') {
    return sendJson(res, 200, response('已读取当前游戏状态。', '当前游戏状态已更新。'));
  }

  if (pathname === `${prefix}/players` && method === 'POST') {
    if (!body.user_id || !body.display_name) return badRequest(res, '缺少 user_id 或 display_name。');
    const existing = state.players.find(player => player.user_id === body.user_id);
    if (existing) existing.display_name = body.display_name;
    else state.players.push({ player_id: `p${state.players.length + 1}`, user_id: body.user_id, display_name: body.display_name, registered: true, ready: false, score: 0 });
    broadcastState();
    return sendJson(res, 200, response(`${body.display_name} 已加入玩家列表。`, `${body.display_name} 已加入。`, { type: 'speak', text: `${body.display_name} 已加入。` }));
  }

  const readyMatch = pathname.match(new RegExp(`^${prefix}/players/([^/]+)/ready$`));
  if (readyMatch && method === 'POST') {
    const player = state.players.find(item => item.player_id === readyMatch[1]);
    if (!player) return notFound(res, '找不到玩家。');
    player.ready = true;
    const missing = state.players.filter(item => !item.ready).map(item => item.display_name);
    state.phase = missing.length ? 'preparing' : 'waiting_start';
    const message = missing.length ? `${player.display_name} 已准备好，还差：${missing.join('、')}。` : '三位玩家已经全部就位，可以开始游戏。';
    const spoken = missing.length ? `${player.display_name} 已准备好，还差 ${missing.join('、')}。` : '玩家已全部就位，请说开始吧。';
    broadcastState();
    return sendJson(res, 200, response(message, spoken, { type: 'speak', text: spoken }));
  }

  if (pathname === `${prefix}/start` && method === 'POST') {
    if (state.players.some(player => !player.ready)) return conflict(res, '仍有玩家未就位，不能开始游戏。');
    state.phase = 'waiting_answer';
    state.current_question_number = 1;
    state.current_question = QUESTIONS[0];
    state.pending_answer = null;
    const question = state.current_question;
    const spoken = `第一题：${question.text}`;
    broadcastState();
    return sendJson(res, 200, response('游戏已开始，第一题已经准备好。', spoken, { type: 'speak', text: spoken }, {
      question: questionForClient(question),
      judge_context: judgeContext(question)
    }));
  }

  if (pathname === `${prefix}/answers` && method === 'POST') {
    if (state.phase !== 'waiting_answer' || !state.current_question) return conflict(res, '当前不在等待回答阶段。');
    if (!body.user_id || !body.transcript) return badRequest(res, '缺少 user_id 或 transcript。');
    const player = state.players.find(item => item.user_id === body.user_id);
    if (!player) return notFound(res, 'user_id 对应的玩家不存在。');
    const answerId = `answer-${String(++answerSequence).padStart(3, '0')}`;
    state.pending_answer = { answer_id: answerId, user_id: player.user_id, display_name: player.display_name, transcript: body.transcript };
    state.phase = 'evaluating';
    broadcastState();
    return sendJson(res, 200, response(`已收到${player.display_name}的回答，等待判断。`, `已收到${player.display_name}的回答。`, { type: 'none' }, {
      answer_id: answerId,
      judge_context: judgeContext(state.current_question, { player: player.display_name, transcript: body.transcript })
    }));
  }

  const evaluateMatch = pathname.match(new RegExp(`^${prefix}/answers/([^/]+)/evaluate$`));
  if (evaluateMatch && method === 'POST') {
    if (!state.pending_answer || state.pending_answer.answer_id !== evaluateMatch[1]) return notFound(res, '找不到待判断的回答。');
    if (typeof body.answer_right !== 'boolean') return badRequest(res, 'answer_right 必须是 true 或 false。');
    const pending = state.pending_answer;
    const player = state.players.find(item => item.user_id === pending.user_id);
    if (body.answer_right) player.score += 1;
    state.last_result = { player: player.display_name, answer: pending.transcript, answer_right: body.answer_right, reason: body.reason || '' };
    const finished = state.current_question_number >= state.total_questions;
    let spoken = body.answer_right ? `${player.display_name} 回答正确，得 1 分。` : `${player.display_name} 回答错误。`;
    let extra = {};
    if (finished) {
      state.phase = 'finished';
      spoken += ' 本局结束，请查看最终排名。';
    } else {
      state.current_question_number += 1;
      state.current_question = QUESTIONS[state.current_question_number - 1];
      state.phase = 'waiting_answer';
      spoken += ` 下一题：${state.current_question.text}`;
      extra = { question: questionForClient(state.current_question), judge_context: judgeContext(state.current_question) };
    }
    state.pending_answer = null;
    broadcastState();
    return sendJson(res, 200, response(`${player.display_name}${body.answer_right ? '回答正确，获得 1 分' : '回答不正确'}，当前比分已更新。`, spoken, { type: 'speak', text: spoken }, extra));
  }

  if (pathname === `${prefix}/pause` && method === 'POST') {
    state.resume_phase = state.phase;
    state.phase = 'paused';
    broadcastState();
    return sendJson(res, 200, response('游戏已暂停，题目和计分不会推进。', '游戏已暂停。', { type: 'speak', text: '游戏已暂停。' }));
  }

  if (pathname === `${prefix}/resume` && method === 'POST') {
    state.phase = state.resume_phase || (state.current_question ? 'waiting_answer' : 'waiting_start');
    delete state.resume_phase;
    broadcastState();
    return sendJson(res, 200, response('游戏已继续。', '游戏继续。', { type: 'speak', text: '游戏继续。' }));
  }

  if (pathname === `${prefix}/reset` && method === 'POST') {
    state = createInitialState();
    answerSequence = 0;
    broadcastState();
    return sendJson(res, 200, response('游戏已重置，玩家需要重新准备。', '新的一局已准备，请大家就位。', { type: 'speak', text: '新的一局已准备，请大家就位。' }));
  }

  return notFound(res, `${method} ${pathname} 未实现`);
}

function serveStatic(pathname, res) {
  const file = pathname === '/' ? path.join(PUBLIC_DIR, 'index.html') : pathname === '/openapi.yaml' ? path.join(ROOT, 'openapi.yaml') : null;
  if (!file) return notFound(res);
  fs.readFile(file, (error, data) => {
    if (error) return notFound(res);
    const contentType = file.endsWith('.yaml') ? 'text/yaml; charset=utf-8' : 'text/html; charset=utf-8';
    sendText(res, 200, data, contentType);
  });
}

const server = http.createServer(async (req, res) => {
  const requestUrl = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
  const { pathname } = requestUrl;

  if (req.method === 'OPTIONS') return sendJson(res, 204, {});

  if (pathname === `/api/sessions/${SESSION_ID}/events` && req.method === 'GET') {
    res.writeHead(200, {
      'Content-Type': 'text/event-stream; charset=utf-8',
      'Cache-Control': 'no-cache',
      Connection: 'keep-alive',
      'Access-Control-Allow-Origin': '*'
    });
    res.write(`event: state\ndata: ${JSON.stringify(publicState())}\n\n`);
    sseClients.add(res);
    req.on('close', () => sseClients.delete(res));
    return;
  }

  if (pathname.startsWith('/api/')) {
    let body = {};
    try { if (req.method === 'POST') body = await readJson(req); } catch (error) { return badRequest(res, error.message); }
    return handleApi(req.method, pathname, body, res);
  }

  return serveStatic(pathname, res);
});

setInterval(() => {
  for (const client of sseClients) client.write(': heartbeat\n\n');
}, 15000).unref();

server.listen(PORT, HOST, () => {
  console.log(`Open Anywhere quiz app running at http://${HOST}:${PORT}`);
  console.log(`OpenAPI: http://${HOST}:${PORT}/openapi.yaml`);
});
