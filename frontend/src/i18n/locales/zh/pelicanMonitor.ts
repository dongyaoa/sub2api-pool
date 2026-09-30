export default {
  pelicanMonitor: {
    title: '鹈鹕监测', description: '用同一道绘画题，持续观察各分组的生成表现。', notice: '公告',
    rate: '分组倍率', schedule: '检测周期', everyMinutes: '每 {count} 分钟检测一次', everySeconds: '每 {count} 秒检测一次', next: '下次检测', afterCurrent: '本次完成后安排', waitingSchedule: '等待调度', paused: '定时已暂停', lastRun: '最近检测',
    recentWorks: '最近作品', newestFirst: '左新右旧', latest: '最新', duration: '生成耗时', search: '搜索分组名称', groupCount: '{count} 个分组', updated: '{seconds} 秒前更新',
    status: { pending: '等待检测', running: '绘制中', succeeded: '已完成', failed: '生成失败', idle: '尚未检测' },
    refresh: '刷新', preview: '鹈鹕动画预览', open: '查看作品', reloadPreview: '重播', loadingPreview: '载入作品…', loadFailed: '暂时无法加载，请稍后刷新重试。', noHTML: '本次没有可展示的作品', waiting: '等待第一幅作品',
    disabled: '鹈鹕监测暂未开放', disabledHint: '开放后可在这里查看各分组的最新作品。', empty: '暂无公开的监测分组', noMatches: '没有匹配的分组', retention: '每个分组保留最近 20 次检测。作品自动播放，新结果自动更新，点击可放大查看。',
  },
}
