import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTestModal from '../AccountTestModal.vue'

const { getAvailableModels, copyToClipboard } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAvailableModels
    }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'admin.accounts.imagePromptDefault': 'Generate a cute orange cat astronaut sticker on a clean pastel background.'
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.accounts.imageReceived' && params?.count) {
          return `received-${params.count}`
        }
        if (key === 'admin.accounts.imagePreviewAlt' && params?.index) {
          return `test-image-${params.index}`
        }
        if (key === 'admin.accounts.testProxySelected') {
          return `Test proxy: ${params?.name} (ID: ${params?.id})`
        }
        return messages[key] || key
      }
    })
  }
})

function createStreamResponse(lines: string[]) {
  const encoder = new TextEncoder()
  const chunks = lines.map((line) => encoder.encode(line))
  let index = 0

  return {
    ok: true,
    body: {
      getReader: () => ({
        read: vi.fn().mockImplementation(async () => {
          if (index < chunks.length) {
            return { done: false, value: chunks[index++] }
          }
          return { done: true, value: undefined }
        })
      })
    }
  } as Response
}

function mountModal(account: Record<string, unknown> = {
  id: 42,
  name: 'Gemini Image Test',
  platform: 'gemini',
  type: 'apikey',
  status: 'active'
}) {
  return mount(AccountTestModal, {
    props: {
      show: false,
      account
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: {
          props: ['modelValue', 'options', 'disabled', 'valueKey', 'labelKey'],
          emits: ['update:modelValue'],
          template: `<select
            class="select-stub"
            :value="modelValue ?? ''"
            :disabled="disabled"
            @change="$emit('update:modelValue', options.find(option => String(option[valueKey || 'value'] ?? '') === $event.target.value)?.[valueKey || 'value'] ?? null)"
          >
            <option v-for="(option, index) in options" :key="index" :value="option[valueKey || 'value'] ?? ''" :disabled="option.disabled">
              {{ option[labelKey || 'label'] }}
            </option>
          </select>`
        },
        TextArea: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template: '<textarea class="textarea-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />'
        },
        Icon: true
      }
    }
  })
}

