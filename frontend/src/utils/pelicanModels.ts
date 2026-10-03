export const DEFAULT_PELICAN_MODEL = 'gpt-6-astra'

export const PELICAN_MODELS = [
  { value: DEFAULT_PELICAN_MODEL, label: 'GPT-6 Astra' },
  { value: 'gpt-6.1-sol', label: 'GPT-6.1 Sol' },
] as const

export function pelicanModelLabel(model?: string): string {
  const value = model || DEFAULT_PELICAN_MODEL
  return PELICAN_MODELS.find(option => option.value === value)?.label || value
}

export function pelicanModelClass(model?: string): string {
  return model === 'gpt-6.1-sol'
    ? 'border-sky-200/70 bg-sky-50 text-sky-700 dark:border-sky-500/20 dark:bg-sky-500/10 dark:text-sky-300'
    : 'border-violet-200/70 bg-violet-50 text-violet-700 dark:border-violet-500/20 dark:bg-violet-500/10 dark:text-violet-300'
}
