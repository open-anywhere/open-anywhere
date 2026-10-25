'use strict';

const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');

// 构建消息提供 player_names 时，按原顺序替换为名字，例如：
// const INITIAL_PLAYERS = ['韩鲁鹏', '邓军辉'];
// 没有名单时保持空数组，让玩家在运行期通过 /api/join 加入。
const INITIAL_PLAYERS = [];

// 示例片库。实际构建时按需求替换，也可以从本地 JSON 文件读取。
const MOVIES = [
  {
    title: '霸王别姬',
    clues: [
      '两位京剧演员跨越数十年的爱恨与命运。',
      '张国荣、张丰毅、巩俐主演。',
      '1993 年上映，陈凯歌执导。',
    ],
  },
  {
    title: '让子弹飞',
    clues: [
      '民国年间，一列火车被劫，县长走马上任。',
      '姜文、葛优、周润发同台。',
      '2010 年上映的黑色喜剧。',
    ],
  },
  {
    title: '流浪地球',
    clues: [
      '太阳即将毁灭，人类决定带着地球一起离开。',
      '行星发动机、地下城和木星引力危机。',
      '2019 年上映的中国科幻电影。',
    ],
  },
];

const RECENT_LIMIT = 5;

const state = {
  players: [],
  round: 0,
  recent: [],
};

for (const rawName of INITIAL_PLAYERS) {
  const name = normalizeName(rawName);
  if (name && !findPlayer(name)) {
    state.players.push({ user_name: name, score: 0 });
  }
}

function normalizeName(value) {
  return typeof value === 'string' ? value.trim() : '';
}

function normalizeGuess(value) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[\s《》〈〉“”"'‘’·:：!！?？,，.。、\-—_()（）]/g, '');
}

function findPlayer(name) {
  return state.players.find((player) => player.user_name === name) || null;
}

function currentMovie() {
  return MOVIES[state.round % MOVIES.length];
}

function publicState(message) {
  const movie = currentMovie();
  return {
    message: message || `第 ${state.round + 1} 轮：根据线索猜电影名`,
    round: state.round + 1,
    clues: movie.clues.slice(),
    players: state.players.map((player) => ({
      user_name: player.user_name,
      score: player.score,
    })),
    recent: state.recent.slice(),
  };
}

function send(res, status, body) {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    'content-type': 'application/json; charset=utf-8',
    'content-length': Buffer.byteLength(payload),
  });
  res.end(payload);
}

function readJSON(req, limit = 64 * 1024) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on('data', (chunk) => {
      size += chunk.length;
      if (size > limit) {
        reject(Object.assign(new Error('请求体过大'), { status: 413 }));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on('end', () => {
      if (chunks.length === 0) {
        resolve({});
        return;
      }
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString('utf8')));
      } catch {
        reject(Object.assign(new Error('请求体不是合法 JSON'), { status: 400 }));
      }
    });
    req.on('error', reject);
  });
}

async function readObject(req) {
  const body = await readJSON(req);
  if (!body || typeof body !== 'object' || Array.isArray(body)) {
    throw Object.assign(new Error('请求体必须是 JSON 对象'), { status: 400 });
  }
  return body;
}

function serveIndex(res) {
  fs.readFile(path.join(__dirname, 'public', 'index.html'), (err, data) => {
    if (err) {
      res.writeHead(500, { 'content-type': 'text/plain; charset=utf-8' });
      res.end('页面加载失败');
      return;
    }
    res.writeHead(200, {
      'content-type': 'text/html; charset=utf-8',
      'content-length': data.length,
    });
    res.end(data);
  });
}

function joinGame(body) {
  const name = normalizeName(body.user_name);
  if (!name) {
    return { status: 400, body: { message: 'user_name 不能为空' } };
  }
  if (findPlayer(name)) {
    return { status: 200, body: publicState(`${name} 已在名单中`) };
  }
  state.players.push({ user_name: name, score: 0 });
  return { status: 200, body: publicState(`${name} 加入游戏`) };
}

function submitGuess(body) {
  const name = normalizeName(body.user_name);
  const guess = normalizeName(body.guess);
  if (!name || !guess) {
    return { status: 400, body: { message: 'user_name 和 guess 都不能为空' } };
  }
  const player = findPlayer(name);
  if (!player) {
    return { status: 400, body: { message: '玩家不存在，请先加入' } };
  }

  const movie = currentMovie();
  const round = state.round + 1;
  const correct = normalizeGuess(guess) === normalizeGuess(movie.title);
  state.recent.unshift({ round, user_name: name, guess, correct });
  state.recent = state.recent.slice(0, RECENT_LIMIT);

  if (!correct) {
    return {
      status: 200,
      body: { ...publicState(`${name} 猜的是「${guess}」，不对，再想想`), correct: false },
    };
  }

  player.score += 1;
  state.round += 1;
  return {
    status: 200,
    body: {
      ...publicState(`${name} 猜对了，答案是《${movie.title}》。第 ${state.round + 1} 轮开始`),
      correct: true,
      answer: movie.title,
    },
  };
}

async function route(req, res, url) {
  if (req.method === 'GET' && url.pathname === '/health') {
    send(res, 200, { status: 'ok' });
    return;
  }
  if (req.method === 'GET' && (url.pathname === '/' || url.pathname === '/index.html')) {
    serveIndex(res);
    return;
  }
  if (req.method === 'GET' && url.pathname === '/api/state') {
    send(res, 200, publicState());
    return;
  }
  if (req.method === 'POST' && url.pathname === '/api/join') {
    const result = joinGame(await readObject(req));
    send(res, result.status, result.body);
    return;
  }
  if (req.method === 'POST' && url.pathname === '/api/guess') {
    const result = submitGuess(await readObject(req));
    send(res, result.status, result.body);
    return;
  }
  send(res, 404, { message: '接口不存在' });
}

const rawPort = process.env.PORT || '';
const port = /^\d+$/.test(rawPort) ? Number(rawPort) : NaN;
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  console.error('PORT 缺失或非法：需要 1-65535 的整数');
  process.exit(1);
}
const host = process.env.HOST || '0.0.0.0';

const server = http.createServer((req, res) => {
  let url;
  try {
    url = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
  } catch {
    send(res, 400, { message: '请求地址无效' });
    return;
  }
  route(req, res, url).catch((err) => {
    const status = err.status || 500;
    send(res, status, { message: status === 500 ? '服务器内部错误' : err.message });
  });
});

server.on('error', (err) => {
  console.error(`启动失败：${err.message}`);
  process.exit(1);
});

server.listen(port, host, () => {
  console.log(`listening on http://${host}:${port}`);
});

function shutdown() {
  server.close(() => process.exit(0));
  setTimeout(() => process.exit(1), 5000).unref();
}

process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);