describe('AccountTestModal', () => {
  beforeEach(() => {
    getAvailableModels.mockResolvedValue([
      { id: 'gemini-2.0-flash', display_name: 'Gemini 2.0 Flash' },
      { id: 'gemini-2.5-flash-image', display_name: 'Gemini 2.5 Flash Image' },
      { id: 'gemini-3.1-flash-image', display_name: 'Gemini 3.1 Flash Image' }
    ])
    copyToClipboard.mockReset()
    Object.defineProperty(globalThis, 'localStorage', {
      value: {
        getItem: vi.fn((key: string) => (key === 'auth_token' ? 'test-token' : null)),
        setItem: vi.fn(),
        removeItem: vi.fn(),
        clear: vi.fn()
      },
      configurable: true
    })
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"gemini-2.5-flash-image"}\n',
        'data: {"type":"image","image_url":"data:image/png;base64,QUJD","mime_type":"image/png"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('gemini 图片模型测试会携带提示词并渲染图片预览', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    const promptInput = wrapper.find('textarea.textarea-stub')
    expect(promptInput.exists()).toBe(true)
    await promptInput.setValue('draw a tiny orange cat astronaut')

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'gemini-3.1-flash-image',
      prompt: 'draw a tiny orange cat astronaut'
    })

    const preview = wrapper.find('img[alt="test-image-1"]')
    expect(preview.exists()).toBe(true)
    expect(preview.attributes('src')).toBe('data:image/png;base64,QUJD')
  })

  it('grok 账号测试默认选择 Grok 模型', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'grok-4.3', display_name: 'Grok 4.3' },
      { id: 'grok-build-0.1', display_name: 'Grok Build 0.1' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"grok-4.3"}\n',
        'data: {"type":"content","text":"ok"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any

    const wrapper = mountModal({
      id: 13,
      name: 'Grok Account',
      platform: 'grok',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'grok-4.3',
      prompt: '',
      mode: 'text'
    })
  })

  it('OpenAI Compact 探测会携带 compact 测试模式', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any

    const wrapper = mountModal({
      id: 42,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    ;(wrapper.vm as any).testMode = 'compact'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toMatchObject({
      model_id: 'gpt-5.4',
      prompt: '',
      mode: 'compact'
    })
  })

  it('显示服务端实际选中的代理并在重试等待期间清除旧代理', async () => {
    let finishRetry!: (response: Response) => void
    global.fetch = vi.fn()
      .mockResolvedValueOnce(createStreamResponse([
        'data: {"type":"proxy_info","route_type":"managed","proxy_id":12,"proxy_name":"US selected node"}\n',
        'data: {"type":"error","error":"API returned 429"}\n'
      ]))
      .mockImplementationOnce(() => new Promise<Response>((resolve) => { finishRetry = resolve })) as any

    const wrapper = mountModal({
      id: 42,
      name: 'Multiple proxy account',
      platform: 'gemini',
      type: 'apikey',
      status: 'active',
      proxy_id: 11,
      proxy: { id: 11, name: 'Legacy primary node' }
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    const startButton = wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.startTest'))
    await startButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('US selected node')
    expect(wrapper.text()).toContain('ID: 12')
    expect(wrapper.find('[data-testid="account-test-actual-proxy"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('API returned 429')
    const copyButton = wrapper.find('button[title="admin.accounts.copyOutput"]')
    await copyButton.trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith(
      expect.stringContaining('Test proxy: US selected node (ID: 12)'),
      'admin.accounts.outputCopied'
    )

    const retryButton = wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.retry'))
    await retryButton!.trigger('click')
    await flushPromises()
    expect(global.fetch).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('US selected node')
    expect(wrapper.text()).not.toContain('ID: 12')

    finishRetry(createStreamResponse([
      'data: {"type":"proxy_info","route_type":"managed","proxy_id":13,"proxy_name":"EU retry node"}\n',
      'data: {"type":"test_complete","success":true}\n'
    ]))
    await flushPromises()
    expect(wrapper.text()).toContain('EU retry node')
    expect(wrapper.text()).toContain('ID: 13')
    expect(wrapper.text()).not.toContain('US selected node')
    wrapper.unmount()
  })

  it.each(['direct', 'unknown'])('显示 %s 路由且重新打开时清除旧结果', async (routeType) => {
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      `data: {"type":"proxy_info","route_type":"${routeType}"}\n`,
      'data: {"type":"test_complete","success":true}\n'
    ])) as any
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()
    const startButton = wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.startTest'))
    await startButton!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain(`admin.accounts.testProxyRoute.${routeType}`)
    expect(wrapper.text()).not.toContain('ID:')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.text()).not.toContain(`admin.accounts.testProxyRoute.${routeType}`)
    wrapper.unmount()
  })

  it('不再显示代理选择，并由后端从账号代理池自动分配', async () => {
    const wrapper = mountModal({
      id: 42,
      name: 'Proxy pool account',
      platform: 'gemini',
      type: 'apikey',
      status: 'active',
      proxy_id: 99,
      proxy: { id: 99, name: 'Outside pool', status: 'active' },
      proxy_pool: [
        { proxy_id: 12, concurrency: 1, proxy: { id: 12, name: 'US node', status: 'active' } },
        { proxy_id: 13, concurrency: 1, proxy: { id: 13, name: 'Disabled node', status: 'inactive' } },
        { proxy_id: 14, concurrency: 1, proxy: { id: 14, name: 'Expired node', status: 'expired' } },
        { proxy_id: 15, concurrency: 1 },
        { proxy_id: 16, concurrency: 1, proxy: { id: 16, name: 'Past expiry', status: 'active', expires_at: '2000-01-01T00:00:00Z' } }
      ]
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(wrapper.find('[data-testid="account-test-proxy-select"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.accounts.testProxyOptions.label')
    await wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.trigger('click')
    await flushPromises()

    expect(JSON.parse(vi.mocked(global.fetch).mock.calls[0][1]!.body as string)).not.toHaveProperty('proxy_id')
    wrapper.unmount()
  })

  it('兼容旧版单代理绑定且不提交代理 ID', async () => {
    const wrapper = mountModal({
      id: 42,
      name: 'Legacy proxy account',
      platform: 'gemini',
      type: 'apikey',
      status: 'active',
      proxy_id: 11,
      proxy: { id: 11, name: 'Legacy node', status: 'active' }
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.find('[data-testid="account-test-proxy-select"]').exists()).toBe(false)
    await wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.trigger('click')
    await flushPromises()
    expect(JSON.parse(vi.mocked(global.fetch).mock.calls[0][1]!.body as string)).not.toHaveProperty('proxy_id')
    wrapper.unmount()
  })

  it('每次重试都保留后端自动代理轮询，不固定上次代理', async () => {
    let finishRetry!: (response: Response) => void
    global.fetch = vi.fn()
      .mockResolvedValueOnce(createStreamResponse([
        'data: {"type":"test_metrics","latency_ms":120,"first_token_ms":280,"duration_ms":930}\n',
        'data: {"type":"error","error":"API returned 429"}\n'
      ]))
      .mockImplementationOnce(() => new Promise<Response>(resolve => { finishRetry = resolve })) as any
    const wrapper = mountModal({
      id: 42,
      name: 'Retry account',
      platform: 'gemini',
      type: 'apikey',
      status: 'active',
      proxy_pool: [{ proxy_id: 12, concurrency: 1, proxy: { id: 12, name: 'US node', status: 'active' } }]
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="account-test-metrics"]').exists()).toBe(false)

    await wrapper.findAll('button').find(button => button.text().includes('admin.accounts.retry'))!.trigger('click')
    await flushPromises()
    expect(JSON.parse(vi.mocked(global.fetch).mock.calls[1][1]!.body as string)).not.toHaveProperty('proxy_id')
    expect(wrapper.findAll('button').find(button => button.text().includes('admin.accounts.testing'))!.attributes('disabled')).toBeDefined()

    finishRetry(createStreamResponse([
      'data: {"type":"test_metrics","duration_ms":450}\n',
      'data: {"type":"test_complete","success":true}\n'
    ]))
    await flushPromises()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.find('[data-testid="account-test-metrics"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('打开弹窗、加载模型和修改选择都不会测试，手动开始后仅发起一次请求', async () => {
    let finishModels!: (models: Array<{ id: string; display_name: string }>) => void
    getAvailableModels.mockImplementationOnce(() => new Promise(resolve => { finishModels = resolve }))
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(global.fetch).not.toHaveBeenCalled()
    expect(wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.attributes('disabled')).toBeDefined()

    finishModels([
      { id: 'gemini-3.1-flash-image', display_name: 'Gemini Image' },
      { id: 'gemini-2.0-flash', display_name: 'Gemini Flash' }
    ])
    await flushPromises()
    expect(global.fetch).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.accounts.readyToTest')
    expect(wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.attributes('disabled')).toBeUndefined()

    await wrapper.get('select.select-stub').setValue('gemini-2.0-flash')
    await wrapper.setProps({ account: { ...wrapper.props('account')!, name: 'Renamed account' } })
    await flushPromises()
    expect(global.fetch).not.toHaveBeenCalled()

    await wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.trigger('click')
    await flushPromises()
    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect(JSON.parse(vi.mocked(global.fetch).mock.calls[0][1]!.body as string)).toEqual({
      model_id: 'gemini-2.0-flash',
      prompt: ''
    })
    wrapper.unmount()
  })

  it('等待模型时关闭弹窗，不会发起后台测试', async () => {
    let finishModels!: (models: Array<{ id: string; display_name: string }>) => void
    getAvailableModels.mockImplementationOnce(() => new Promise(resolve => { finishModels = resolve }))
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await wrapper.setProps({ show: false })
    finishModels([{ id: 'gemini-2.0-flash', display_name: 'Gemini Flash' }])
    await flushPromises()
    expect(global.fetch).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('重新打开其他账号时忽略旧模型请求，手动开始后只测试当前账号', async () => {
    let finishOldModels!: (models: Array<{ id: string; display_name: string }>) => void
    getAvailableModels.mockImplementationOnce(() => new Promise(resolve => { finishOldModels = resolve }))
    getAvailableModels.mockResolvedValueOnce([{ id: 'new-account-model', display_name: 'New model' }])
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, account: { ...wrapper.props('account')!, id: 99 } })
    await flushPromises()
    finishOldModels([{ id: 'old-account-model', display_name: 'Old model' }])
    await flushPromises()
    expect(global.fetch).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('Old model')
    await wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.trigger('click')
    await flushPromises()
    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect(vi.mocked(global.fetch).mock.calls[0][0]).toContain('/admin/accounts/99/test')
    expect(JSON.parse(vi.mocked(global.fetch).mock.calls[0][1]!.body as string).model_id).toBe('new-account-model')
    expect(wrapper.text()).not.toContain('Old model')
    wrapper.unmount()
  })

  it('关闭手动测试会取消请求，重新打开等待点击且迟到的旧响应不会覆盖新测试结果', async () => {
    let finishOldTest!: (response: Response) => void
    global.fetch = vi.fn()
      .mockImplementationOnce(() => new Promise<Response>(resolve => { finishOldTest = resolve }))
      .mockResolvedValueOnce(createStreamResponse([
        'data: {"type":"proxy_info","route_type":"managed","proxy_id":13,"proxy_name":"Current proxy"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])) as any
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(global.fetch).not.toHaveBeenCalled()
    await wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.trigger('click')
    await flushPromises()
    const previousSignal = vi.mocked(global.fetch).mock.calls[0][1]!.signal!
    await wrapper.setProps({ show: false })
    expect(previousSignal.aborted).toBe(true)
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('admin.accounts.readyToTest')
    await wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.trigger('click')
    await flushPromises()
    finishOldTest(createStreamResponse([
      'data: {"type":"proxy_info","route_type":"managed","proxy_id":12,"proxy_name":"Old proxy"}\n',
      'data: {"type":"error","error":"Old test failure"}\n'
    ]))
    await flushPromises()
    expect(global.fetch).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Current proxy')
    expect(wrapper.text()).not.toContain('Old proxy')
    expect(wrapper.text()).not.toContain('Old test failure')
    expect(wrapper.text()).toContain('admin.accounts.testCompleted')
    wrapper.unmount()
  })

  it('缺少测试模型时显示原因且不发送无效请求', async () => {
    getAvailableModels.mockResolvedValueOnce([])
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.accounts.testNoModelsAvailable')
    expect(global.fetch).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('测试模型加载失败时显示可理解的错误', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    getAvailableModels.mockRejectedValueOnce(new Error('model fetch failed'))
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.accounts.testModelsLoadFailed')
    expect(global.fetch).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('收到耗时事件后仍可完成测试，页面及复制输出不再包含耗时', async () => {
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      'data: {"type":"test_metrics","latency_ms":0}\n',
      'data: {"type":"test_metrics","duration_ms":375}\n',
      'data: {"type":"test_complete","success":true}\n'
    ])) as any
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="account-test-metrics"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.testComplete')
    await wrapper.get('button[title="admin.accounts.copyOutput"]').trigger('click')
    const copiedOutput = copyToClipboard.mock.calls[0][0] as string
    expect(copiedOutput).not.toContain('admin.accounts.testMetrics')
    wrapper.unmount()
  })
})
