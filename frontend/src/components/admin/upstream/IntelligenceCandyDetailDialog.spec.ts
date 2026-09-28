import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IntelligenceFingerprint, IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import IntelligenceCandyDetailDialog from './IntelligenceCandyDetailDialog.vue'

const mocks = vi.hoisted(() => ({ detail: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: mocks }))
const run = (id = 1, fields: Partial<IntelligenceRun> = {}) => ({ id, plan_id: 4, test_kind: 'candy', status: 'succeeded', correct: true, answer: '21', model: 'gpt-6-astra', reasoning_effort: 'high', created_at: '2026-09-27T01:00:00Z', duration_ms: 1200, raw_text: 'The answer is 21.', ...fields }) as IntelligenceRun
const fingerprint = (fields: Partial<IntelligenceFingerprint> = {}) => ({ method: 'behavioral-jsd-v1', mode: 'quick', status: 'completed', passed: true, model: 'gpt-6-astra', reasoning_effort: 'low', done: 60, total: 60, valid: 60, errors: 0, attribution: { status: 'consistent', nearest: 'gpt-6-astra', message: 'Consistent distribution', alpha: 0.01, self_jsd: 0.01, warnings: [], comparisons: Array.from({ length: 7 }, (_, i) => ({ model: i === 0 ? 'gpt-6-astra' : `baseline-${i}`, mean_jsd: 0.1256 + i / 10, p_value: 0.5, verdict: i === 0 ? 'match' : 'mismatch', self_jsd: 0.03 })) }, ...fields }) as IntelligenceFingerprint
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes }); return { promise, resolve } }
let wrapper: VueWrapper | undefined
function render(value = run()) {
  wrapper = mount(IntelligenceCandyDetailDialog, { props: { run: value }, global: { stubs: { Icon: true, BaseDialog: { name: 'BaseDialog', props: ['show', 'title', 'width', 'motion'], emits: ['close'], template: '<div><slot /></div>' } } } })
  return wrapper
}
beforeEach(() => { vi.resetAllMocks(); mocks.detail.mockResolvedValue(run()) })
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('candy result detail', () => {
  it('uses a fixed fade dialog and renders raw response as escaped text with no preview frame', async () => {
    const value = run(1, { raw_text: '<script>alert(1)</script>21', answer: '21', http_status: 200 })
    mocks.detail.mockResolvedValue(value)
    const view = render(); await flushPromises()
    expect(view.getComponent({ name: 'BaseDialog' }).props()).toMatchObject({ show: true, width: 'wide', motion: 'fade' })
    expect(view.get('[data-testid="candy-detail"]').classes()).toContain('candy-detail')
    expect(mocks.detail).toHaveBeenCalledWith(1, expect.any(AbortSignal))
    expect(view.get('[data-testid="candy-response"]').text()).toBe(value.raw_text)
    expect(view.find('iframe, script').exists()).toBe(false)
    expect(view.text()).toContain('intelligenceMonitor.candy.correct')
    expect(view.get('[data-testid="candy-combined-verdict"]').text()).toBe('intelligenceMonitor.candy.unverified')
    expect(view.text()).toContain('intelligenceMonitor.candy.fingerprint.statuses.missing')
    expect(view.text()).toContain('200')
  })

  it('displays seven baseline comparisons and keeps the combined pass separate from answer correctness', async () => {
    const value = run(1, { fingerprint: fingerprint() })
    mocks.detail.mockResolvedValue(value)
    const view = render(value); await flushPromises()
    expect(view.get('[data-testid="candy-combined-verdict"]').text()).toBe('intelligenceMonitor.candy.passed')
    expect(view.get('[data-testid="fingerprint-comparisons"]').findAll('tbody tr')).toHaveLength(7)
    expect(view.text()).toContain('0.1256')
    expect(view.text()).toContain('0.5000')
    expect(view.text()).toContain('intelligenceMonitor.candy.fingerprint.comparisonVerdicts.match')
    expect(view.text()).toContain('intelligenceMonitor.candy.fingerprint.sampling')
    expect(view.get('[data-testid="candy-detail-scroll"]').classes()).toContain('overflow-y-auto')
    expect(view.text()).toContain('intelligenceMonitor.candy.fingerprint.disclaimer')
  })

  it('refreshes fingerprint progress and completion without clearing the loaded response', async () => {
    const collecting = run(1, { status: 'running', fingerprint: fingerprint({ status: 'collecting', done: 20, valid: 20, passed: null, attribution: undefined }) })
    mocks.detail.mockResolvedValue(collecting)
    const view = render(collecting); await flushPromises()
    expect(view.text()).toContain('intelligenceMonitor.candy.fingerprint.statuses.collecting')
    const pending = deferred<IntelligenceRun>(); mocks.detail.mockReturnValueOnce(pending.promise)
    const comparing = { ...collecting, fingerprint: fingerprint({ status: 'comparing', passed: null, attribution: undefined }) }
    await view.setProps({ run: comparing })
    expect(view.get('[data-testid="candy-response"]').text()).toBe('The answer is 21.')
    pending.resolve(comparing); await flushPromises()
    expect(mocks.detail).toHaveBeenCalledTimes(2)
    const completed = run(1, { fingerprint: fingerprint(), finished_at: '2026-09-27T01:02:00Z' })
    mocks.detail.mockResolvedValue(completed)
    await view.setProps({ run: completed }); await flushPromises()
    expect(view.get('[data-testid="candy-combined-verdict"]').text()).toBe('intelligenceMonitor.candy.passed')
    await view.setProps({ run: { ...completed, fingerprint: { ...completed.fingerprint! } } }); await flushPromises()
    expect(mocks.detail).toHaveBeenCalledTimes(3)
  })

  it('aborts the previous record, ignores its late reply and refreshes a queued record when it finishes', async () => {
    const old = deferred<IntelligenceRun>()
    mocks.detail.mockReturnValueOnce(old.promise)
    const view = render()
    const oldSignal = mocks.detail.mock.calls[0]![1] as AbortSignal
    const queued = run(2, { status: 'pending', correct: null, raw_text: '' })
    mocks.detail.mockResolvedValue(queued)
    await view.setProps({ run: queued }); await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    old.resolve(run()); await flushPromises()
    expect(view.text()).toContain('intelligenceMonitor.candy.pending')
    expect(view.text()).not.toContain('The answer is 21.')
    const completed = run(2, { correct: false, answer: '20', raw_text: '20', finished_at: '2026-09-27T01:00:05Z' })
    mocks.detail.mockResolvedValue(completed)
    await view.setProps({ run: completed }); await flushPromises()
    expect(view.get('[data-testid="candy-response"]').text()).toBe('20')
    expect(view.text()).toContain('intelligenceMonitor.candy.incorrect')
    expect(mocks.detail).toHaveBeenCalledTimes(3)
  })

  it.each([{ id: 2 }, { plan_id: 8 }, { test_kind: 'pelican' }] as const)('rejects a mismatched detail %o and supports retry', async fields => {
    mocks.detail.mockResolvedValueOnce(run(1, fields))
    const view = render(); await flushPromises()
    expect(view.get('[role="alert"]').text()).toBeTruthy()
    expect(view.get('[data-testid="candy-response"]').text()).toBe('intelligenceMonitor.candy.noResponse')
    await view.get('button').trigger('click'); await flushPromises()
    expect(view.find('[role="alert"]').exists()).toBe(false)
    expect(view.get('[data-testid="candy-response"]').text()).toBe('The answer is 21.')
  })

  it('refreshes an open completed record when background regrading corrects its answer', async () => {
    const original = run(1, { correct: false, answer: '', raw_text: 'The answer is \\boxed{21}.', finished_at: '2026-09-27T01:00:05Z' })
    mocks.detail.mockResolvedValue(original)
    const view = render(original); await flushPromises()
    expect(view.text()).toContain('intelligenceMonitor.candy.incorrect')
    const corrected = { ...original, correct: true, answer: '21' }
    mocks.detail.mockResolvedValue(corrected)
    await view.setProps({ run: corrected }); await flushPromises()
    expect(mocks.detail).toHaveBeenCalledTimes(2)
    expect(view.text()).toContain('intelligenceMonitor.candy.correct')
    expect(view.text()).not.toContain('intelligenceMonitor.candy.incorrect')
    await view.setProps({ run: { ...corrected, source_name: 'Updated source label' } }); await flushPromises()
    expect(mocks.detail).toHaveBeenCalledTimes(2)
  })

  it.each(['close', 'unmount'] as const)('aborts on %s and ignores late replies', async event => {
    const response = deferred<IntelligenceRun>(); mocks.detail.mockReturnValueOnce(response.promise)
    const view = render(); const signal = mocks.detail.mock.calls[0]![1] as AbortSignal
    if (event === 'close') view.getComponent({ name: 'BaseDialog' }).vm.$emit('close')
    else { view.unmount(); wrapper = undefined }
    expect(signal.aborted).toBe(true)
    response.resolve(run()); await flushPromises()
    if (event === 'close') { expect(view.emitted('close')).toEqual([[]]); expect(view.text()).not.toContain('The answer is 21.') }
    expect(mocks.detail).toHaveBeenCalledTimes(1)
  })
})
