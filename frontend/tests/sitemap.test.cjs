// Run with: node --test tests/sitemap.test.cjs
// Execute the actual route handlers with isolated API fixtures; no database writes.
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const Module = require('node:module');
const path = require('node:path');
const { test } = require('node:test');
const ts = require('typescript');

const root = path.resolve(__dirname, '..');
const origin = 'https://forum.example.test';

function harness({ tags = 301, threads = 501, fail } = {}) {
  const calls = [];
  const cache = new Map();
  async function serverRequest(endpoint, options) {
    assert.equal(options.forwardCookies, false);
    const page = options.query?.page ?? 1;
    calls.push({ endpoint, page });
    if (fail?.(endpoint, page)) throw new Error('upstream unavailable');
    if (endpoint === '/api/v1/forums') return { data: [{ forums: [{ id: '1' }] }] };
    const isTag = endpoint === '/api/v1/tags';
    assert.ok(isTag || endpoint === '/api/v1/index/threads');
    const total = isTag ? tags : threads;
    const pageSize = isTag ? 30 : options.query.size;
    const start = (page - 1) * pageSize;
    const rows = Array.from({ length: Math.max(0, Math.min(pageSize, total - start)) }, (_, i) => {
      const id = String(start + i + 1);
      return isTag ? { id, slug: 'tag-' + id } : { id, updatedAt: '2026-09-29T00:00:00Z' };
    });
    return {
      data: isTag ? rows : { threads: rows },
      meta: { page, pageSize, total, totalPages: Math.ceil(total / pageSize) },
    };
  }

  function load(filename) {
    if (cache.has(filename)) return cache.get(filename).exports;
    const mod = new Module(filename, module);
    mod.filename = filename;
    mod.paths = Module._nodeModulePaths(path.dirname(filename));
    const nativeRequire = mod.require.bind(mod);
    mod.require = (name) => {
      if (name === '@/lib/api/server') return { serverRequest };
      if (name === '@/lib/site-url') return { SITE_URL: origin };
      if (name.startsWith('@/')) return load(path.join(root, 'src', name.slice(2)) + '.ts');
      return nativeRequire(name);
    };
    cache.set(filename, mod);
    const compiled = ts.transpileModule(readFileSync(filename, 'utf8'), {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
      fileName: filename,
    });
    mod._compile(compiled.outputText, filename);
    return mod.exports;
  }

  return {
    calls,
    index: load(path.join(root, 'src/app/sitemap.xml/route.ts')).GET,
    shard: (shard) => load(path.join(root, 'src/app/sitemap/[shard]/route.ts')).GET(
      new Request(origin + '/sitemap/' + shard), { params: { shard } },
    ),
  };
}

const locations = (body) => [...body.matchAll(new RegExp('<loc>([^<]+)</loc>', 'g'))].map((m) => m[1]);

test('index discovers every tag and thread across the shard boundary', async () => {
  const app = harness();
  const index = await app.index();
  assert.equal(index.status, 200);
  const shards = locations(await index.text());
  assert.equal(shards.length, 5);
  const urls = [];
  for (const loc of shards) {
    const before = app.calls.length;
    const name = loc.split('/').at(-1);
    const res = await app.shard(name);
    assert.equal(res.status, 200);
    assert.equal(res.headers.get('cache-control'), 'no-store');
    urls.push(...locations(await res.text()));
    const budget = name.startsWith('tags-') ? 11 : name.startsWith('threads-') ? 6 : 1;
    assert.ok(app.calls.length - before <= budget, name + ' exceeded its bounded reads');
  }
  for (const [segment, prefix, count] of [['tags', 'tag-', 301], ['threads', '', 501]]) {
    const actual = urls.filter((url) => url.startsWith(origin + '/' + segment + '/'));
    const expected = Array.from({ length: count }, (_, i) => origin + '/' + segment + '/' + prefix + (i + 1));
    assert.deepEqual(actual, expected);
    assert.equal(new Set(actual).size, count);
  }
});

test('numeric out-of-range shards return 404 after only a first-page lookup', async () => {
  const app = harness({ tags: 300, threads: 500 });
  for (const name of ['threads-1.xml', 'threads-999.xml', 'tags-1.xml', 'tags-999.xml']) {
    const before = app.calls.length;
    assert.equal((await app.shard(name)).status, 404, name);
    assert.equal(app.calls.length - before, 1);
    assert.equal(app.calls.at(-1).page, 1);
  }
});

test('malformed and unsafe indices are rejected before any upstream request', async () => {
  const app = harness();
  for (const name of ['unknown.xml', 'threads--1.xml', 'threads-01.xml', 'tags-1e3.xml',
    'threads-9007199254740992.xml', 'tags-9007199254740991.xml', 'threads-' + '9'.repeat(400) + '.xml']) {
    assert.equal((await app.shard(name)).status, 404, name);
  }
  assert.equal(app.calls.length, 0);
});

test('empty site retains a valid empty zero shard and rejects later shards', async () => {
  const app = harness({ tags: 0, threads: 0 });
  const index = await app.index();
  assert.equal(locations(await index.text()).length, 3);
  for (const kind of ['threads', 'tags']) {
    const res = await app.shard(kind + '-0.xml');
    assert.equal(res.status, 200);
    assert.deepEqual(locations(await res.text()), []);
    assert.equal((await app.shard(kind + '-1.xml')).status, 404);
  }
});

test('upstream failures remain 503, including later pages of a valid shard', async () => {
  for (const failed of ['/api/v1/tags', '/api/v1/index/threads']) {
    const app = harness({ fail: (endpoint) => endpoint === failed });
    assert.equal((await app.index()).status, 503);
  }
  const partial = harness({ fail: (_, page) => page === 2 });
  for (const kind of ['tags', 'threads']) {
    assert.equal((await partial.shard(kind + '-0.xml')).status, 503);
  }
  const directory = harness({ fail: () => true });
  assert.equal((await directory.shard('pages.xml')).status, 503);
});
