import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

// Exercise the workflow's actual publication script with a fake GitHub boundary;
// no credentials, registry access, or remote mutations are used by these tests.
const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const workflow = fs.readFileSync(path.join(repo, '.github/workflows/pool-images.yml'), 'utf8').replaceAll('\r\n', '\n')
function workflowScript(name) {
  const step = workflow.split(`      - name: ${name}\n`)[1]?.split('\n      - name: ')[0]
  assert.ok(step, `workflow step exists: ${name}`)
  const script = step.split('          script: |\n')[1]
  assert.ok(script, `workflow step contains JavaScript: ${name}`)
  return script.split('\n').map(line => line.startsWith('            ') ? line.slice(12) : line).join('\n')
}
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor
const publish = new AsyncFunction('require', 'github', 'context', 'core', 'process', workflowScript('Publish complete application update release'))
const gateName = 'Require successful CI and security scan for the release commit'
const checkRelease = new AsyncFunction('github', 'context', 'core', 'process', 'Date', 'setTimeout', workflowScript(gateName))
const version = '0.2.7-pool.12'
const revision = 'a'.repeat(40)
const binary = Buffer.from('release fixture binary')
const sha256 = crypto.createHash('sha256').update(binary).digest('hex')

function workflowRun(overrides = {}) {
  return { id: 100, run_attempt: 1, head_sha: revision, event: 'push', status: 'completed',
    conclusion: 'success', html_url: 'https://example.test/actions/runs/100', ...overrides }
}

function gateFixture(runs = {}) {
  let now = 0
  const calls = [], waits = [], failures = [], messages = []
  const attempts = new Map()
  const github = { rest: { actions: { listWorkflowRuns: async args => {
    calls.push(args)
    assert.equal(args.head_sha, revision)
    assert.equal(args.event, 'push')
    assert.deepEqual([args.owner, args.repo], ['fixture', 'pool'])
    assert.ok(['backend-ci.yml', 'security-scan.yml'].includes(args.workflow_id))
    const attempt = attempts.get(args.workflow_id) || 0
    attempts.set(args.workflow_id, attempt + 1)
    const configured = runs[args.workflow_id] ?? [workflowRun()]
    const selected = typeof configured === 'function' ? configured(attempt) : configured
    return { data: { workflow_runs: selected } }
  } } } }
  const run = () => checkRelease(github, { repo: { owner: 'fixture', repo: 'pool' } },
    { info: message => messages.push(message), setFailed: message => failures.push(message) },
    { env: { RELEASE_REF: revision } }, { now: () => now }, (resolve, delay) => {
      assert.ok(delay > 0 && delay <= 30_000)
      waits.push(delay)
      now += delay
      assert.ok(now <= 15 * 60 * 1000, 'the gate must not wait past its deadline')
      resolve()
    })
  return { calls, waits, failures, messages, run }
}

test('CI and security gate precedes registry login and publication', () => {
  const gateIndex = workflow.indexOf(`      - name: ${gateName}\n`)
  assert.ok(gateIndex >= 0)
  assert.ok(gateIndex < workflow.indexOf('      - name: Login to GitHub Container Registry\n'))
  assert.ok(gateIndex < workflow.indexOf('      - name: Publish tested image and verify registry digests\n'))
})

test('release gate requires successful push runs for both workflows and the exact SHA', async () => {
  const f = gateFixture()
  await f.run()
  assert.deepEqual(f.failures, [])
  assert.deepEqual(f.waits, [])
  assert.deepEqual(f.calls.map(call => call.workflow_id).sort(), ['backend-ci.yml', 'security-scan.yml'])
  assert.ok(f.messages.some(message => message.startsWith('Release CI and Security Scan passed')))
})

for (const conclusion of ['failure', 'cancelled', 'timed_out', 'skipped', 'neutral']) {
  test(`successful CI cannot publish with security conclusion ${conclusion}`, async () => {
    const f = gateFixture({ 'security-scan.yml': [workflowRun({ conclusion })] })
    await f.run()
    assert.equal(f.failures.length, 1)
    assert.match(f.failures[0], new RegExp(`Security Scan concluded ${conclusion}`))
    assert.deepEqual(f.waits, [])
  })
}

test('successful security cannot publish with failed CI', async () => {
  const f = gateFixture({ 'backend-ci.yml': [workflowRun({ conclusion: 'failure' })] })
  await f.run()
  assert.equal(f.failures.length, 1)
  assert.match(f.failures[0], /CI concluded failure/)
  assert.deepEqual(f.waits, [])
})

for (const workflowID of ['backend-ci.yml', 'security-scan.yml']) {
  for (const invalidRuns of [[], [workflowRun({ head_sha: 'b'.repeat(40) })],
    [workflowRun({ event: 'pull_request' }), workflowRun({ event: 'schedule' })]]) {
    test(`${workflowID} missing matching SHA and push event blocks after 15 minutes: ${JSON.stringify(invalidRuns)}`, async () => {
      const f = gateFixture({ [workflowID]: invalidRuns })
      await f.run()
      assert.equal(f.failures.length, 1)
      assert.match(f.failures[0], /did not both pass within 15 minutes/)
      assert.equal(f.waits.reduce((total, delay) => total + delay, 0), 15 * 60 * 1000)
    })
  }
}

test('release gate waits for pending checks to succeed', async () => {
  const f = gateFixture({
    'backend-ci.yml': attempt => [workflowRun(attempt === 0 ? { status: 'queued', conclusion: null } : {})],
    'security-scan.yml': attempt => attempt === 0 ? []
      : [workflowRun(attempt === 1 ? { status: 'in_progress', conclusion: null } : {})],
  })
  await f.run()
  assert.deepEqual(f.failures, [])
  assert.deepEqual(f.waits, [30_000, 30_000])
  assert.equal(f.calls.length, 6, 'both workflows must be rechecked on every poll')
})

