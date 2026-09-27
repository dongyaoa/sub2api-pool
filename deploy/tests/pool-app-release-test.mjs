import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

// Exercise the workflow's actual publication script with a fake GitHub boundary;
// no credentials, registry access, or remote mutations are used by these tests.
const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const workflow = fs.readFileSync(path.join(repo, '.github/workflows/pool-images.yml'), 'utf8')
const step = workflow.split('      - name: Publish complete application update release\n')[1]
assert.ok(step, 'publication step exists')
const body = step.split('          script: |\n')[1].split('\n').map(line => line.startsWith('            ') ? line.slice(12) : line).join('\n')
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor
const publish = new AsyncFunction('require', 'github', 'context', 'core', 'process', body)
const version = '0.2.7-pool.12'
const revision = 'a'.repeat(40)
const binary = Buffer.from('release fixture binary')
const sha256 = crypto.createHash('sha256').update(binary).digest('hex')

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