test('pending security check times out even after CI succeeded', async () => {
  const f = gateFixture({ 'security-scan.yml': [workflowRun({ status: 'in_progress', conclusion: null })] })
  await f.run()
  assert.equal(f.failures.length, 1)
  assert.match(f.failures[0], /did not both pass within 15 minutes/)
  assert.equal(f.waits.reduce((total, delay) => total + delay, 0), 15 * 60 * 1000)
})

test('latest matching push run wins over an older successful run', async () => {
  const f = gateFixture({ 'security-scan.yml': [workflowRun(), workflowRun({ id: 101, conclusion: 'failure' })] })
  await f.run()
  assert.equal(f.failures.length, 1)
  assert.match(f.failures[0], /Security Scan concluded failure/)
})

test('a newly started security run supersedes success while CI is pending', async () => {
  const f = gateFixture({
    'backend-ci.yml': attempt => [workflowRun(attempt === 0 ? { status: 'in_progress', conclusion: null } : {})],
    'security-scan.yml': attempt => [workflowRun(), ...(attempt > 0 ? [workflowRun({ id: 101, conclusion: 'cancelled' })] : [])],
  })
  await f.run()
  assert.deepEqual(f.waits, [30_000])
  assert.equal(f.failures.length, 1)
  assert.match(f.failures[0], /Security Scan concluded cancelled/)
})

test('GitHub API errors fail the gate without allowing publication', async () => {
  const f = gateFixture({ 'security-scan.yml': () => { throw new Error('API unavailable') } })
  await assert.rejects(f.run, /API unavailable/)
  assert.ok(!f.messages.some(message => message.startsWith('Release CI and Security Scan passed')))
})

function fixture(options = {}) {
  const events = []
  const manifest = Buffer.from(JSON.stringify({ schema_version: 1, version, revision,
    platform: 'linux/amd64', asset: 'sub2api-linux-amd64', sha256, size: binary.length, ...options.manifest }))
  const github = {
    paginate: async () => options.assets || [],
    rest: { repos: {
      getReleaseByTag: async () => {
        if (options.release) return { data: options.release }
        throw Object.assign(new Error('missing'), { status: 404 })
      },
      createRelease: async data => {
        assert.equal(data.draft, true)
        assert.equal(data.target_commitish, revision)
        events.push('draft')
        return { data: { id: 42 } }
      },
      listReleaseAssets: async () => {},
      deleteReleaseAsset: async ({ asset_id }) => { events.push(`delete:${asset_id}`) },
      uploadReleaseAsset: async ({ name, data }) => {
        events.push(`upload:${name}`)
        if (options.failUpload === name) throw new Error('network failure')
        return { data: { state: options.assetState || 'uploaded', size: data.length,
          digest: options.assetDigest || `sha256:${crypto.createHash('sha256').update(data).digest('hex')}` } }
      },
      updateRelease: async data => {
        assert.equal(data.draft, false)
        events.push('publish')
        return { data: { html_url: 'https://example.test/release' } }
      },
    } },
  }
  const require = name => name === 'node:fs'
    ? { readFileSync: filename => filename.endsWith('pool-update.json') ? manifest : binary }
    : name === 'node:path' ? path : crypto
  const run = () => publish(require, github,
    { repo: { owner: 'fixture', repo: 'pool' }, serverUrl: 'https://example.test', runId: 1 },
    { summary: { addLink() {}, async write() {} } },
    { env: { RELEASE_VERSION: version, RELEASE_REF: revision, RUNNER_TEMP: '/tmp', POOL_IMAGE: 'fixture/pool' } })
  return { events, run }
}

test('publish only after both verified draft assets', async () => {
  const f = fixture()
  await f.run()
  assert.deepEqual(f.events, ['draft', 'upload:sub2api-linux-amd64', 'upload:pool-update.json', 'publish'])
})

for (const options of [
  { failUpload: 'pool-update.json' },
  { assetState: 'starter' },
  { assetDigest: `sha256:${'0'.repeat(64)}` },
]) {
  test(`incomplete upload remains unpublished: ${JSON.stringify(options)}`, async () => {
    const f = fixture(options)
    await assert.rejects(f.run)
    assert.ok(!f.events.includes('publish'))
  })
}

for (const release of [
  { id: 42, draft: false, target_commitish: revision },
  { id: 42, draft: true, target_commitish: 'b'.repeat(40) },
]) {
  test(`refuse incompatible existing release: ${JSON.stringify(release)}`, async () => {
    const f = fixture({ release })
    await assert.rejects(f.run, /Refusing to overwrite/)
    assert.deepEqual(f.events, [])
  })
}

test('resume matching draft without deleting unrelated attachments', async () => {
  const f = fixture({ release: { id: 42, draft: true, target_commitish: revision }, assets: [
    { id: 1, name: 'sub2api-linux-amd64' }, { id: 2, name: 'pool-update.json' }, { id: 3, name: 'notes.txt' },
  ] })
  await f.run()
  assert.deepEqual(f.events, ['delete:1', 'delete:2', 'upload:sub2api-linux-amd64', 'upload:pool-update.json', 'publish'])
})

test('corrupt local manifest prevents all publication mutations', async () => {
  const f = fixture({ manifest: { sha256: '0'.repeat(64) } })
  await assert.rejects(f.run, /does not match/)
  assert.deepEqual(f.events, [])
})
